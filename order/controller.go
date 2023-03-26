package order

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rodrigo-brito/ninjabot/exchange"
	"github.com/rodrigo-brito/ninjabot/model"
	"github.com/rodrigo-brito/ninjabot/service"
	"github.com/rodrigo-brito/ninjabot/storage"

	"github.com/olekukonko/tablewriter"
	log "github.com/sirupsen/logrus"
)

type summary struct {
	Pair      string
	WinLong   []float64
	WinShort  []float64
	LoseLong  []float64
	LoseShort []float64
	Volume    float64
}

func (s summary) Win() []float64 {
	return append(s.WinLong, s.WinShort...)
}

func (s summary) Lose() []float64 {
	return append(s.LoseLong, s.LoseShort...)
}

func (s summary) Profit() float64 {
	profit := 0.0
	for _, value := range append(s.Win(), s.Lose()...) {
		profit += value
	}
	return profit
}

func (s summary) SQN() float64 {
	total := float64(len(s.Win()) + len(s.Lose()))
	avgProfit := s.Profit() / total
	stdDev := 0.0
	for _, profit := range append(s.Win(), s.Lose()...) {
		stdDev += math.Pow(profit-avgProfit, 2)
	}
	stdDev = math.Sqrt(stdDev / total)
	return math.Sqrt(total) * (s.Profit() / total) / stdDev
}

func (s summary) Payoff() float64 {
	avgWin := 0.0
	avgLose := 0.0

	for _, value := range s.Win() {
		avgWin += value
	}

	for _, value := range s.Lose() {
		avgLose += value
	}

	if len(s.Win()) == 0 || len(s.Lose()) == 0 || avgLose == 0 {
		return 0
	}

	return (avgWin / float64(len(s.Win()))) / math.Abs(avgLose/float64(len(s.Lose())))
}

func (s summary) WinPercentage() float64 {
	if len(s.Win())+len(s.Lose()) == 0 {
		return 0
	}

	return float64(len(s.Win())) / float64(len(s.Win())+len(s.Lose())) * 100
}

func (s summary) String() string {
	tableString := &strings.Builder{}
	table := tablewriter.NewWriter(tableString)
	_, quote := exchange.SplitAssetQuote(s.Pair)
	data := [][]string{
		{"Coin", s.Pair},
		{"Trades", strconv.Itoa(len(s.Lose()) + len(s.Win()))},
		{"Win", strconv.Itoa(len(s.Win()))},
		{"Loss", strconv.Itoa(len(s.Lose()))},
		{"% Win", fmt.Sprintf("%.1f", s.WinPercentage())},
		{"Payoff", fmt.Sprintf("%.1f", s.Payoff()*100)},
		{"Profit", fmt.Sprintf("%.4f %s", s.Profit(), quote)},
		{"Volume", fmt.Sprintf("%.4f %s", s.Volume, quote)},
	}
	table.AppendBulk(data)
	table.SetColumnAlignment([]int{tablewriter.ALIGN_LEFT, tablewriter.ALIGN_RIGHT})
	table.Render()
	return tableString.String()
}

type Status string

const (
	StatusRunning Status = "running"
	StatusStopped Status = "stopped"
	StatusError   Status = "error"
)

type Controller struct {
	mtx            sync.Mutex
	ctx            context.Context
	exchange       service.Exchange
	storage        storage.Storage
	orderFeed      *Feed
	notifier       service.Notifier
	Results        map[string]*summary
	lastPrice      map[string]float64
	tickerInterval time.Duration
	finish         chan bool
	status         Status
}

func NewController(ctx context.Context, exchange service.Exchange, storage storage.Storage,
	orderFeed *Feed) *Controller {

	return &Controller{
		ctx:            ctx,
		storage:        storage,
		exchange:       exchange,
		orderFeed:      orderFeed,
		lastPrice:      make(map[string]float64),
		Results:        make(map[string]*summary),
		tickerInterval: time.Second,
		finish:         make(chan bool),
	}
}

func (c *Controller) SetNotifier(notifier service.Notifier) {
	c.notifier = notifier
}

func (c *Controller) OnCandle(candle model.Candle) {
	c.lastPrice[candle.Pair] = candle.Close
}

/*
这个函数的主要目的是计算订单的利润（value）和利润百分比（percent）。它接收一个指向 model.Order 类型的指针 o 作为参数，并返回计算得到的利润值、利润百分比以及可能的错误（err）。

以下是该函数的大致步骤：

从存储中获取在当前订单之前已完成（状态为已成交，model.OrderStatusTypeFilled）的订单，同时筛选出与当前订单具有相同交易对（o.Pair）的订单。

初始化变量：quantity（表示持有数量，正数表示买入，负数表示卖出），avgPriceLong（表示买入平均价格）和 avgPriceShort（表示卖出平均价格）。

遍历筛选后的订单：

跳过当前订单（o.ID == order.ID）。
计算订单价格。对于止损和止损限价订单，使用止损价格（*order.Stop）。
根据订单方向（买入/卖出）计算持有数量的变化（diff）。
计算买入和卖出的平均价格。
更新持有数量（quantity）。
如果最终持有数量（quantity）为零，则返回 0 利润值和 0 利润百分比。

如果当前订单是买入订单（o.Side == model.SideTypeBuy），并且持有数量为负数（即空头），则计算空头利润：

使用当前订单价格或止损价格计算利润值。
计算利润百分比。
返回利润值和利润百分比。
如果当前订单是卖出订单（o.Side == model.SideTypeSell），并且持有数量为正数（即多头），则计算多头利润：

使用当前订单价格或止损价格计算利润值。
计算利润百分比。
返回利润值和利润百分比。
如果不满足上述条件，则返回 0 利润值和 0 利润百分比。

这个函数可以用于计算在给定订单的情况下，交易者的利润值和利润百分比。它可以帮助交易者了解他们在交易过程中的盈亏情况。
*/
func (c *Controller) calculateProfit(o *model.Order) (value, percent float64, err error) {
	// get filled orders before the current order
	// 筛选
	orders, err := c.storage.Orders(
		storage.WithUpdateAtBeforeOrEqual(o.UpdatedAt),
		storage.WithStatus(model.OrderStatusTypeFilled),
		storage.WithPair(o.Pair),
	)
	if err != nil {
		return 0, 0, err
	}

	quantity := 0.0
	avgPriceLong := 0.0
	avgPriceShort := 0.0

	for _, order := range orders {

		// skip current order
		if o.ID == order.ID {
			continue
		}

		// calculate avg price
		price := order.Price
		if order.Type == model.OrderTypeStopLoss || order.Type == model.OrderTypeStopLossLimit {
			price = *order.Stop
		}

		var diff = order.Quantity
		if order.Side == model.SideTypeSell {
			diff = -order.Quantity
		}

		if order.Side == model.SideTypeBuy && quantity+diff >= 0 {
			avgPriceLong = (order.Quantity*price + avgPriceLong*math.Abs(quantity)) / (order.Quantity + math.Abs(quantity))
		} else if order.Side == model.SideTypeSell && quantity+diff <= 0 {
			avgPriceShort = (order.Quantity*price + avgPriceShort*math.Abs(quantity)) / (order.Quantity + math.Abs(quantity))
		}

		if order.Side == model.SideTypeBuy {
			quantity += order.Quantity
		} else {
			quantity -= order.Quantity
		}

	}

	if quantity == 0 {
		return 0, 0, nil
	}

	if o.Side == model.SideTypeBuy && quantity < 0 {
		// profit short
		price := o.Price
		if o.Type == model.OrderTypeStopLoss || o.Type == model.OrderTypeStopLossLimit {
			price = *o.Stop
		}
		profitValue := (avgPriceShort - price) * o.Quantity

		/*
			这部分计算的是空头交易的利润百分比。我们将其分解如下：

			profitValue：空头利润值，即（平均卖出价格 - 当前订单价格）* 订单数量。空头交易的目标是在价格下跌时获利，所以我们从平均卖出价格中减去当前订单价格。

			o.Quantity：当前订单的数量。我们用利润值除以订单数量，以获取每个单位的利润。

			avgPriceShort：平均卖出价格，即之前的卖出订单的平均价格。我们将每个单位的利润再除以平均卖出价格，得到空头利润的百分比。

			综上，profitValue / o.Quantity / avgPriceShort 是计算空头利润百分比的公式。这个百分比表示相对于平均卖出价格的盈亏情况。例如，如果空头利润百分比为 0.05，表示当前订单相对于平均卖出价格获得了 5% 的利润。
		*/
		return profitValue, profitValue / o.Quantity / avgPriceShort, nil
	}

	if o.Side == model.SideTypeSell && quantity > 0 {
		// profit long
		price := o.Price
		if o.Type == model.OrderTypeStopLoss || o.Type == model.OrderTypeStopLossLimit {
			price = *o.Stop
		}
		profitValue := (price - avgPriceLong) * o.Quantity
		return profitValue, profitValue / o.Quantity / avgPriceLong, nil
	}

	return 0, 0, nil
}

func (c *Controller) notify(message string) {
	log.Info(message)
	if c.notifier != nil {
		c.notifier.Notify(message)
	}
}

func (c *Controller) notifyError(err error) {
	log.Error(err)
	if c.notifier != nil {
		c.notifier.OnError(err)
	}
}

func (c *Controller) processTrade(order *model.Order) {
	if order.Status != model.OrderStatusTypeFilled {
		return
	}

	// initializer results map if needed
	if _, ok := c.Results[order.Pair]; !ok {
		c.Results[order.Pair] = &summary{Pair: order.Pair}
	}

	// register order volume
	c.Results[order.Pair].Volume += order.Price * order.Quantity

	profitValue, profit, err := c.calculateProfit(order)
	if err != nil {
		c.notifyError(err)
		return
	}

	order.Profit = profit
	if profitValue == 0 {
		return
	} else if profitValue > 0 {
		if order.Side == model.SideTypeBuy {
			c.Results[order.Pair].WinLong = append(c.Results[order.Pair].WinLong, profitValue)
		} else {
			c.Results[order.Pair].WinShort = append(c.Results[order.Pair].WinShort, profitValue)
		}
	} else {
		if order.Side == model.SideTypeBuy {
			c.Results[order.Pair].LoseLong = append(c.Results[order.Pair].LoseLong, profitValue)
		} else {
			c.Results[order.Pair].LoseShort = append(c.Results[order.Pair].LoseShort, profitValue)
		}
	}

	_, quote := exchange.SplitAssetQuote(order.Pair)
	c.notify(fmt.Sprintf("[PROFIT] %f %s (%f %%)\n`%s`", profitValue, quote, profit*100, c.Results[order.Pair].String()))
}

func (c *Controller) updateOrders() {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	// pending orders
	orders, err := c.storage.Orders(storage.WithStatusIn(
		model.OrderStatusTypeNew,
		model.OrderStatusTypePartiallyFilled,
		model.OrderStatusTypePendingCancel,
	))
	if err != nil {
		c.notifyError(err)
		c.mtx.Unlock()
		return
	}

	// For each pending order, check for updates
	var updatedOrders []model.Order
	for _, order := range orders {
		excOrder, err := c.exchange.Order(order.Pair, order.ExchangeID)
		if err != nil {
			log.WithField("id", order.ExchangeID).Error("orderControler/get: ", err)
			continue
		}

		// no status change
		if excOrder.Status == order.Status {
			continue
		}

		excOrder.ID = order.ID
		err = c.storage.UpdateOrder(&excOrder)
		if err != nil {
			c.notifyError(err)
			continue
		}

		log.Infof("[ORDER %s] %s", excOrder.Status, excOrder)
		updatedOrders = append(updatedOrders, excOrder)
	}

	for _, processOrder := range updatedOrders {
		c.processTrade(&processOrder)
		c.orderFeed.Publish(processOrder, false)
	}
}

func (c *Controller) Status() Status {
	return c.status
}

func (c *Controller) Start() {
	// 定时任务
	if c.status != StatusRunning {
		c.status = StatusRunning
		go func() {
			ticker := time.NewTicker(c.tickerInterval)
			for {
				select {
				case <-ticker.C:
					c.updateOrders()
				case <-c.finish:
					ticker.Stop()
					return
				}
			}
		}()
		log.Info("Bot started.")
	}
}

func (c *Controller) Stop() {
	if c.status == StatusRunning {
		c.status = StatusStopped
		c.updateOrders()
		c.finish <- true
		log.Info("Bot stopped.")
	}
}

func (c *Controller) Account() (model.Account, error) {
	return c.exchange.Account()
}

func (c *Controller) Position(pair string) (asset, quote float64, err error) {
	return c.exchange.Position(pair)
}

func (c *Controller) LastQuote(pair string) (float64, error) {
	return c.exchange.LastQuote(c.ctx, pair)
}

func (c *Controller) PositionValue(pair string) (float64, error) {
	asset, _, err := c.exchange.Position(pair)
	if err != nil {
		return 0, err
	}
	return asset * c.lastPrice[pair], nil
}

func (c *Controller) Order(pair string, id int64) (model.Order, error) {
	return c.exchange.Order(pair, id)
}

func (c *Controller) CreateOrderOCO(side model.SideType, pair string, size, price, stop,
	stopLimit float64) ([]model.Order, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	log.Infof("[ORDER] Creating OCO order for %s", pair)
	orders, err := c.exchange.CreateOrderOCO(side, pair, size, price, stop, stopLimit)
	if err != nil {
		c.notifyError(err)
		return nil, err
	}

	for i := range orders {
		err := c.storage.CreateOrder(&orders[i])
		if err != nil {
			c.notifyError(err)
			return nil, err
		}
		go c.orderFeed.Publish(orders[i], true)
	}

	return orders, nil
}

func (c *Controller) CreateOrderLimit(side model.SideType, pair string, size, limit float64) (model.Order, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	log.Infof("[ORDER] Creating LIMIT %s order for %s", side, pair)
	order, err := c.exchange.CreateOrderLimit(side, pair, size, limit)
	if err != nil {
		c.notifyError(err)
		return model.Order{}, err
	}

	err = c.storage.CreateOrder(&order)
	if err != nil {
		c.notifyError(err)
		return model.Order{}, err
	}
	go c.orderFeed.Publish(order, true)
	log.Infof("[ORDER CREATED] %s", order)
	return order, nil
}

func (c *Controller) CreateOrderMarketQuote(side model.SideType, pair string, amount float64) (model.Order, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	log.Infof("[ORDER] Creating MARKET %s order for %s", side, pair)
	order, err := c.exchange.CreateOrderMarketQuote(side, pair, amount)
	if err != nil {
		c.notifyError(err)
		return model.Order{}, err
	}

	err = c.storage.CreateOrder(&order)
	if err != nil {
		c.notifyError(err)
		return model.Order{}, err
	}

	// calculate profit
	c.processTrade(&order)
	go c.orderFeed.Publish(order, true)
	log.Infof("[ORDER CREATED] %s", order)
	return order, err
}

func (c *Controller) CreateOrderMarket(side model.SideType, pair string, size float64) (model.Order, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	log.Infof("[ORDER] Creating MARKET %s order for %s", side, pair)
	order, err := c.exchange.CreateOrderMarket(side, pair, size)
	if err != nil {
		c.notifyError(err)
		return model.Order{}, err
	}

	err = c.storage.CreateOrder(&order)
	if err != nil {
		c.notifyError(err)
		return model.Order{}, err
	}

	// calculate profit
	c.processTrade(&order)
	go c.orderFeed.Publish(order, true)
	log.Infof("[ORDER CREATED] %s", order)
	return order, err
}

func (c *Controller) CreateOrderStop(pair string, size float64, limit float64) (model.Order, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	log.Infof("[ORDER] Creating STOP order for %s", pair)
	order, err := c.exchange.CreateOrderStop(pair, size, limit)
	if err != nil {
		c.notifyError(err)
		return model.Order{}, err
	}

	err = c.storage.CreateOrder(&order)
	if err != nil {
		c.notifyError(err)
		return model.Order{}, err
	}
	go c.orderFeed.Publish(order, true)
	log.Infof("[ORDER CREATED] %s", order)
	return order, nil
}

func (c *Controller) Cancel(order model.Order) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	log.Infof("[ORDER] Cancelling order for %s", order.Pair)
	err := c.exchange.Cancel(order)
	if err != nil {
		return err
	}

	order.Status = model.OrderStatusTypePendingCancel
	err = c.storage.UpdateOrder(&order)
	if err != nil {
		c.notifyError(err)
		return err
	}
	log.Infof("[ORDER CANCELED] %s", order)
	return nil
}

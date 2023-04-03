package exchange

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/adshao/go-binance/v2"
	"github.com/adshao/go-binance/v2/common"
	"github.com/jpillora/backoff"

	"github.com/rodrigo-brito/ninjabot/model"
	"github.com/rodrigo-brito/ninjabot/tools/log"
)

type MetadataFetchers func(pair string, t time.Time) (string, float64)

func init() {
	os.Setenv("HTTP_PROXY", "http://127.0.0.1:1087")
	os.Setenv("HTTPS_PROXY", "http://127.0.0.1:1087")
}

type Binance struct {
	ctx        context.Context
	client     *binance.Client
	assetsInfo map[string]model.AssetInfo
	HeikinAshi bool
	Testnet    bool

	APIKey    string
	APISecret string

	MetadataFetchers []MetadataFetchers
}

type BinanceOption func(*Binance)

// WithBinanceCredentials will set Binance credentials

func WithBinanceCredentials(key, secret string) BinanceOption {
	return func(b *Binance) {
		b.APIKey = key
		b.APISecret = secret
	}
}

// WithBinanceHeikinAshiCandle will convert candle to Heikin Ashi
func WithBinanceHeikinAshiCandle() BinanceOption {
	return func(b *Binance) {
		b.HeikinAshi = true
	}
}

// WithMetadataFetcher will execute a function after receive a new candle and include additional
// information to candle's metadata
// 执行函数
func WithMetadataFetcher(fetcher MetadataFetchers) BinanceOption {
	return func(b *Binance) {
		b.MetadataFetchers = append(b.MetadataFetchers, fetcher)
	}
}

// WithTestNet activate Bianance testnet
// 使用test网络
func WithTestNet() BinanceOption {
	return func(b *Binance) {
		binance.UseTestnet = true
	}
}

// NewBinance create a new Binance exchange instance
func NewBinance(ctx context.Context, options ...BinanceOption) (*Binance, error) {
	binance.WebsocketKeepalive = true
	exchange := &Binance{ctx: ctx}
	for _, option := range options {
		option(exchange)
	}

	// 客户端
	exchange.client = binance.NewClient(exchange.APIKey, exchange.APISecret)
	// ping
	err := exchange.client.NewPingService().Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("binance ping fail: %w", err)
	}

	results, err := exchange.client.NewExchangeInfoService().Do(ctx)
	if err != nil {
		return nil, err
	}

	// Initialize with orders precision and assets limits
	exchange.assetsInfo = make(map[string]model.AssetInfo)
	for _, info := range results.Symbols {
		tradeLimits := model.AssetInfo{
			BaseAsset:          info.BaseAsset,
			QuoteAsset:         info.QuoteAsset,
			BaseAssetPrecision: info.BaseAssetPrecision,
			QuotePrecision:     info.QuotePrecision,
		}
		for _, filter := range info.Filters {
			if typ, ok := filter["filterType"]; ok {
				if typ == string(binance.SymbolFilterTypeLotSize) {
					tradeLimits.MinQuantity, _ = strconv.ParseFloat(filter["minQty"].(string), 64)
					tradeLimits.MaxQuantity, _ = strconv.ParseFloat(filter["maxQty"].(string), 64)
					tradeLimits.StepSize, _ = strconv.ParseFloat(filter["stepSize"].(string), 64)
				}

				if typ == string(binance.SymbolFilterTypePriceFilter) {
					tradeLimits.MinPrice, _ = strconv.ParseFloat(filter["minPrice"].(string), 64)
					tradeLimits.MaxPrice, _ = strconv.ParseFloat(filter["maxPrice"].(string), 64)
					tradeLimits.TickSize, _ = strconv.ParseFloat(filter["tickSize"].(string), 64)
				}
			}
		}
		exchange.assetsInfo[info.Symbol] = tradeLimits
	}

	log.Info("[SETUP] Using Binance exchange")

	return exchange, nil
}

func (b *Binance) LastQuote(ctx context.Context, pair string) (float64, error) {
	// 最后一次报价
	candles, err := b.CandlesByLimit(ctx, pair, "1m", 1)
	if err != nil || len(candles) < 1 {
		return 0, err
	}
	return candles[0].Close, nil
}

func (b *Binance) AssetsInfo(pair string) model.AssetInfo {
	return b.assetsInfo[pair]
}

// 是否可以购买
func (b *Binance) validate(pair string, quantity float64) error {
	info, ok := b.assetsInfo[pair]
	if !ok {
		return ErrInvalidAsset
	}
	// 如果小于  或者大于
	if quantity > info.MaxQuantity || quantity < info.MinQuantity {
		return &OrderError{
			Err:      fmt.Errorf("%w: min: %f max: %f", ErrInvalidQuantity, info.MinQuantity, info.MaxQuantity),
			Pair:     pair,
			Quantity: quantity,
		}
	}

	return nil
}

/*

用于在 Binance 交易所创建一个新的 OCO（One-Cancels-the-Other）订单。OCO 订单是一种组合订单类型，其中包含两个子订单，一旦其中一个子订单成交，另一个子订单将自动取消。

在这段代码中：

b.client.NewCreateOCOService() 创建一个用于创建 OCO 订单的服务。
配置 OCO 订单的参数：
Side(binance.SideType(side))：设置订单的方向，买入（BUY）或卖出（SELL）。
Quantity(b.formatQuantity(pair, quantity))：设置订单的数量。
Price(b.formatPrice(pair, price))：设置限价订单的价格。
StopPrice(b.formatPrice(pair, stop))：设置止损价格。
StopLimitPrice(b.formatPrice(pair, stopLimit))：设置触发止损后的限价。
StopLimitTimeInForce(binance.TimeInForceTypeGTC)：设置止损限价订单的有效期（在这里为 GTC，即 Good-Til-Canceled，订单会一直有效直到被取消）。
Symbol(pair)：设置交易对（例如 "BTCUSDT"）。


假设你是一位加密货币交易者，正在交易 BTC/USDT 交易对。
当前 BTC 的价格为 50,000 USDT。你希望在 BTC 价格上涨时获利，
同时在价格下跌时限制损失。在这种情况下，
你可以使用 OCO 订单来实现这一策略。

以下是一个 OCO 订单的示例：

你预测 BTC 的价格将上涨，因此你计划在价格达到 52,000 USDT 时卖出 BTC，获得利润。
但是，如果市场不利，BTC 的价格下跌，你希望在价格跌至 48,000 USDT 时止损，以减少损失。
在这个例子中，你可以创建一个 OCO 订单，包含以下两个子订单：

限价卖单：价格为 52,000 USDT。如果 BTC 的价格达到或超过 52,000 USDT，这个订单将被触发并成交，实现获利。
止损限价卖单：止损价格为 48,000 USDT，限价为 47,500 USDT。如果 BTC 的价格跌至或低于 48,000 USDT，这个订单将被触发。在被触发后，系统将以 47,500 USDT 的价格挂出卖单。这样，一旦价格触发止损价，卖单会以限价或更好的价格成交，从而限制损失。
创建了 OCO 订单后，一旦其中一个子订单成交，另一个子订单将自动取消。例如，如果限价卖单在 52,000 USDT 成交，止损限价卖单将自动取消。反之亦然，如果止损限价卖单在 48,000 USDT 触发并成交，限价卖单将自动取消。

stop 是触发
stop limit 是挂单
通过这种方式，OCO 订单允许你在一个复合订单中设置获利和止损策略，确保只有一个策略被执行，而另一个策略被自动取消。这简化了订单管理，同时为你的交易提供了更好的风险控制。

*/

func (b *Binance) CreateOrderOCO(side model.SideType,
	pair string,
	quantity,
	price,
	stop,
	stopLimit float64) ([]model.Order, error) {

	// validate stop
	err := b.validate(pair, quantity)
	if err != nil {
		return nil, err
	}

	//OCO 订单是一种组合订单类型，其中包含两个子订单，一旦其中一个子订单成交，另一个子订单将自动取消。

	ocoOrder, err := b.client.NewCreateOCOService().
		Side(binance.SideType(side)).
		Quantity(b.formatQuantity(pair, quantity)).
		Price(b.formatPrice(pair, price)).
		StopPrice(b.formatPrice(pair, stop)).
		StopLimitPrice(b.formatPrice(pair, stopLimit)).
		StopLimitTimeInForce(binance.TimeInForceTypeGTC).
		Symbol(pair).
		Do(b.ctx)
	if err != nil {
		return nil, err
	}

	orders := make([]model.Order, 0, len(ocoOrder.Orders))
	for _, order := range ocoOrder.OrderReports {
		price, _ := strconv.ParseFloat(order.Price, 64)
		quantity, _ := strconv.ParseFloat(order.OrigQuantity, 64)
		item := model.Order{
			ExchangeID: order.OrderID,
			CreatedAt:  time.Unix(0, ocoOrder.TransactionTime*int64(time.Millisecond)),
			UpdatedAt:  time.Unix(0, ocoOrder.TransactionTime*int64(time.Millisecond)),
			Pair:       pair,
			Side:       model.SideType(order.Side),
			Type:       model.OrderType(order.Type),
			Status:     model.OrderStatusType(order.Status),
			Price:      price,
			Quantity:   quantity,
			GroupID:    &order.OrderListID,
		}

		if item.Type == model.OrderTypeStopLossLimit || item.Type == model.OrderTypeStopLoss {
			item.Stop = &stop
		}

		orders = append(orders, item)
	}

	return orders, nil
}

func (b *Binance) CreateOrderStop(pair string, quantity float64, limit float64) (model.Order, error) {
	err := b.validate(pair, quantity)
	if err != nil {
		return model.Order{}, err
	}

	order, err := b.client.NewCreateOrderService().Symbol(pair).
		Type(binance.OrderTypeStopLoss).
		TimeInForce(binance.TimeInForceTypeGTC).
		Side(binance.SideTypeSell).
		Quantity(b.formatQuantity(pair, quantity)).
		Price(b.formatPrice(pair, limit)).
		Do(b.ctx)
	if err != nil {
		return model.Order{}, err
	}

	price, _ := strconv.ParseFloat(order.Price, 64)
	quantity, _ = strconv.ParseFloat(order.OrigQuantity, 64)

	return model.Order{
		ExchangeID: order.OrderID,
		CreatedAt:  time.Unix(0, order.TransactTime*int64(time.Millisecond)),
		UpdatedAt:  time.Unix(0, order.TransactTime*int64(time.Millisecond)),
		Pair:       pair,
		Side:       model.SideType(order.Side),
		Type:       model.OrderType(order.Type),
		Status:     model.OrderStatusType(order.Status),
		Price:      price,
		Quantity:   quantity,
	}, nil
}

func (b *Binance) formatPrice(pair string, value float64) string {
	if info, ok := b.assetsInfo[pair]; ok {
		value = common.AmountToLotSize(info.TickSize, info.QuotePrecision, value)
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// 格式化数量
func (b *Binance) formatQuantity(pair string, value float64) string {
	if info, ok := b.assetsInfo[pair]; ok {
		value = common.AmountToLotSize(info.StepSize, info.BaseAssetPrecision, value)
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// 限价单
func (b *Binance) CreateOrderLimit(side model.SideType, pair string,
	quantity float64, limit float64) (model.Order, error) {

	err := b.validate(pair, quantity)
	if err != nil {
		return model.Order{}, err
	}

	order, err := b.client.NewCreateOrderService().
		Symbol(pair).
		Type(binance.OrderTypeLimit).
		TimeInForce(binance.TimeInForceTypeGTC).
		Side(binance.SideType(side)).
		Quantity(b.formatQuantity(pair, quantity)).
		Price(b.formatPrice(pair, limit)).
		Do(b.ctx)
	if err != nil {
		return model.Order{}, err
	}

	price, err := strconv.ParseFloat(order.Price, 64)
	if err != nil {
		return model.Order{}, err
	}

	quantity, err = strconv.ParseFloat(order.OrigQuantity, 64)
	if err != nil {
		return model.Order{}, err
	}

	// 返回订单
	return model.Order{
		ExchangeID: order.OrderID,
		CreatedAt:  time.Unix(0, order.TransactTime*int64(time.Millisecond)),
		UpdatedAt:  time.Unix(0, order.TransactTime*int64(time.Millisecond)),
		Pair:       pair,
		Side:       model.SideType(order.Side),
		Type:       model.OrderType(order.Type),
		Status:     model.OrderStatusType(order.Status),
		Price:      price,
		Quantity:   quantity,
	}, nil
}

/*
这两个方法的主要区别在于市价订单的执行方式。这里是两个方法的主要区别：

CreateOrderMarket：这个方法用于创建基于订单数量（quantity）的市价订单。在这种情况下，
您需要指定购买或出售的基本资产数量。例如，如果您要在BTC/USDT交易对上以市场价格购买0.01个BTC，那么您需要使用这个方法，
并将quantity设置为0.01。
CreateOrderMarketQuote：这个方法用于创建基于报价资产订单数量（quantity）的市价订单。
在这种情况下，您需要指定购买或出售的报价资产数量。例如，如果您要在BTC/USDT交易对上以市场价格购买价值100 USDT的BTC，
那么您需要使用这个方法，并将quantity设置为100。
在这两种情况下，创建市价订单的主要逻辑相同，但在CreateOrderMarket方法中，
您需要使用.Quantity()设置订单数量，而在CreateOrderMarketQuote方法中，您需要使用.QuoteOrderQty()设置报价资产订单数量。
返回的model.Order对象在两个方法中都是相似的。

总之，CreateOrderMarket和CreateOrderMarketQuote方法的区别在于它们处理市价订单的方式。前者基于基本资产的数量，后者基于报价资产的数量。根据您的交易需求选择合适的方法。
*/

func (b *Binance) CreateOrderMarket(side model.SideType, pair string, quantity float64) (model.Order, error) {
	err := b.validate(pair, quantity)
	if err != nil {
		return model.Order{}, err
	}

	order, err := b.client.NewCreateOrderService().
		Symbol(pair).
		Type(binance.OrderTypeMarket).
		Side(binance.SideType(side)).
		Quantity(b.formatQuantity(pair, quantity)).
		NewOrderRespType(binance.NewOrderRespTypeFULL).
		Do(b.ctx)
	if err != nil {
		return model.Order{}, err
	}

	cost, err := strconv.ParseFloat(order.CummulativeQuoteQuantity, 64)
	if err != nil {
		return model.Order{}, err
	}

	quantity, err = strconv.ParseFloat(order.ExecutedQuantity, 64)
	if err != nil {
		return model.Order{}, err
	}

	return model.Order{
		ExchangeID: order.OrderID,
		CreatedAt:  time.Unix(0, order.TransactTime*int64(time.Millisecond)),
		UpdatedAt:  time.Unix(0, order.TransactTime*int64(time.Millisecond)),
		Pair:       order.Symbol,
		Side:       model.SideType(order.Side),
		Type:       model.OrderType(order.Type),
		Status:     model.OrderStatusType(order.Status),
		Price:      cost / quantity,
		Quantity:   quantity,
	}, nil
}

func (b *Binance) CreateOrderMarketQuote(side model.SideType, pair string, quantity float64) (model.Order, error) {
	err := b.validate(pair, quantity)
	if err != nil {
		return model.Order{}, err
	}

	order, err := b.client.NewCreateOrderService().
		Symbol(pair).
		Type(binance.OrderTypeMarket).
		Side(binance.SideType(side)).
		QuoteOrderQty(b.formatQuantity(pair, quantity)).
		NewOrderRespType(binance.NewOrderRespTypeFULL).
		Do(b.ctx)
	if err != nil {
		return model.Order{}, err
	}

	cost, err := strconv.ParseFloat(order.CummulativeQuoteQuantity, 64)
	if err != nil {
		return model.Order{}, err
	}

	quantity, err = strconv.ParseFloat(order.ExecutedQuantity, 64)
	if err != nil {
		return model.Order{}, err
	}

	return model.Order{
		ExchangeID: order.OrderID,
		CreatedAt:  time.Unix(0, order.TransactTime*int64(time.Millisecond)),
		UpdatedAt:  time.Unix(0, order.TransactTime*int64(time.Millisecond)),
		Pair:       order.Symbol,
		Side:       model.SideType(order.Side),
		Type:       model.OrderType(order.Type),
		Status:     model.OrderStatusType(order.Status),
		Price:      cost / quantity,
		Quantity:   quantity,
	}, nil
}

func (b *Binance) Cancel(order model.Order) error {
	_, err := b.client.NewCancelOrderService().
		Symbol(order.Pair).
		OrderID(order.ExchangeID).
		Do(b.ctx)
	return err
}

func (b *Binance) Orders(pair string, limit int) ([]model.Order, error) {
	result, err := b.client.NewListOrdersService().
		Symbol(pair).
		Limit(limit).
		Do(b.ctx)

	if err != nil {
		return nil, err
	}

	orders := make([]model.Order, 0)
	for _, order := range result {
		orders = append(orders, newOrder(order))
	}
	return orders, nil
}

func (b *Binance) Order(pair string, id int64) (model.Order, error) {
	order, err := b.client.NewGetOrderService().
		Symbol(pair).
		OrderID(id).
		Do(b.ctx)

	if err != nil {
		return model.Order{}, err
	}

	return newOrder(order), nil
}

func newOrder(order *binance.Order) model.Order {
	var price float64
	cost, _ := strconv.ParseFloat(order.CummulativeQuoteQuantity, 64)
	quantity, _ := strconv.ParseFloat(order.ExecutedQuantity, 64)
	if cost > 0 && quantity > 0 {
		price = cost / quantity
	} else {
		price, _ = strconv.ParseFloat(order.Price, 64)
		quantity, _ = strconv.ParseFloat(order.OrigQuantity, 64)
	}

	return model.Order{
		ExchangeID: order.OrderID,
		Pair:       order.Symbol,
		CreatedAt:  time.Unix(0, order.Time*int64(time.Millisecond)),
		UpdatedAt:  time.Unix(0, order.UpdateTime*int64(time.Millisecond)),
		Side:       model.SideType(order.Side),
		Type:       model.OrderType(order.Type),
		Status:     model.OrderStatusType(order.Status),
		Price:      price,
		Quantity:   quantity,
	}
}

func (b *Binance) Account() (model.Account, error) {
	acc, err := b.client.NewGetAccountService().Do(b.ctx)
	if err != nil {
		return model.Account{}, err
	}

	balances := make([]model.Balance, 0)
	for _, balance := range acc.Balances {
		free, err := strconv.ParseFloat(balance.Free, 64)
		if err != nil {
			return model.Account{}, err
		}
		locked, err := strconv.ParseFloat(balance.Locked, 64)
		if err != nil {
			return model.Account{}, err
		}
		balances = append(balances, model.Balance{
			Asset: balance.Asset,
			Free:  free,
			Lock:  locked,
		})
	}

	return model.Account{
		Balances: balances,
	}, nil
}

/*
这是一个在Binance交易所获取特定交易对头寸的函数。
函数首先通过调用 SplitAssetQuote 函数将交易对拆分为资产和计价货币。
然后，它调用 Account 函数来获取当前账户的余额信息，并使用 Balance 函数获取该交易对的资产和计价货币的余额。
最后，函数将这些余额相加并返回资产和计价货币的总余额。
*/
// 仓位信息
func (b *Binance) Position(pair string) (asset, quote float64, err error) {
	// 资产  和  报价
	assetTick, quoteTick := SplitAssetQuote(pair)
	// 得到账户信息
	acc, err := b.Account()
	if err != nil {
		return 0, 0, err
	}

	assetBalance, quoteBalance := acc.Balance(assetTick, quoteTick)

	// 得到资产  和 报价资产
	return assetBalance.Free + assetBalance.Lock, quoteBalance.Free + quoteBalance.Lock, nil
}

/*
这个函数用于订阅Binance的K线数据流，并返回一个包含接收到的K线数据的通道和一个错误通道。
它需要一个上下文对象作为参数，用于在需要时取消订阅。它还需要一个交易对和一个K线周期作为参数
。在内部，它使用Binance Go SDK提供的函数来建立WebSocket连接，并将接收到的K线数据转换为模型中的Candle对象，
并发送到Candle通道中。如果有任何错误发生，它会发送到错误通道中。
*/

func (b *Binance) CandlesSubscription(ctx context.Context, pair, period string) (chan model.Candle, chan error) {
	ccandle := make(chan model.Candle)
	cerr := make(chan error)
	ha := model.NewHeikinAshi()

	go func() {
		ba := &backoff.Backoff{
			Min: 100 * time.Millisecond,
			Max: 1 * time.Second,
		}

		for {
			done, _, err := binance.WsKlineServe(pair, period, func(event *binance.WsKlineEvent) {
				ba.Reset()
				candle := CandleFromWsKline(pair, event.Kline)

				if candle.Complete && b.HeikinAshi {
					candle = candle.ToHeikinAshi(ha)
				}

				if candle.Complete {
					// fetch aditional data if needed
					for _, fetcher := range b.MetadataFetchers {
						key, value := fetcher(pair, candle.Time)
						candle.Metadata[key] = value
					}
				}

				ccandle <- candle

			}, func(err error) {
				cerr <- err
			})
			if err != nil {
				cerr <- err
				close(cerr)
				close(ccandle)
				return
			}

			select {
			case <-ctx.Done():
				close(cerr)
				close(ccandle)
				return
			case <-done:
				time.Sleep(ba.Duration())
			}
		}
	}()

	return ccandle, cerr
}

func (b *Binance) CandlesByLimit(ctx context.Context, pair, period string, limit int) ([]model.Candle, error) {
	candles := make([]model.Candle, 0)
	klineService := b.client.NewKlinesService()
	ha := model.NewHeikinAshi()

	data, err := klineService.Symbol(pair).
		Interval(period).
		Limit(limit + 1).
		Do(ctx)

	if err != nil {
		return nil, err
	}

	for _, d := range data {
		// kline转化为  CandleFromKline
		candle := CandleFromKline(pair, *d)

		if b.HeikinAshi {
			candle = candle.ToHeikinAshi(ha)
		}

		candles = append(candles, candle)
	}

	// discard last candle, because it is incomplete
	return candles[:len(candles)-1], nil
}

func (b *Binance) CandlesByPeriod(ctx context.Context, pair, period string,
	start, end time.Time) ([]model.Candle, error) {

	candles := make([]model.Candle, 0)
	klineService := b.client.NewKlinesService()
	ha := model.NewHeikinAshi()

	data, err := klineService.Symbol(pair).
		Interval(period).
		StartTime(start.UnixNano() / int64(time.Millisecond)).
		EndTime(end.UnixNano() / int64(time.Millisecond)).
		Do(ctx)

	if err != nil {
		return nil, err
	}

	for _, d := range data {
		candle := CandleFromKline(pair, *d)

		if b.HeikinAshi {
			candle = candle.ToHeikinAshi(ha)
		}

		candles = append(candles, candle)
	}

	return candles, nil
}

func CandleFromKline(pair string, k binance.Kline) model.Candle {
	t := time.Unix(0, k.OpenTime*int64(time.Millisecond))
	candle := model.Candle{Pair: pair, Time: t, UpdatedAt: t}
	candle.Open, _ = strconv.ParseFloat(k.Open, 64)
	candle.Close, _ = strconv.ParseFloat(k.Close, 64)
	candle.High, _ = strconv.ParseFloat(k.High, 64)
	candle.Low, _ = strconv.ParseFloat(k.Low, 64)
	candle.Volume, _ = strconv.ParseFloat(k.Volume, 64)
	candle.Complete = true
	candle.Metadata = make(map[string]float64)
	return candle
}

func CandleFromWsKline(pair string, k binance.WsKline) model.Candle {
	t := time.Unix(0, k.StartTime*int64(time.Millisecond))
	candle := model.Candle{Pair: pair, Time: t, UpdatedAt: t}
	candle.Open, _ = strconv.ParseFloat(k.Open, 64)
	candle.Close, _ = strconv.ParseFloat(k.Close, 64)
	candle.High, _ = strconv.ParseFloat(k.High, 64)
	candle.Low, _ = strconv.ParseFloat(k.Low, 64)
	candle.Volume, _ = strconv.ParseFloat(k.Volume, 64)
	candle.Complete = k.IsFinal
	candle.Metadata = make(map[string]float64)
	return candle
}

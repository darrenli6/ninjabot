package exchange

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/common"

	"github.com/rodrigo-brito/ninjabot/model"
	"github.com/rodrigo-brito/ninjabot/service"
	"github.com/rodrigo-brito/ninjabot/tools/log"
)

type assetInfo struct {
	Free float64
	Lock float64
}

type AssetValue struct {
	Time  time.Time
	Value float64
}

type PaperWallet struct {
	sync.Mutex
	ctx           context.Context
	baseCoin      string
	counter       int64
	takerFee      float64
	makerFee      float64
	initialValue  float64
	feeder        service.Feeder
	orders        []model.Order
	assets        map[string]*assetInfo
	avgShortPrice map[string]float64
	avgLongPrice  map[string]float64
	volume        map[string]float64
	lastCandle    map[string]model.Candle
	fistCandle    map[string]model.Candle
	assetValues   map[string][]AssetValue
	equityValues  []AssetValue
}

func (p *PaperWallet) AssetsInfo(pair string) model.AssetInfo {
	asset, quote := SplitAssetQuote(pair)
	return model.AssetInfo{
		BaseAsset:          asset,
		QuoteAsset:         quote,
		MaxPrice:           math.MaxFloat64,
		MaxQuantity:        math.MaxFloat64,
		StepSize:           0.00000001,
		TickSize:           0.00000001,
		QuotePrecision:     8,
		BaseAssetPrecision: 8,
	}
}

type PaperWalletOption func(*PaperWallet)

func WithPaperAsset(pair string, amount float64) PaperWalletOption {
	return func(wallet *PaperWallet) {
		wallet.assets[pair] = &assetInfo{
			Free: amount,
			Lock: 0,
		}
	}
}

func WithPaperFee(maker, taker float64) PaperWalletOption {
	return func(wallet *PaperWallet) {
		wallet.makerFee = maker
		wallet.takerFee = taker
	}
}

func WithDataFeed(feeder service.Feeder) PaperWalletOption {
	return func(wallet *PaperWallet) {
		wallet.feeder = feeder
	}
}

func NewPaperWallet(ctx context.Context, baseCoin string, options ...PaperWalletOption) *PaperWallet {
	wallet := PaperWallet{
		ctx:           ctx,
		baseCoin:      baseCoin,
		orders:        make([]model.Order, 0),
		assets:        make(map[string]*assetInfo),
		fistCandle:    make(map[string]model.Candle),
		lastCandle:    make(map[string]model.Candle),
		avgShortPrice: make(map[string]float64),
		avgLongPrice:  make(map[string]float64),
		volume:        make(map[string]float64),
		assetValues:   make(map[string][]AssetValue),
		equityValues:  make([]AssetValue, 0),
	}

	for _, option := range options {
		option(&wallet)
	}

	wallet.initialValue = wallet.assets[wallet.baseCoin].Free
	log.Info("[SETUP] Using paper wallet")
	log.Infof("[SETUP] Initial Portfolio = %f %s", wallet.initialValue, wallet.baseCoin)

	return &wallet
}

func (p *PaperWallet) ID() int64 {
	p.counter++
	return p.counter
}

func (p *PaperWallet) Pairs() []string {
	pairs := make([]string, 0)
	for pair := range p.assets {
		pairs = append(pairs, pair)
	}
	return pairs
}

func (p *PaperWallet) LastQuote(ctx context.Context, pair string) (float64, error) {
	return p.feeder.LastQuote(ctx, pair)
}

func (p *PaperWallet) AssetValues(pair string) []AssetValue {
	return p.assetValues[pair]
}

func (p *PaperWallet) EquityValues() []AssetValue {
	return p.equityValues
}

/*
这是一个Golang代码片段，定义了一个名为 MaxDrawdown 的方法。这个方法计算了一个名为
PaperWallet 的结构体实例的最大回撤，并返回最大回撤的百分比、
开始时间和结束时间。这个方法对于评估投资组合在某段时间内的最大价值损失非常有用。

让我们一步一步地了解这个方法的实现：

func (p *PaperWallet) MaxDrawdown() (float64, time.Time, time.Time):
定义了一个名为 MaxDrawdown 的方法，它接收一个 *PaperWallet 类型的指针，并返回三个值：一个浮点数（最大回撤百分比）、两个 time.Time 类型（开始时间和结束时间）。
如果 p.equityValues 数组的长度小于1，说明没有足够的数据来计算最大回撤，
方法将返回0和两个空的时间值。
*/
func (p *PaperWallet) MaxDrawdown() (float64, time.Time, time.Time) {
	if len(p.equityValues) < 1 {
		return 0, time.Time{}, time.Time{}
	}

	localMin := math.MaxFloat64
	localMinBase := p.equityValues[0].Value
	localMinStart := p.equityValues[0].Time
	localMinEnd := p.equityValues[0].Time

	globalMin := localMin
	globalMinBase := localMinBase
	globalMinStart := localMinStart
	globalMinEnd := localMinEnd

	for i := 1; i < len(p.equityValues); i++ {
		diff := p.equityValues[i].Value - p.equityValues[i-1].Value

		if localMin > 0 {
			localMin = diff
			localMinBase = p.equityValues[i-1].Value
			localMinStart = p.equityValues[i-1].Time
			localMinEnd = p.equityValues[i].Time
		} else {
			localMin += diff
			localMinEnd = p.equityValues[i].Time
		}

		if localMin < globalMin {
			globalMin = localMin
			globalMinBase = localMinBase
			globalMinStart = localMinStart
			globalMinEnd = localMinEnd
		}
	}

	return globalMin / globalMinBase, globalMinStart, globalMinEnd
}

func (p *PaperWallet) Summary() {
	var (
		total        float64
		marketChange float64
		volume       float64
	)

	fmt.Println("-- FINAL WALLET --")
	for pair := range p.lastCandle {
		asset, quote := SplitAssetQuote(pair)
		quantity := p.assets[asset].Free + p.assets[asset].Lock
		value := quantity * p.lastCandle[pair].Close
		if quantity < 0 {
			totalShort := 2.0*p.avgShortPrice[pair]*quantity - p.lastCandle[pair].Close*quantity
			value = math.Abs(totalShort)
		}
		total += value
		marketChange += (p.lastCandle[pair].Close - p.fistCandle[pair].Close) / p.fistCandle[pair].Close
		fmt.Printf("%.4f %s = %.4f %s\n", quantity, asset, total, quote)
	}

	avgMarketChange := marketChange / float64(len(p.lastCandle))
	baseCoinValue := p.assets[p.baseCoin].Free + p.assets[p.baseCoin].Lock
	profit := total + baseCoinValue - p.initialValue
	fmt.Printf("%.4f %s\n", baseCoinValue, p.baseCoin)
	fmt.Println()
	maxDrawDown, _, _ := p.MaxDrawdown()
	fmt.Println("----- RETURNS -----")
	fmt.Printf("START PORTFOLIO     = %.2f %s\n", p.initialValue, p.baseCoin)
	fmt.Printf("FINAL PORTFOLIO     = %.2f %s\n", total+baseCoinValue, p.baseCoin)
	fmt.Printf("GROSS PROFIT        =  %f %s (%.2f%%)\n", profit, p.baseCoin, profit/p.initialValue*100)
	fmt.Printf("MARKET CHANGE (B&H) =  %.2f%%\n", avgMarketChange*100)
	fmt.Println()
	fmt.Println("------ RISK -------")
	fmt.Printf("MAX DRAWDOWN = %.2f %%\n", maxDrawDown*100)
	fmt.Println()
	fmt.Println("------ VOLUME -----")
	for pair, vol := range p.volume {
		volume += vol
		fmt.Printf("%s         = %.2f %s\n", pair, vol, p.baseCoin)
	}
	fmt.Printf("TOTAL           = %.2f %s\n", volume, p.baseCoin)
	fmt.Println("-------------------")
}

/*
validateFunds 的 Golang 函数，它属于 PaperWallet 结构体。此函数检查并更新一个虚拟（paper）交易账户的资产信息。当执行买入或卖出操作时，它将验证是否有足够的资金来完成交易，更新账户中资产的锁定和可用资金，并在交易成功时更新资产的平均价格。

函数参数：

side：交易方向（买入或卖出）。
pair：交易的资产对，如 "BTC/USDT"。
amount：交易数量。
value：交易的价格。
fill：布尔值，表示交易是否被完全成交。
函数首先将交易对拆分为资产和报价货币。然后，如果资产或报价货币在账户中不存在，函数将为其创建一个新的 assetInfo 结构体实例。

接下来，根据交易方向（side），函数将执行以下操作：

如果是卖出操作：
a. 计算可用资金。
b. 检查是否有足够的资金来完成交易。如果没有足够的资金，将返回一个包含 ErrInsufficientFunds 错误的 OrderError。
c. 计算要锁定的资产和报价货币的数量。
d. 从可用资产中扣除锁定的资产和报价货币。
e. 如果交易已完全成交（fill 为 true），则更新平均价格，根据交易类型（开仓或平仓）更新资产和报价货币的可用数量。
f. 如果交易未完全成交（fill 为 false），则将锁定的资产和报价货币添加到锁定资产中。

如果是买入操作：
a. 计算可用资金，如果有空头仓位，将空头仓位的清算价值计算进去。
b. 计算要购买的数量，如果有空头仓位，需加上空头仓位的数量。
c. 检查是否有足够的资金来完成交易。如果没有足够的资金，将返回一个包含 ErrInsufficientFunds 错误的 OrderError。
d. 计算要锁定的资产和报价货币的数量，同时考虑空头仓位的清算价值。
e. 向可用资产中添加锁定的资产，从可用报价货币中扣除锁定的报价货币。
f. 如果交易已完全成交（fill 为 true），则更新平均价格，将购买的数量加到可用资产中（扣除锁定的资产）。
g. 如果交易未完全成交（fill 为 false），则将锁定的资产和报价货币添加到锁定资产中。

举个例子：

假设你的 PaperWallet 包含以下资产：

BTC：1（可用）
USDT：5000（可用）
现在你想要进行一次卖出操作（side 为卖出），交易对为 "BTC/USDT"，数量（amount）为 0.5，价格（value）为 10000 USDT，假设交易已经完全成交（fill 为 true）。

在执行 validateFunds 函数后，PaperWallet 的资产将发生如下变化：

BTC：0.5（可用）
USDT：10000（可用）
这是因为在卖出操作中，你已经将 0.5 BTC 卖出，以每个 10000 USDT 的价格出售。因此，你的 BTC 数量减少了 0.5，而 USDT 数量增加了 0.5 * 10000 = 5000。

validateFunds 函数在此过程中执行了以下操作：

验证交易对中的资产（BTC）和报价货币（USDT）在 PaperWallet 中存在。
计算在卖出操作中可用的总资金。
验证是否有足够的资金来完成交易。
更新资产和报价货币的可用和锁定数量。
由于交易已完全成交（fill 为true），所以更新了 BTC/USDT 的平均价格。
如果 fill 为 false，表示交易尚未完全成交，validateFunds 将锁定相应的资产和报价货币，以防止在等待交易完成时进行其他交易。

总之，validateFunds 函数用于验证和更新虚拟交易账户的资产，在执行买入或卖出操作时确保有足够的资金。通过锁定资产和报价货币以及更新平均价格，此函数模拟了实际交易场景。







*/

func (p *PaperWallet) validateFunds(side model.SideType, pair string, amount, value float64, fill bool) error {
	asset, quote := SplitAssetQuote(pair)
	if _, ok := p.assets[asset]; !ok {
		p.assets[asset] = &assetInfo{}
	}

	if _, ok := p.assets[quote]; !ok {
		p.assets[quote] = &assetInfo{}
	}

	funds := p.assets[quote].Free
	if side == model.SideTypeSell {
		if p.assets[asset].Free > 0 {
			funds += p.assets[asset].Free * value
		}

		if funds < amount*value {
			return &OrderError{
				Err:      ErrInsufficientFunds,
				Pair:     pair,
				Quantity: amount,
			}
		}

		lockedAsset := math.Min(math.Max(p.assets[asset].Free, 0), amount) // ignore negative asset amount to lock
		lockedQuote := (amount - lockedAsset) * value

		p.assets[asset].Free -= lockedAsset
		p.assets[quote].Free -= lockedQuote
		if fill {
			p.updateAveragePrice(side, pair, amount, value)
			if lockedQuote > 0 { // entering in short position
				p.assets[asset].Free -= amount
			} else { // liquidating long position
				p.assets[quote].Free += amount * value

			}
		} else {
			p.assets[asset].Lock += lockedAsset
			p.assets[quote].Lock += lockedQuote
		}

		log.Debugf("%s -> LOCK = %f / FREE %f", asset, p.assets[asset].Lock, p.assets[asset].Free)
	} else { // SideTypeBuy
		var liquidShortValue float64
		if p.assets[asset].Free < 0 {
			v := math.Abs(p.assets[asset].Free)
			liquidShortValue = 2*v*p.avgShortPrice[pair] - v*value // liquid price of short position
			funds += liquidShortValue
		}

		amountToBuy := amount
		if p.assets[asset].Free < 0 {
			amountToBuy = amount + p.assets[asset].Free
		}

		if funds < amountToBuy*value {
			return &OrderError{
				Err:      ErrInsufficientFunds,
				Pair:     pair,
				Quantity: amount,
			}
		}

		lockedAsset := math.Min(-math.Min(p.assets[asset].Free, 0), amount) // ignore positive amount to lock
		lockedQuote := (amount-lockedAsset)*value - liquidShortValue

		p.assets[asset].Free += lockedAsset
		p.assets[quote].Free -= lockedQuote

		if fill {
			p.updateAveragePrice(side, pair, amount, value)
			p.assets[asset].Free += amount - lockedAsset
		} else {
			p.assets[asset].Lock += lockedAsset
			p.assets[quote].Lock += lockedQuote
		}
		log.Debugf("%s -> LOCK = %f / FREE %f", asset, p.assets[asset].Lock, p.assets[asset].Free)
	}

	return nil
}

/*
更新一个虚拟交易（paper trading）钱包的平均价格。虚拟交易是一种在不实际进行交易的情况下进行模拟的交易方法，通常用于测试交易策略。

这个功能的主要目的是在执行买入和卖出操作时更新特定交易对的平均多头和空头价格。这里的代码有两个完全相同的函数，可以删除其中之一。

以下是代码中涉及到的一些关键概念：

actualQty: 表示当前钱包中特定资产的实际数量。
avgLongPrice: 存储每个交易对的多头平均价格。
avgShortPrice: 存储每个交易对的空头平均价格。
函数的参数是：

side: 交易的类型（买入或卖出）。
pair: 交易对amount: 交易的数量。
value: 交易的价格。
函数按照以下步骤处理：

首先，如果钱包中没有持有该资产，则将平均多头或空头价格设置为交易价格。
如果钱包中持有多头仓位且执行买入操作，计算新的多头平均价格。
如果钱包中持有多头仓位且执行卖出操作，计算交易的利润值，如果卖出数量不足以关闭整个多头仓位，则不进行任何操作。如果卖出数量足以关闭多头仓位，则设置空头平均价格。
如果钱包中持有空头仓位且执行卖出操作，计算新的空头平均价格。
如果钱包中持有空头仓位且执行买入操作，计算交易的利润值，如果买入数量不足以关闭整个空头仓位，则不进行任何操作。如果买入数量足以关闭空头仓位，则设置多头平均价格。
以一个实际例子来说明：
虚拟交易钱包，开始时没有任何资产。用户计划在 BTC/USD 交易对上进行操作。钱包的初始状态如下：

实际数量（actualQty）：0
多头平均价格（avgLongPrice）：0
空头平均价格（avgShortPrice）：0
现在，用户执行以下操作：

买入 1 BTC，价格为 10,000 USD。函数将更新多头平均价格为 10,000 USD，因为实际数量为 0。
再买入 1 BTC，价格为 11,000 USD。函数将重新计算多头平均价格为：(10,000 * 1 + 11,000 * 1) / (1 + 1) = 10,500 USD。
卖出 1 BTC，价格为 12,000 USD。函数将计算利润值：1 * 12,000 - 1 * 10,500 = 1,500 USD，并记录这个利润。因为卖出数量
不足以关闭整个多头仓位（只有1 BTC），所以不会更新空头平均价格。

接下来，用户再卖出 2 BTC，价格为 13,000 USD。函数首先计算利润值：1 * 13,000 - 1 * 10,500 = 2,500 USD，并记录这个利润。由于卖出数量足以关闭多头仓位（1 BTC），此时钱包将进入空头仓位。函数将设置空头平均价格为 13,000 USD。

现在，用户继续执行卖出操作，卖出 1 BTC，价格为 14,000 USD。函数将重新计算空头平均价格为：(13,000 * 1 + 14,000 * 1) / (1 + 1) = 13,500 USD。

最后，用户购买 1 BTC，价格为 12,500 USD。函数将计算利润值：1 * 13,500 - 1 * 12,500 = 1,000 USD，并记录这个利润。因为买入数量不足以关闭
整个空头仓位（2 BTC），所以不会更新多头平均价格。

在整个过程中，这个函数通过计算利润值来追踪用户在多头和空头仓位上的交易利润。同样，它还更新平均多头价格和平均空头价格，以便在执行交易时考虑到钱包中的实际仓位。

需要注意的是，在这段代码中，有一个 TODO 注释提到了存储利润。实际上，在将这段代码应用于实际项目时，您可能希望记录并分析这些利润数据，以便了解交易策略的效果。

总之，这个函数是用来在执行虚拟交易时更新多头和空头仓位的平均价格，并在适当时机计算并记录交易利润。这对于测试交易策略以及评估其盈利能力非常有用。


*/

func (p *PaperWallet) updateAveragePrice(side model.SideType, pair string, amount, value float64) {
	actualQty := 0.0
	asset, quote := SplitAssetQuote(pair)

	if p.assets[asset] != nil {
		actualQty = p.assets[asset].Free
	}

	// without previous position
	if actualQty == 0 {
		if side == model.SideTypeBuy {
			p.avgLongPrice[pair] = value
		} else {
			p.avgShortPrice[pair] = value
		}
		return
	}

	// actual long + order buy
	if actualQty > 0 && side == model.SideTypeBuy {
		positionValue := p.avgLongPrice[pair] * actualQty
		p.avgLongPrice[pair] = (positionValue + amount*value) / (actualQty + amount)
		return
	}

	// actual long + order sell
	if actualQty > 0 && side == model.SideTypeSell {
		profitValue := amount*value - math.Min(amount, actualQty)*p.avgLongPrice[pair]
		percentage := profitValue / (amount * p.avgLongPrice[pair])
		log.Infof("PROFIT = %.4f %s (%.2f %%)", profitValue, quote, percentage*100.0) // TODO: store profits

		if amount <= actualQty { // not enough quantity to close the position
			return
		}

		p.avgShortPrice[pair] = value

		return
	}

	// actual short + order sell
	if actualQty < 0 && side == model.SideTypeSell {
		positionValue := p.avgShortPrice[pair] * -actualQty
		p.avgShortPrice[pair] = (positionValue + amount*value) / (-actualQty + amount)

		return
	}

	// actual short + order buy
	if actualQty < 0 && side == model.SideTypeBuy {
		profitValue := math.Min(amount, -actualQty)*p.avgShortPrice[pair] - amount*value
		percentage := profitValue / (amount * p.avgShortPrice[pair])
		log.Infof("PROFIT = %.4f %s (%.2f %%)", profitValue, quote, percentage*100.0) // TODO: store profits

		if amount <= -actualQty { // not enough quantity to close the position
			return
		}

		p.avgLongPrice[pair] = value
	}
}

/*
这个函数是一个回调函数，用于在每个K线（蜡烛）到达时更新虚拟交易钱包的状态。具体而言，它会执行以下操作：

更新最新的蜡烛（candle）和第一个蜡烛（firstCandle）。
遍历当前订单列表，对于与蜡烛对应交易对（Pair）相同且状态为“New”的订单，执行以下操作：
如果是买入订单并且价格大于等于蜡烛的收盘价，则将订单状态更新为“Filled”，更新订单量和交易量，更新资产大小和平均价格。
如果是卖出订单，则根据订单类型和价格与蜡烛的最高价和最低价进行比较，如果满足条件，则将订单状态更新为“Filled”，更新订单量和交易量，更新资产大小和平均价格。如果该订单属于某一组，取消组中除自身之外的其他订单。
如果蜡烛已完成，则计算所有资产的价值和钱包的净值，并将结果添加到资产价值历史和净值历史中。
下面是一个示例，展示了如何使用该函数更新虚拟交易钱包的状态：

假设有一个虚拟交易钱包，钱包中有一些资产，并且有一些订单尚未执行。当每个新的K线（蜡烛）到达时，我们希望更新钱包的状态，以便我们可以跟踪交易的利润和钱包的净值。

假设我们当前在 BTC/USD 交易对上执行一组订单，其中一个限价卖单的价格为 10,000 USD。当蜡烛的收盘价小于 10,000 USD 时，我们期望该订单状态保持为“New”，直到价格上升到 10,000 USD 时才将其标记为“Filled”。

现在，当新的 BTC/USD 蜡烛到达时，我们调用 OnCandle 函数。函数首先更新最新的蜡烛和第一个蜡烛。然后，它遍历当前订单列表并查找 BTC/USD 订单。由于当前的蜡烛收盘价低于 10,000 USD，该订单的状态不会被更新。在计算资产价值和净值之后，我们将得到更新的钱包状态。






*/

func (p *PaperWallet) OnCandle(candle model.Candle) {
	p.Lock()
	defer p.Unlock()

	p.lastCandle[candle.Pair] = candle
	if _, ok := p.fistCandle[candle.Pair]; !ok {
		p.fistCandle[candle.Pair] = candle
	}

	for i, order := range p.orders {
		if order.Pair != candle.Pair || order.Status != model.OrderStatusTypeNew {
			continue
		}

		if _, ok := p.volume[candle.Pair]; !ok {
			p.volume[candle.Pair] = 0
		}

		asset, quote := SplitAssetQuote(order.Pair)
		if order.Side == model.SideTypeBuy && order.Price >= candle.Close {
			if _, ok := p.assets[asset]; !ok {
				p.assets[asset] = &assetInfo{}
			}

			p.volume[candle.Pair] += order.Price * order.Quantity
			p.orders[i].UpdatedAt = candle.Time
			p.orders[i].Status = model.OrderStatusTypeFilled

			// update assets size
			p.updateAveragePrice(order.Side, order.Pair, order.Quantity, order.Price)
			p.assets[asset].Free = p.assets[asset].Free + order.Quantity
			p.assets[quote].Lock = p.assets[quote].Lock - order.Price*order.Quantity
		}

		if order.Side == model.SideTypeSell {
			var orderPrice float64
			if (order.Type == model.OrderTypeLimit ||
				order.Type == model.OrderTypeLimitMaker ||
				order.Type == model.OrderTypeTakeProfit ||
				order.Type == model.OrderTypeTakeProfitLimit) &&
				candle.High >= order.Price {
				orderPrice = order.Price
			} else if (order.Type == model.OrderTypeStopLossLimit ||
				order.Type == model.OrderTypeStopLoss) &&
				candle.Low <= *order.Stop {
				orderPrice = *order.Stop
			} else {
				continue
			}

			// Cancel other orders from same group
			if order.GroupID != nil {
				for j, groupOrder := range p.orders {
					if groupOrder.GroupID != nil && *groupOrder.GroupID == *order.GroupID &&
						groupOrder.ExchangeID != order.ExchangeID {
						p.orders[j].Status = model.OrderStatusTypeCanceled
						p.orders[j].UpdatedAt = candle.Time
						break
					}
				}
			}

			if _, ok := p.assets[quote]; !ok {
				p.assets[quote] = &assetInfo{}
			}

			orderVolume := order.Quantity * orderPrice

			p.volume[candle.Pair] += orderVolume
			p.orders[i].UpdatedAt = candle.Time
			p.orders[i].Status = model.OrderStatusTypeFilled

			// update assets size
			p.updateAveragePrice(order.Side, order.Pair, order.Quantity, orderPrice)
			p.assets[asset].Lock = p.assets[asset].Lock - order.Quantity
			p.assets[quote].Free = p.assets[quote].Free + order.Quantity*orderPrice
		}
	}

	if candle.Complete {
		var total float64
		for asset, info := range p.assets {
			amount := info.Free + info.Lock
			pair := strings.ToUpper(asset + p.baseCoin)
			if amount < 0 {
				v := math.Abs(amount)
				liquid := 2*v*p.avgShortPrice[pair] - v*p.lastCandle[pair].Close
				total += liquid
			} else {
				total += amount * p.lastCandle[pair].Close
			}

			p.assetValues[asset] = append(p.assetValues[asset], AssetValue{
				Time:  candle.Time,
				Value: amount * p.lastCandle[pair].Close,
			})
		}

		baseCoinInfo := p.assets[p.baseCoin]
		p.equityValues = append(p.equityValues, AssetValue{
			Time:  candle.Time,
			Value: total + baseCoinInfo.Lock + baseCoinInfo.Free,
		})
	}
}

func (p *PaperWallet) Account() (model.Account, error) {
	balances := make([]model.Balance, 0)
	for pair, info := range p.assets {
		balances = append(balances, model.Balance{
			Asset: pair,
			Free:  info.Free,
			Lock:  info.Lock,
		})
	}

	return model.Account{
		Balances: balances,
	}, nil
}

func (p *PaperWallet) Position(pair string) (asset, quote float64, err error) {
	p.Lock()
	defer p.Unlock()

	assetTick, quoteTick := SplitAssetQuote(pair)
	acc, err := p.Account()
	if err != nil {
		return 0, 0, err
	}

	assetBalance, quoteBalance := acc.Balance(assetTick, quoteTick)

	return assetBalance.Free + assetBalance.Lock, quoteBalance.Free + quoteBalance.Lock, nil
}

func (p *PaperWallet) CreateOrderOCO(side model.SideType, pair string,
	size, price, stop, stopLimit float64) ([]model.Order, error) {
	p.Lock()
	defer p.Unlock()

	if size == 0 {
		return nil, ErrInvalidQuantity
	}

	err := p.validateFunds(side, pair, size, price, false)
	if err != nil {
		return nil, err
	}

	groupID := p.ID()
	/*
		这段代码创建了一个名为 limitMaker 的订单对象，该订单对象包含了一个限价挂单的详细信息，其属性包括：

		ExchangeID：该订单所属的交易所 ID。
		CreatedAt：该订单的创建时间。
		UpdatedAt：该订单的更新时间。
		Pair：该订单的交易对。
		Side：该订单的买卖方向，可以是 model.SideTypeBuy 或 model.SideTypeSell。
		Type：该订单的订单类型，可以是 model.OrderTypeLimitMaker 或其他类型的订单（如 model.OrderTypeLimit）。
		Status：该订单的状态，可以是 model.OrderStatusTypeNew、model.OrderStatusTypeFilled、model.OrderStatusTypeCanceled 等状态。
		Price：该订单的挂单价格。
		Quantity：该订单的数量。
		GroupID：该订单所属的订单组 ID。
		RefPrice：该订单的参考价格，即订单创建时的市场价格
	*/
	limitMaker := model.Order{
		ExchangeID: p.ID(),
		CreatedAt:  p.lastCandle[pair].Time,
		UpdatedAt:  p.lastCandle[pair].Time,
		Pair:       pair,
		Side:       side,
		Type:       model.OrderTypeLimitMaker,
		Status:     model.OrderStatusTypeNew,
		Price:      price,
		Quantity:   size,
		GroupID:    &groupID,
		RefPrice:   p.lastCandle[pair].Close,
	}

	/*

			这段代码创建了一个名为 stopOrder 的订单对象，该订单对象包含了一个停损订单的详细信息，其属性包括：

			ExchangeID：该订单所属的交易所 ID。
			CreatedAt：该订单的创建时间。
			UpdatedAt：该订单的更新时间。
			Pair：该订单的交易对。
			Side：该订单的买卖方向，可以是 model.SideTypeBuy 或 model.SideTypeSell。
			Type：该订单的订单类型，可以是 model.OrderTypeStopLoss 或其他类型的订单（如 model.OrderTypeLimit）。
			Status：该订单的状态，可以是 model.OrderStatusTypeNew、model.OrderStatusTypeFilled、model.OrderStatusTypeCanceled 等状态。
			Price：该订单的触发价格，即当市场价格达到该价格时，该订单将被触发执行。
			Stop：当触发价格被触及时，该订单的执行价格将是 Stop 属性的值。例如，如果 Side 是 model.SideTypeSell，则 Stop 属性将是一个低于当前市场价的价格。
			Quantity：该订单的数量。
			GroupID：该订单所属的订单组 ID。
			RefPrice：该订单的参考价格，即订单创建时的市场价格。
			例如，假设交易所 ID 是 binance，该订单在 2023 年 4 月 1 日创建，交易对是 BTC/USDT，买卖方向是 model.SideTypeSell，订单类型是 model.OrderTypeStopLoss，触发价格是 50,000 美元，执行价格是 49,000 美元，
		数量是 0.5 BTC，订单组 ID 是 123456，参考价格是当前市场价 51,000 美元。则可以创建一个如下的停损订单：
		stopOrder := model.Order{
		    ExchangeID: "binance",
		    CreatedAt:  time.Date(2023, 4, 1, 0, 0, 0, 0, time.UTC),
		    UpdatedAt:  time.Date(2023, 4, 1, 0, 0, 0, 0, time.UTC),
		    Pair:       "BTC/USDT",
		    Side:       model.SideTypeSell,
		    Type:       model.OrderTypeStopLoss,
		    Status:     model.OrderStatusTypeNew,
		    Price:      50000,
		    Stop:       &49000,
		    Quantity:   0.5,
		    GroupID:    &123456,
		    RefPrice:   51000,
		}
	*/

	stopOrder := model.Order{
		ExchangeID: p.ID(),
		CreatedAt:  p.lastCandle[pair].Time,
		UpdatedAt:  p.lastCandle[pair].Time,
		Pair:       pair,
		Side:       side,
		Type:       model.OrderTypeStopLoss,
		Status:     model.OrderStatusTypeNew,
		Price:      stopLimit,
		Stop:       &stop,
		Quantity:   size,
		GroupID:    &groupID,
		RefPrice:   p.lastCandle[pair].Close,
	}
	p.orders = append(p.orders, limitMaker, stopOrder)

	return []model.Order{limitMaker, stopOrder}, nil
}

func (p *PaperWallet) CreateOrderLimit(side model.SideType, pair string,
	size float64, limit float64) (model.Order, error) {

	p.Lock()
	defer p.Unlock()

	if size == 0 {
		return model.Order{}, ErrInvalidQuantity
	}

	err := p.validateFunds(side, pair, size, limit, false)
	if err != nil {
		return model.Order{}, err
	}
	order := model.Order{
		ExchangeID: p.ID(),
		CreatedAt:  p.lastCandle[pair].Time,
		UpdatedAt:  p.lastCandle[pair].Time,
		Pair:       pair,
		Side:       side,
		Type:       model.OrderTypeLimit,
		Status:     model.OrderStatusTypeNew,
		Price:      limit,
		Quantity:   size,
	}
	p.orders = append(p.orders, order)
	return order, nil
}

func (p *PaperWallet) CreateOrderMarket(side model.SideType, pair string, size float64) (model.Order, error) {
	p.Lock()
	defer p.Unlock()

	return p.createOrderMarket(side, pair, size)
}

func (p *PaperWallet) CreateOrderStop(pair string, size float64, limit float64) (model.Order, error) {
	p.Lock()
	defer p.Unlock()

	if size == 0 {
		return model.Order{}, ErrInvalidQuantity
	}

	err := p.validateFunds(model.SideTypeSell, pair, size, limit, false)
	if err != nil {
		return model.Order{}, err
	}

	/*

	 */
	order := model.Order{
		ExchangeID: p.ID(),
		CreatedAt:  p.lastCandle[pair].Time,
		UpdatedAt:  p.lastCandle[pair].Time,
		Pair:       pair,
		Side:       model.SideTypeSell,
		Type:       model.OrderTypeStopLossLimit,
		Status:     model.OrderStatusTypeNew,
		Price:      limit,
		Stop:       &limit,
		Quantity:   size,
	}
	p.orders = append(p.orders, order)
	return order, nil
}

func (p *PaperWallet) createOrderMarket(side model.SideType, pair string, size float64) (model.Order, error) {
	if size == 0 {
		return model.Order{}, ErrInvalidQuantity
	}

	err := p.validateFunds(side, pair, size, p.lastCandle[pair].Close, true)
	if err != nil {
		return model.Order{}, err
	}

	if _, ok := p.volume[pair]; !ok {
		p.volume[pair] = 0
	}

	p.volume[pair] += p.lastCandle[pair].Close * size

	order := model.Order{
		ExchangeID: p.ID(),
		CreatedAt:  p.lastCandle[pair].Time,
		UpdatedAt:  p.lastCandle[pair].Time,
		Pair:       pair,
		Side:       side,
		Type:       model.OrderTypeMarket,
		Status:     model.OrderStatusTypeFilled,
		Price:      p.lastCandle[pair].Close,
		Quantity:   size,
	}

	p.orders = append(p.orders, order)

	return order, nil
}

func (p *PaperWallet) CreateOrderMarketQuote(side model.SideType, pair string,
	quoteQuantity float64) (model.Order, error) {
	p.Lock()
	defer p.Unlock()

	info := p.AssetsInfo(pair)
	quantity := common.AmountToLotSize(info.StepSize, info.BaseAssetPrecision, quoteQuantity/p.lastCandle[pair].Close)
	return p.createOrderMarket(side, pair, quantity)
}

func (p *PaperWallet) Cancel(order model.Order) error {
	p.Lock()
	defer p.Unlock()

	for i, o := range p.orders {
		if o.ExchangeID == order.ExchangeID {
			p.orders[i].Status = model.OrderStatusTypeCanceled
		}
	}
	return nil
}

func (p *PaperWallet) Order(_ string, id int64) (model.Order, error) {
	for _, order := range p.orders {
		if order.ExchangeID == id {
			return order, nil
		}
	}
	return model.Order{}, errors.New("order not found")
}

func (p *PaperWallet) CandlesByPeriod(ctx context.Context, pair, period string,
	start, end time.Time) ([]model.Candle, error) {
	return p.feeder.CandlesByPeriod(ctx, pair, period, start, end)
}

func (p *PaperWallet) CandlesByLimit(ctx context.Context, pair, period string, limit int) ([]model.Candle, error) {
	return p.feeder.CandlesByLimit(ctx, pair, period, limit)
}

func (p *PaperWallet) CandlesSubscription(ctx context.Context, pair, timeframe string) (chan model.Candle, chan error) {
	return p.feeder.CandlesSubscription(ctx, pair, timeframe)
}

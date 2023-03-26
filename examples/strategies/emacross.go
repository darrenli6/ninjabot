package strategies

import (
	"github.com/rodrigo-brito/ninjabot"
	"github.com/rodrigo-brito/ninjabot/indicator"
	"github.com/rodrigo-brito/ninjabot/service"
	"github.com/rodrigo-brito/ninjabot/strategy"
	"github.com/rodrigo-brito/ninjabot/tools/log"
)

/*
这是一个名为CrossEMA的策略实现，实现了Strategy接口。

Timeframe方法返回策略的时间框架，这里是4小时。

WarmupPeriod方法返回策略启动前需要等待的周期数，这里是21个周期。

Indicators方法接受一个数据帧作为输入，该数据帧包含一个交易对的OHLCV数据，
并使用indicator.EMA和indicator.SMA函数计算EMA8和SMA21指标。然后它返回一个ChartIndicator结构体的切片，用于在图表上绘制指标。

OnCandle方法在每个新的蜡烛图出现时被调用，计算完指标后。它检查EMA8指标是否上穿或下穿SMA21指标，
并相应地开仓或平仓，使用broker.CreateOrderMarket方法放置市价单。

总的来说，CrossEMA策略是一种简单的趋势跟踪策略，使用两个移动平均线生成开仓和平仓的信号。
*/

type CrossEMA struct{}

// 周期是4h
func (e CrossEMA) Timeframe() string {
	return "4h"
}

func (e CrossEMA) WarmupPeriod() int {
	return 21
}

func (e CrossEMA) Indicators(df *ninjabot.Dataframe) []strategy.ChartIndicator {
	df.Metadata["ema8"] = indicator.EMA(df.Close, 8)
	df.Metadata["sma21"] = indicator.SMA(df.Close, 21)

	// 绘图指示
	return []strategy.ChartIndicator{
		{
			Overlay:   true,
			GroupName: "MA's",
			Time:      df.Time,
			Metrics: []strategy.IndicatorMetric{
				{
					Values: df.Metadata["ema8"],
					Name:   "EMA 8",
					Color:  "red",
					Style:  strategy.StyleLine,
				},
				{
					Values: df.Metadata["sma21"],
					Name:   "SMA 21",
					Color:  "blue",
					Style:  strategy.StyleLine,
				},
			},
		},
	}
}

func (e *CrossEMA) OnCandle(df *ninjabot.Dataframe, broker service.Broker) {
	// 取出最后一根线的收盘价
	closePrice := df.Close.Last(0)

	assetPosition, quotePosition, err := broker.Position(df.Pair)
	if err != nil {
		log.Error(err)
		return
	}

	if quotePosition >= 10 && // minimum quote position to trade
		df.Metadata["ema8"].Crossover(df.Metadata["sma21"]) { // trade signal (EMA8 > SMA21)

		amount := quotePosition / closePrice // calculate amount of asset to buy
		_, err := broker.CreateOrderMarket(ninjabot.SideTypeBuy, df.Pair, amount)
		if err != nil {
			log.Error(err)
		}

		return
	}

	if assetPosition > 0 &&
		df.Metadata["ema8"].Crossunder(df.Metadata["sma21"]) { // trade signal (EMA8 < SMA21)

		_, err = broker.CreateOrderMarket(ninjabot.SideTypeSell, df.Pair, assetPosition)
		if err != nil {
			log.Error(err)
		}
	}
}

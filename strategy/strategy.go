package strategy

import (
	"github.com/rodrigo-brito/ninjabot/model"
	"github.com/rodrigo-brito/ninjabot/service"
)

type Strategy interface {
	// Timeframe is the time interval in which the strategy will be executed. eg: 1h, 1d, 1w
	Timeframe() string
	// WarmupPeriod is the necessary time to wait before executing the strategy, to load data for indicators.
	// This time is measured in the period specified in the `Timeframe` function.
	WarmupPeriod() int
	// Indicators will be executed for each new candle, in order to fill indicators before `OnCandle` function is called.
	Indicators(df *model.Dataframe) []ChartIndicator
	// OnCandle will be executed for each new candle, after indicators are filled, here you can do your trading logic.
	// OnCandle is executed after the candle close.
	OnCandle(df *model.Dataframe, broker service.Broker)
}

type HighFrequencyStrategy interface {
	Strategy

	// OnPartialCandle will be executed for each new partial candle, after indicators are filled.
	OnPartialCandle(df *model.Dataframe, broker service.Broker)
}

/*
这段代码定义了两个接口，Strategy和HighFrequencyStrategy，它们都用于实现交易策略。以下是每个接口的功能和方法：

Strategy接口：表示一种交易策略，具有以下方法：
Timeframe：返回执行策略的时间间隔，例如1小时、1天或1周等。
WarmupPeriod：返回执行策略前需要等待的时间，以加载指标数据。这段时间是以Timeframe函数指定的时间为单位计算的。
Indicators：在每个新蜡烛图上执行指标计算，以填充指标数据。
OnCandle：在每个新蜡烛图关闭后执行，这里可以执行交易逻辑。
HighFrequencyStrategy接口：表示高频交易策略，具有以下方法：
OnPartialCandle：在每个新的部分蜡烛图关闭后执行，这里可以执行交易逻辑。
HighFrequencyStrategy接口继承了Strategy接口的所有方法，因此它可以执行与低频策略相同的操作，但它还有一个额外的OnPartialCandle方法，用于在接收到部分蜡烛图数据时执行交易逻辑。这对于需要在每个新的蜡烛图上进行交易操作的策略非常有用，而不仅仅是在每个新的蜡烛图关闭时。

*/

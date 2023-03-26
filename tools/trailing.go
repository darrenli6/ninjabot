package tools

/*
这段代码定义了一个名为TrailingStop的结构体，用于实现跟踪止损（Trailing Stop）功能。跟踪止损是一种动态止损策略，当价格上涨时，止损价格会跟随上涨；当价格下跌时，止损价格保持不变。以下是代码的主要组成部分：

TrailingStop结构体：包含当前价格（current）、止损价格（stop）以及一个表示是否激活（active）的布尔值。
NewTrailingStop函数：创建一个新的TrailingStop实例。
Start方法：设置当前价格和止损价格，然后将active设置为true，以启动跟踪止损。
Stop方法：将active设置为false，以停止跟踪止损。
Active方法：返回跟踪止损是否处于激活状态。
Update方法：更新当前价格。如果跟踪止损处于激活状态并且当前价格大于之前的价格，更新止损价格。如果当前价格小于等于止损价格，返回true，表示触发止损。否则，返回false。
这个TrailingStop结构体可以用于管理加密货币交易中的跟踪止损策略。跟踪止损在投资管理中非常有用，因为它可以在价格上涨时提高止损价位，从而保护投资收益。
*/

type TrailingStop struct {
	current float64
	stop    float64
	active  bool
}

func NewTrailingStop() *TrailingStop {
	return &TrailingStop{}
}

func (t *TrailingStop) Start(current, stop float64) {
	t.stop = stop
	t.current = current
	t.active = true
}

func (t *TrailingStop) Stop() {
	t.active = false
}

func (t TrailingStop) Active() bool {
	return t.active
}

func (t *TrailingStop) Update(current float64) bool {
	if !t.active {
		return false
	}

	if current > t.current {
		t.stop = t.stop + (current - t.current)
		t.current = current
		return false
	}

	t.current = current
	return current <= t.stop
}

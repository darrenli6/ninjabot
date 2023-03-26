package strategy

import (
	log "github.com/sirupsen/logrus"

	"github.com/rodrigo-brito/ninjabot/model"
	"github.com/rodrigo-brito/ninjabot/service"
)

/*
这段代码定义了一个名为Controller的结构体，它用于处理交易策略。Controller的主要功能是在接收到蜡烛图数据时，
更新交易策略和数据框，然后调用交易策略的OnCandle和OnPartialCandle方法。以下是代码的主要组成部分：

Controller结构体：包含一个交易策略（strategy）、一个数据框（dataframe）、一个代理服务（broker）以及一个布尔值（started）表示控制器是否已启动。
NewStrategyController函数：创建一个新的Controller实例，接收一个交易对（pair）、一个交易策略（strategy）以及一个代理服务（broker）作为参数。
Start方法：将started设置为true，表示控制器已启动。
OnPartialCandle方法：当接收到一个不完整的蜡烛图数据时调用此方法。如果蜡烛图数据不完整，且数据框的Close数据长度大于等于策略的热身周期，此方法会更新数据框，计算策略指标，并调用策略的OnPartialCandle方法。
updateDataFrame方法：根据给定的蜡烛图数据更新数据框。
OnCandle方法：当接收到一个完整的蜡烛图数据时调用此方法。首先，此方法会检查蜡烛图数据的时间戳，确保其晚于数据框中最后一个时间戳。然后，更新数据框，计算策略指标。最后，如果控制器已启动，调用策略的OnCandle方法。
这个Controller结构体可以用于处理交易策略，在接收到蜡烛图数据时更新策略状态并执行策略相关操作。这对于实时交易策略执行非常有用。
*/
type Controller struct {
	strategy  Strategy
	dataframe *model.Dataframe
	broker    service.Broker
	started   bool
}

func NewStrategyController(pair string, strategy Strategy, broker service.Broker) *Controller {
	dataframe := &model.Dataframe{
		Pair:     pair,
		Metadata: make(map[string]model.Series[float64]),
	}

	return &Controller{
		dataframe: dataframe,
		strategy:  strategy,
		broker:    broker,
	}
}

func (s *Controller) Start() {
	s.started = true
}

func (s *Controller) OnPartialCandle(candle model.Candle) {
	if !candle.Complete && len(s.dataframe.Close) >= s.strategy.WarmupPeriod() {
		if str, ok := s.strategy.(HighFrequencyStrategy); ok {
			s.updateDataFrame(candle)
			str.Indicators(s.dataframe)
			str.OnPartialCandle(s.dataframe, s.broker)
		}
	}
}

func (s *Controller) updateDataFrame(candle model.Candle) {
	if len(s.dataframe.Time) > 0 && candle.Time.Equal(s.dataframe.Time[len(s.dataframe.Time)-1]) {
		last := len(s.dataframe.Time) - 1
		s.dataframe.Close[last] = candle.Close
		s.dataframe.Open[last] = candle.Open
		s.dataframe.High[last] = candle.High
		s.dataframe.Low[last] = candle.Low
		s.dataframe.Volume[last] = candle.Volume
		s.dataframe.Time[last] = candle.Time
		for k, v := range candle.Metadata {
			s.dataframe.Metadata[k][last] = v
		}
	} else {
		s.dataframe.Close = append(s.dataframe.Close, candle.Close)
		s.dataframe.Open = append(s.dataframe.Open, candle.Open)
		s.dataframe.High = append(s.dataframe.High, candle.High)
		s.dataframe.Low = append(s.dataframe.Low, candle.Low)
		s.dataframe.Volume = append(s.dataframe.Volume, candle.Volume)
		s.dataframe.Time = append(s.dataframe.Time, candle.Time)
		s.dataframe.LastUpdate = candle.Time
		for k, v := range candle.Metadata {
			s.dataframe.Metadata[k] = append(s.dataframe.Metadata[k], v)
		}
	}
}

func (s *Controller) OnCandle(candle model.Candle) {
	if len(s.dataframe.Time) > 0 && candle.Time.Before(s.dataframe.Time[len(s.dataframe.Time)-1]) {
		log.Errorf("late candle received: %#v", candle)
		return
	}

	s.updateDataFrame(candle)

	if len(s.dataframe.Close) >= s.strategy.WarmupPeriod() {
		s.strategy.Indicators(s.dataframe)
		if s.started {
			s.strategy.OnCandle(s.dataframe, s.broker)
		}
	}
}

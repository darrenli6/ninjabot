package tools

import (
	"github.com/rodrigo-brito/ninjabot"
	"github.com/rodrigo-brito/ninjabot/service"
	"github.com/samber/lo"
	log "github.com/sirupsen/logrus"
)

/*
这段代码定义了一个名为Scheduler的结构体，它用于在满足特定条件时执行买入和卖出操作。Scheduler的主要功能是根据提供的条件函数（在加密货币交易数据满足条件时返回true）以及交易数量，创建买入或卖出订单。以下是代码的主要组成部分：

OrderCondition结构体：包含条件函数（Condition）、交易数量（Size）以及交易方向（Side，可以是买入或卖出）。
Scheduler结构体：包含一个交易对（pair）以及一个订单条件列表（orderConditions）。
NewScheduler函数：创建一个新的Scheduler实例，接收一个交易对参数。
SellWhen方法：向orderConditions列表中添加一个卖出订单条件，接收交易数量（size）和条件函数（condition）作为参数。
BuyWhen方法：向orderConditions列表中添加一个买入订单条件，接收交易数量（size）和条件函数（condition）作为参数。
Update方法：当ninjabot.Dataframe更新时，调用此方法。该方法会遍历所有的订单条件。如果满足条件，则创建一个市价单，并从订单条件列表中移除该条件。如果创建订单过程中出现错误，记录错误信息并保留该条件。
这个Scheduler结构体可以用于自动执行交易策略，当满足特定条件时，自动发起买入或卖出操作。这对于根据实时数据执行交易策略非常有用。
*/
type OrderCondition struct {
	Condition func(df *ninjabot.Dataframe) bool
	Size      float64
	Side      ninjabot.SideType
}

type Scheduler struct {
	pair            string
	orderConditions []OrderCondition
}

func NewScheduler(pair string) *Scheduler {
	return &Scheduler{pair: pair}
}

func (s *Scheduler) SellWhen(size float64, condition func(df *ninjabot.Dataframe) bool) {
	s.orderConditions = append(
		s.orderConditions,
		OrderCondition{Condition: condition, Size: size, Side: ninjabot.SideTypeSell},
	)
}

func (s *Scheduler) BuyWhen(size float64, condition func(df *ninjabot.Dataframe) bool) {
	s.orderConditions = append(
		s.orderConditions,
		OrderCondition{Condition: condition, Size: size, Side: ninjabot.SideTypeBuy},
	)
}

func (s *Scheduler) Update(df *ninjabot.Dataframe, broker service.Broker) {
	s.orderConditions = lo.Filter[OrderCondition](s.orderConditions, func(oc OrderCondition, _ int) bool {
		if oc.Condition(df) {
			_, err := broker.CreateOrderMarket(oc.Side, s.pair, oc.Size)
			if err != nil {
				log.Error(err)
				return true
			}
			return false
		}
		return true
	})
}

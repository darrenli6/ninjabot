package main

import (
	"context"

	"github.com/rodrigo-brito/ninjabot"
	"github.com/rodrigo-brito/ninjabot/examples/strategies"
	"github.com/rodrigo-brito/ninjabot/exchange"
	"github.com/rodrigo-brito/ninjabot/plot"
	"github.com/rodrigo-brito/ninjabot/plot/indicator"
	"github.com/rodrigo-brito/ninjabot/storage"
	"github.com/rodrigo-brito/ninjabot/tools/log"
)

/*
这是一个使用Golang编写的程序，用于运行一个名为Ninjabot的加密货币交易机器人。Ninjabot使用一个名为CrossEMA（指数移动平均线交叉）的策略进行交易。程序的主要功能如下：

初始化一些交易对（这里是BTCUSDT和ETHUSDT）。
加载历史数据，从CSV文件中读取BTC和ETH的小时数据。
创建一个内存中的数据库用于存储交易数据。
初始化一个模拟钱包，以10,000 USDT作为起始资金。
创建一个图表，展示策略中的指标以及一个自定义的相对强度指数（RSI）指标。
初始化Ninjabot，连接设置好的模拟钱包、策略等。
运行交易机器人进行模拟交易。
输出交易机器人的交易结果。
在本地浏览器中展示蜡烛图。
程序的目的是使用策略进行回测交易，以评估策略在历史数据上的表现。回测在模拟环境中进行，不涉及真实资金。在回测结束后，程序会打印出交易结果，以及在本地浏览器中显示蜡烛图。
*/
func main() {
	ctx := context.Background()

	// bot settings (eg: pairs, telegram, etc)
	// 设置参数
	settings := ninjabot.Settings{
		Pairs: []string{
			"BTCUSDT",
			"ETHUSDT",
		},
	}

	// initialize your strategy
	strategy := new(strategies.CrossEMA)

	// load historical data from CSV files
	csvFeed, err := exchange.NewCSVFeed(
		strategy.Timeframe(),
		exchange.PairFeed{
			Pair:      "BTCUSDT",
			File:      "testdata/btc-1h.csv",
			Timeframe: "1h",
		},
		exchange.PairFeed{
			Pair:      "ETHUSDT",
			File:      "testdata/eth-1h.csv",
			Timeframe: "1h",
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	// initialize a database in memory
	storage, err := storage.FromMemory()
	if err != nil {
		log.Fatal(err)
	}

	// create a paper wallet for simulation, initializing with 10.000 USDT
	// 钱包初始化
	wallet := exchange.NewPaperWallet(
		ctx,
		"USDT",
		exchange.WithPaperAsset("USDT", 10000),
		exchange.WithDataFeed(csvFeed),
	)

	// create a chart  with indicators from the strategy and a custom additional RSI indicator
	chart, err := plot.NewChart(
		plot.WithStrategyIndicators(strategy),
		plot.WithCustomIndicators(
			indicator.RSI(14, "purple"),
		),
		plot.WithPaperWallet(wallet),
	)
	if err != nil {
		log.Fatal(err)
	}

	// initializer Ninjabot with the objects created before
	bot, err := ninjabot.NewBot(
		ctx,
		settings,
		wallet,
		strategy,
		ninjabot.WithBacktest(wallet), // Required for Backtest mode
		ninjabot.WithStorage(storage),

		// connect bot feed (candle and orders) to the chart
		ninjabot.WithCandleSubscription(chart),
		ninjabot.WithOrderSubscription(chart),
		ninjabot.WithLogLevel(log.WarnLevel),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Initializer simulation
	err = bot.Run(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// Print bot results
	bot.Summary()

	// Display candlesticks chart in local browser
	err = chart.Start()
	if err != nil {
		log.Fatal(err)
	}
}

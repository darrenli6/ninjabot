package main

import (
	"context"
	"os"
	"strconv"

	"github.com/rodrigo-brito/ninjabot/plot"
	"github.com/rodrigo-brito/ninjabot/plot/indicator"

	"github.com/rodrigo-brito/ninjabot"
	"github.com/rodrigo-brito/ninjabot/examples/strategies"
	"github.com/rodrigo-brito/ninjabot/exchange"
	"github.com/rodrigo-brito/ninjabot/storage"
	"github.com/rodrigo-brito/ninjabot/tools/log"
)

/*
这段代码是一个使用 Ninjabot 交易框架的简单示例，用于模拟加密货币交易。Ninjabot 是一个用 Go 语言编写的开源交易框架，可以用于创建自动化的交易策略。

以下是代码的逻辑概述：

首先，创建一个上下文对象 ctx，并从环境变量中获取 Telegram 机器人的 token 和用户 ID。
定义交易设置（settings），包括要交易的货币对（Pairs）和 Telegram 设置（启用通知、token 和用户列表）。
创建一个用于实时数据提供的 Binance 交易所实例。
创建一个内存存储实例（storage），用于保存交易数据。
创建一个模拟交易所钱包（paperWallet），用于模拟交易操作。这个钱包初始资产为 10,000 USDT，并使用 Binance 实例作为数据提供源。
初始化一个交易策略（strategy），在这个例子中，使用一个名为 CrossEMA 的策略。
创建一个新的图表（chart），并定义要显示的自定义指标，如 8 周期的指数移动平均线（EMA）和 21 周期的简单移动平均线（SMA）。
使用之前定义的设置、策略、存储、模拟钱包和图表初始化 Ninjabot 实例（bot）。
在一个新的 goroutine 中启动图表服务。
运行 Ninjabot，开始模拟交易。如果遇到错误，将其记录到日志。
通过这个例子，你可以看到如何使用 Ninjabot 创建一个基本的自动化交易策略。这个策略在模拟环境中运行，不会实际交易加密货币。你可以根据自己的需求修改策略、设置和其他参数来创建更复杂的交易策略。
*/

func main() {
	var (
		ctx             = context.Background()
		telegramToken   = os.Getenv("TELEGRAM_TOKEN")
		telegramUser, _ = strconv.Atoi(os.Getenv("TELEGRAM_USER"))
	)

	settings := ninjabot.Settings{
		Pairs: []string{
			"BTCUSDT",
			"ETHUSDT",
			"BNBUSDT",
			"LTCUSDT",
		},
		Telegram: ninjabot.TelegramSettings{
			Enabled: telegramToken != "" && telegramUser != 0,
			Token:   telegramToken,
			Users:   []int{telegramUser},
		},
	}

	// Use binance for realtime data feed
	binance, err := exchange.NewBinance(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// creating a storage to save trades
	storage, err := storage.FromMemory()
	if err != nil {
		log.Fatal(err)
	}

	// creating a paper wallet to simulate an exchange waller for fake operataions
	// paper wallet is simulation of a real exchange wallet
	paperWallet := exchange.NewPaperWallet(
		ctx,
		"USDT",
		exchange.WithPaperFee(0.001, 0.001),
		exchange.WithPaperAsset("USDT", 10000),
		exchange.WithDataFeed(binance),
	)

	// initializing my strategy
	strategy := new(strategies.CrossEMA)

	chart, err := plot.NewChart(
		plot.WithCustomIndicators(
			indicator.EMA(8, "red"),
			indicator.SMA(21, "blue"),
		),
	)
	if err != nil {
		log.Fatal(err)
	}

	// initializer ninjabot
	bot, err := ninjabot.NewBot(
		ctx,
		settings,
		paperWallet,
		strategy,
		ninjabot.WithStorage(storage),
		ninjabot.WithPaperWallet(paperWallet),
		ninjabot.WithCandleSubscription(chart),
		ninjabot.WithOrderSubscription(chart),
	)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		err := chart.Start()
		if err != nil {
			log.Fatal(err)
		}
	}()

	err = bot.Run(ctx)
	if err != nil {
		log.Fatal(err)
	}
}

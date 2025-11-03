package main

import (
	"fmt"
	"log"
	"nofx/trader"
)

// 这是一个使用新的 golighter SDK 的 Lighter 交易器示例
func main() {
	// 配置 Lighter 交易器
	config := trader.LighterConfig{
		Endpoint:      "https://mainnet.zklighter.elliot.ai",
		APIKeyPrivKey: "your_api_key_private_key_hex",
		AccountIndex:  0,
		APIKeyIndex:   0,
		ChainID:       2, // mainnet=2, testnet=1
	}

	// 创建交易器实例
	lighterTrader, err := trader.NewLighterTrader(config)
	if err != nil {
		log.Fatalf("创建 Lighter 交易器失败: %v", err)
	}

	// 获取账户余额
	balance, err := lighterTrader.GetBalance()
	if err != nil {
		log.Printf("获取余额失败: %v", err)
	} else {
		fmt.Printf("账户余额: %+v\n", balance)
	}

	// 获取持仓信息
	positions, err := lighterTrader.GetPositions()
	if err != nil {
		log.Printf("获取持仓失败: %v", err)
	} else {
		fmt.Printf("持仓列表: %+v\n", positions)
	}

	// 获取市场价格
	price, err := lighterTrader.GetMarketPrice("ETHUSDT")
	if err != nil {
		log.Printf("获取价格失败: %v", err)
	} else {
		fmt.Printf("ETH 价格: %.2f\n", price)
	}

	// 开多仓示例（注意：这会真实下单！）
	// order, err := lighterTrader.OpenLong("ETHUSDT", 0.01, 10)
	// if err != nil {
	// 	log.Printf("开多仓失败: %v", err)
	// } else {
	// 	fmt.Printf("开多仓成功: %+v\n", order)
	// }
}

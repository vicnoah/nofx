package trader

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	lighterapi "github.com/defi-maker/golighter/api"
	"github.com/defi-maker/golighter/client"
	"github.com/elliottech/lighter-go/types"
	"github.com/elliottech/lighter-go/types/txtypes"
)

// LighterTrader Lighter交易所交易器
type LighterTrader struct {
	restClient   *client.Client
	txClient     *client.TxClient
	ctx          context.Context
	accountIndex int64
	apiKeyIndex  uint8
	chainID      uint32
	
	// 市场信息缓存
	marketInfoCache map[string]*lighterapi.OrderBook // symbol -> market info
}

// LighterConfig Lighter交易器配置
type LighterConfig struct {
	Endpoint      string // API端点
	APIKeyPrivKey string // API密钥私钥 (hex格式)
	AccountIndex  int64  // 账户索引
	APIKeyIndex   uint8  // API密钥索引
	ChainID       uint32 // 链ID (testnet=1 mainnet=2)
}

// NewLighterTrader 创建Lighter交易器
func NewLighterTrader(config LighterConfig) (*LighterTrader, error) {
	// 创建REST客户端
	restClient, err := client.New(config.Endpoint, client.WithChannelName("nofx"))
	if err != nil {
		return nil, fmt.Errorf("创建REST客户端失败: %w", err)
	}

	// 创建交易客户端
	txClient, err := client.NewTxClient(
		restClient,
		config.APIKeyPrivKey,
		config.AccountIndex,
		config.APIKeyIndex,
		config.ChainID,
	)
	if err != nil {
		return nil, fmt.Errorf("创建交易客户端失败: %w", err)
	}

	log.Printf("✓ Lighter交易器初始化成功 (endpoint=%s, account=%d, apiKey=%d, chainID=%d)",
		config.Endpoint, config.AccountIndex, config.APIKeyIndex, config.ChainID)

	trader := &LighterTrader{
		restClient:      restClient,
		txClient:        txClient,
		ctx:             context.Background(),
		accountIndex:    config.AccountIndex,
		apiKeyIndex:     config.APIKeyIndex,
		chainID:         config.ChainID,
		marketInfoCache: make(map[string]*lighterapi.OrderBook),
	}

	// 初始化时加载市场信息
	if err := trader.loadMarketInfo(); err != nil {
		log.Printf("⚠️ 加载市场信息失败: %v", err)
	}

	return trader, nil
}

// GetBalance 获取账户余额
func (t *LighterTrader) GetBalance() (map[string]interface{}, error) {
	log.Printf("🔄 正在调用Lighter API获取账户余额...")

	// 调用 Account 接口
	accountResp, err := t.restClient.Account(t.ctx, &lighterapi.AccountParams{
		By:    "index",
		Value: fmt.Sprint(t.accountIndex),
	})
	if err != nil {
		log.Printf("❌ Lighter API调用失败: %v", err)
		return nil, fmt.Errorf("获取账户信息失败: %w", err)
	}

	if len(accountResp.Accounts) == 0 {
		return nil, fmt.Errorf("未找到账户信息")
	}

	account := accountResp.Accounts[0]

	// 解析余额和抵押品
	availableBalance, _ := strconv.ParseFloat(*account.AvailableBalance, 64)
	collateral, _ := strconv.ParseFloat(*account.Collateral, 64)

	// 计算未实现盈亏
	totalUnrealizedPnl := 0.0
	if account.Positions != nil {
		for _, pos := range *account.Positions {
			if pos.UnrealizedPnl != nil {
				unrealizedPnl, _ := strconv.ParseFloat(*pos.UnrealizedPnl, 64)
				totalUnrealizedPnl += unrealizedPnl
			}
		}
	}

	// 计算钱包余额（不含未实现盈亏）
	walletBalanceWithoutUnrealized := collateral - totalUnrealizedPnl

	result := make(map[string]interface{})
	result["totalWalletBalance"] = walletBalanceWithoutUnrealized
	result["availableBalance"] = availableBalance
	result["totalUnrealizedProfit"] = totalUnrealizedPnl

	log.Printf("✅ Lighter 账户: 总净值=%.2f (钱包%.2f+未实现%.2f), 可用=%.2f",
		collateral,
		walletBalanceWithoutUnrealized,
		totalUnrealizedPnl,
		availableBalance)

	return result, nil
}

// GetPositions 获取所有持仓
func (t *LighterTrader) GetPositions() ([]map[string]interface{}, error) {
	// 调用 Account 接口获取持仓信息
	accountResp, err := t.restClient.Account(t.ctx, &lighterapi.AccountParams{
		By:    "index",
		Value: fmt.Sprint(t.accountIndex),
	})
	if err != nil {
		return nil, fmt.Errorf("获取账户信息失败: %w", err)
	}

	if len(accountResp.Accounts) == 0 {
		return []map[string]interface{}{}, nil
	}

	account := accountResp.Accounts[0]
	var result []map[string]interface{}

	if account.Positions == nil {
		return result, nil
	}

	// 遍历所有持仓
	for _, pos := range *account.Positions {
		if pos.Position == nil {
			continue
		}

		position, _ := strconv.ParseFloat(*pos.Position, 64)
		
		// 跳过无持仓的
		if position == 0 {
			continue
		}

		posMap := make(map[string]interface{})
		
		// 标准化symbol格式
		symbol := *pos.Symbol + "USDT"
		posMap["symbol"] = symbol

		// 持仓数量和方向
		if pos.Sign != nil && *pos.Sign > 0 {
			posMap["side"] = "long"
			posMap["positionAmt"] = position
		} else {
			posMap["side"] = "short"
			posMap["positionAmt"] = absFloat64(position)
		}

		// 价格信息
		if pos.AvgEntryPrice != nil {
			entryPrice, _ := strconv.ParseFloat(*pos.AvgEntryPrice, 64)
			posMap["entryPrice"] = entryPrice
		}
		
		if pos.PositionValue != nil {
			positionValue, _ := strconv.ParseFloat(*pos.PositionValue, 64)
			if position != 0 {
				posMap["markPrice"] = positionValue / absFloat64(position)
			}
		}
		
		if pos.UnrealizedPnl != nil {
			unrealizedPnl, _ := strconv.ParseFloat(*pos.UnrealizedPnl, 64)
			posMap["unRealizedProfit"] = unrealizedPnl
		}
		
		if pos.LiquidationPrice != nil {
			liquidationPrice, _ := strconv.ParseFloat(*pos.LiquidationPrice, 64)
			posMap["liquidationPrice"] = liquidationPrice
		}

		// 从 InitialMarginFraction 计算杠杆
		if pos.InitialMarginFraction != nil {
			imf, _ := strconv.ParseFloat(*pos.InitialMarginFraction, 64)
			if imf > 0 {
				posMap["leverage"] = 100.0 / imf
			}
		}

		result = append(result, posMap)
	}

	return result, nil
}

// SetMarginMode 设置仓位模式
func (t *LighterTrader) SetMarginMode(symbol string, isCrossMargin bool) error {
	marginModeStr := "全仓"
	if !isCrossMargin {
		marginModeStr = "逐仓"
	}
	log.Printf("  ✓ %s 将使用 %s 模式 (Lighter通过UpdateLeverage设置)", symbol, marginModeStr)
	return nil
}

// SetLeverage 设置杠杆
func (t *LighterTrader) SetLeverage(symbol string, leverage int) error {
	marketIndex, err := t.getMarketIndex(symbol)
	if err != nil {
		return err
	}

	// 计算InitialMarginFraction (IMF = 1 / leverage * 10000)
	imf := uint16(10000 / leverage)

	updateLeverageTx := &types.UpdateLeverageTxReq{
		MarketIndex:           marketIndex,
		InitialMarginFraction: imf,
		MarginMode:            txtypes.CrossMargin,
	}

	txInfo, err := t.txClient.GetUpdateLeverageTransaction(updateLeverageTx, nil)
	if err != nil {
		return fmt.Errorf("设置杠杆失败: %w", err)
	}

	// 发送交易
	txHash, err := t.txClient.SendRawTx(t.ctx, txInfo, nil)
	if err != nil {
		return fmt.Errorf("提交杠杆交易失败: %w", err)
	}

	log.Printf("  ✓ %s 杠杆已切换为 %dx (tx: %s)", symbol, leverage, txHash)
	return nil
}

// OpenLong 开多仓
func (t *LighterTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	// 先取消该币种的所有委托单
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("  ⚠ 取消旧委托单失败: %v", err)
	}

	// 设置杠杆
	if err := t.SetLeverage(symbol, leverage); err != nil {
		return nil, err
	}

	marketIndex, err := t.getMarketIndex(symbol)
	if err != nil {
		return nil, err
	}

	// 获取当前价格
	price, err := t.GetMarketPrice(symbol)
	if err != nil {
		return nil, err
	}

	// 计算base amount和价格
	baseAmount := t.convertToBaseAmount(symbol, quantity)
	lighterPrice := t.convertToLighterPrice(symbol, price*1.01)

	// 生成客户端订单ID
	clientOrderIndex := time.Now().UnixNano() / 1000000

	// 创建市价单（使用IOC限价单模拟）
	createOrderTx := &types.CreateOrderTxReq{
		MarketIndex:      marketIndex,
		ClientOrderIndex: clientOrderIndex,
		BaseAmount:       baseAmount,
		Price:            lighterPrice,
		IsAsk:            0, // 0=买入(多仓)
		Type:             txtypes.LimitOrder,
		TimeInForce:      txtypes.ImmediateOrCancel,
		ReduceOnly:       0,
		TriggerPrice:     txtypes.NilOrderTriggerPrice,
		OrderExpiry:      txtypes.NilOrderExpiry,
	}

	txInfo, err := t.txClient.GetCreateOrderTransaction(createOrderTx, nil)
	if err != nil {
		return nil, fmt.Errorf("开多仓失败: %w", err)
	}

	txHash, err := t.txClient.SendRawTx(t.ctx, txInfo, nil)
	if err != nil {
		return nil, fmt.Errorf("提交开多仓交易失败: %w", err)
	}

	log.Printf("✓ 开多仓成功: %s 数量: %.4f tx: %s", symbol, quantity, txHash)

	result := make(map[string]interface{})
	result["orderId"] = clientOrderIndex
	result["symbol"] = symbol
	result["status"] = "PENDING"
	result["hash"] = txHash

	return result, nil
}

// OpenShort 开空仓
func (t *LighterTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	// 先取消该币种的所有委托单
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("  ⚠ 取消旧委托单失败: %v", err)
	}

	// 设置杠杆
	if err := t.SetLeverage(symbol, leverage); err != nil {
		return nil, err
	}

	marketIndex, err := t.getMarketIndex(symbol)
	if err != nil {
		return nil, err
	}

	// 获取当前价格
	price, err := t.GetMarketPrice(symbol)
	if err != nil {
		return nil, err
	}

	baseAmount := t.convertToBaseAmount(symbol, quantity)
	lighterPrice := t.convertToLighterPrice(symbol, price*0.99)

	clientOrderIndex := time.Now().UnixNano() / 1000000

	createOrderTx := &types.CreateOrderTxReq{
		MarketIndex:      marketIndex,
		ClientOrderIndex: clientOrderIndex,
		BaseAmount:       baseAmount,
		Price:            lighterPrice,
		IsAsk:            1, // 1=卖出(空仓)
		Type:             txtypes.LimitOrder,
		TimeInForce:      txtypes.ImmediateOrCancel,
		ReduceOnly:       0,
		TriggerPrice:     txtypes.NilOrderTriggerPrice,
		OrderExpiry:      txtypes.NilOrderExpiry,
	}

	txInfo, err := t.txClient.GetCreateOrderTransaction(createOrderTx, nil)
	if err != nil {
		return nil, fmt.Errorf("开空仓失败: %w", err)
	}

	txHash, err := t.txClient.SendRawTx(t.ctx, txInfo, nil)
	if err != nil {
		return nil, fmt.Errorf("提交开空仓交易失败: %w", err)
	}

	log.Printf("✓ 开空仓成功: %s 数量: %.4f tx: %s", symbol, quantity, txHash)

	result := make(map[string]interface{})
	result["orderId"] = clientOrderIndex
	result["symbol"] = symbol
	result["status"] = "PENDING"
	result["hash"] = txHash

	return result, nil
}

// CloseLong 平多仓
func (t *LighterTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	// 如果数量为0，获取当前持仓数量
	if quantity == 0 {
		positions, err := t.GetPositions()
		if err != nil {
			return nil, err
		}

		for _, pos := range positions {
			if pos["symbol"] == symbol && pos["side"] == "long" {
				quantity = pos["positionAmt"].(float64)
				break
			}
		}

		if quantity == 0 {
			return nil, fmt.Errorf("没有找到 %s 的多仓", symbol)
		}
	}

	marketIndex, err := t.getMarketIndex(symbol)
	if err != nil {
		return nil, err
	}

	price, err := t.GetMarketPrice(symbol)
	if err != nil {
		return nil, err
	}

	baseAmount := t.convertToBaseAmount(symbol, quantity)
	lighterPrice := t.convertToLighterPrice(symbol, price*0.99)
	clientOrderIndex := time.Now().UnixNano() / 1000000

	createOrderTx := &types.CreateOrderTxReq{
		MarketIndex:      marketIndex,
		ClientOrderIndex: clientOrderIndex,
		BaseAmount:       baseAmount,
		Price:            lighterPrice,
		IsAsk:            1, // 卖出平多
		Type:             txtypes.LimitOrder,
		TimeInForce:      txtypes.ImmediateOrCancel,
		ReduceOnly:       1,
		TriggerPrice:     txtypes.NilOrderTriggerPrice,
		OrderExpiry:      txtypes.NilOrderExpiry,
	}

	txInfo, err := t.txClient.GetCreateOrderTransaction(createOrderTx, nil)
	if err != nil {
		return nil, fmt.Errorf("平多仓失败: %w", err)
	}

	txHash, err := t.txClient.SendRawTx(t.ctx, txInfo, nil)
	if err != nil {
		return nil, fmt.Errorf("提交平多仓交易失败: %w", err)
	}

	log.Printf("✓ 平多仓成功: %s 数量: %.4f", symbol, quantity)

	// 平仓后取消该币种的所有挂单
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("  ⚠ 取消挂单失败: %v", err)
	}

	result := make(map[string]interface{})
	result["orderId"] = clientOrderIndex
	result["symbol"] = symbol
	result["status"] = "PENDING"
	result["hash"] = txHash

	return result, nil
}

// CloseShort 平空仓
func (t *LighterTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	// 如果数量为0，获取当前持仓数量
	if quantity == 0 {
		positions, err := t.GetPositions()
		if err != nil {
			return nil, err
		}

		for _, pos := range positions {
			if pos["symbol"] == symbol && pos["side"] == "short" {
				quantity = pos["positionAmt"].(float64)
				break
			}
		}

		if quantity == 0 {
			return nil, fmt.Errorf("没有找到 %s 的空仓", symbol)
		}
	}

	marketIndex, err := t.getMarketIndex(symbol)
	if err != nil {
		return nil, err
	}

	price, err := t.GetMarketPrice(symbol)
	if err != nil {
		return nil, err
	}

	baseAmount := t.convertToBaseAmount(symbol, quantity)
	lighterPrice := t.convertToLighterPrice(symbol, price*1.01)
	clientOrderIndex := time.Now().UnixNano() / 1000000

	createOrderTx := &types.CreateOrderTxReq{
		MarketIndex:      marketIndex,
		ClientOrderIndex: clientOrderIndex,
		BaseAmount:       baseAmount,
		Price:            lighterPrice,
		IsAsk:            0, // 买入平空
		Type:             txtypes.LimitOrder,
		TimeInForce:      txtypes.ImmediateOrCancel,
		ReduceOnly:       1,
		TriggerPrice:     txtypes.NilOrderTriggerPrice,
		OrderExpiry:      txtypes.NilOrderExpiry,
	}

	txInfo, err := t.txClient.GetCreateOrderTransaction(createOrderTx, nil)
	if err != nil {
		return nil, fmt.Errorf("平空仓失败: %w", err)
	}

	txHash, err := t.txClient.SendRawTx(t.ctx, txInfo, nil)
	if err != nil {
		return nil, fmt.Errorf("提交平空仓交易失败: %w", err)
	}

	log.Printf("✓ 平空仓成功: %s 数量: %.4f", symbol, quantity)

	// 平仓后取消该币种的所有挂单
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("  ⚠ 取消挂单失败: %v", err)
	}

	result := make(map[string]interface{})
	result["orderId"] = clientOrderIndex
	result["symbol"] = symbol
	result["status"] = "PENDING"
	result["hash"] = txHash

	return result, nil
}

// CancelAllOrders 取消该币种的所有挂单
func (t *LighterTrader) CancelAllOrders(symbol string) error {
	cancelAllTx := &types.CancelAllOrdersTxReq{
		TimeInForce: txtypes.ImmediateCancelAll,
		Time:        time.Now().UnixMilli(),
	}

	txInfo, err := t.txClient.GetCancelAllOrdersTransaction(cancelAllTx, nil)
	if err != nil {
		return fmt.Errorf("取消挂单失败: %w", err)
	}

	txHash, err := t.txClient.SendRawTx(t.ctx, txInfo, nil)
	if err != nil {
		return fmt.Errorf("提交取消挂单交易失败: %w", err)
	}

	log.Printf("  ✓ 已取消所有挂单 tx: %s", txHash)
	return nil
}

// GetMarketPrice 获取市场价格
func (t *LighterTrader) GetMarketPrice(symbol string) (float64, error) {
	coin := convertSymbolToLighterCoin(symbol)
	
	marketInfo, exists := t.marketInfoCache[coin]
	if !exists {
		return 0, fmt.Errorf("未找到市场 %s 的信息", symbol)
	}
	
	// 调用 OrderBookDetails 接口
	details, err := t.restClient.OrderBookDetails(t.ctx, &lighterapi.OrderBookDetailsParams{
		MarketId: marketInfo.MarketId,
	})
	if err != nil {
		return 0, fmt.Errorf("获取市场价格失败: %w", err)
	}

	// 尝试使用 MarkPrice
	if details.MarkPrice != nil {
		markPrice, _ := strconv.ParseFloat(*details.MarkPrice, 64)
		if markPrice > 0 {
			return markPrice, nil
		}
	}

	// 使用买卖盘中间价
	var askPrice, bidPrice float64
	if details.Asks != nil && len(*details.Asks) > 0 {
		if (*details.Asks)[0].Price != nil {
			askPrice, _ = strconv.ParseFloat(*(*details.Asks)[0].Price, 64)
		}
	}
	if details.Bids != nil && len(*details.Bids) > 0 {
		if (*details.Bids)[0].Price != nil {
			bidPrice, _ = strconv.ParseFloat(*(*details.Bids)[0].Price, 64)
		}
	}
	
	if askPrice > 0 && bidPrice > 0 {
		return (askPrice + bidPrice) / 2, nil
	}
	
	return 0, fmt.Errorf("无法获取 %s 的价格", symbol)
}

// SetStopLoss 设置止损单
func (t *LighterTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	marketIndex, err := t.getMarketIndex(symbol)
	if err != nil {
		return err
	}

	isAsk := uint8(0)
	if positionSide == "LONG" {
		isAsk = 1
	}

	baseAmount := t.convertToBaseAmount(symbol, quantity)
	lighterPrice := t.convertToLighterPrice(symbol, stopPrice)
	triggerPrice := t.convertToLighterPrice(symbol, stopPrice)
	clientOrderIndex := time.Now().UnixNano() / 1000000
	orderExpiry := time.Now().Add(30 * 24 * time.Hour).UnixMilli()

	createOrderTx := &types.CreateOrderTxReq{
		MarketIndex:      marketIndex,
		ClientOrderIndex: clientOrderIndex,
		BaseAmount:       baseAmount,
		Price:            lighterPrice,
		IsAsk:            isAsk,
		Type:             txtypes.StopLossOrder,
		TimeInForce:      txtypes.ImmediateOrCancel,
		ReduceOnly:       1,
		TriggerPrice:     triggerPrice,
		OrderExpiry:      orderExpiry,
	}

	txInfo, err := t.txClient.GetCreateOrderTransaction(createOrderTx, nil)
	if err != nil {
		return fmt.Errorf("设置止损失败: %w", err)
	}

	txHash, err := t.txClient.SendRawTx(t.ctx, txInfo, nil)
	if err != nil {
		return fmt.Errorf("提交止损交易失败: %w", err)
	}

	log.Printf("  止损价设置: %.4f tx: %s", stopPrice, txHash)
	return nil
}

// SetTakeProfit 设置止盈单
func (t *LighterTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	marketIndex, err := t.getMarketIndex(symbol)
	if err != nil {
		return err
	}

	isAsk := uint8(0)
	if positionSide == "LONG" {
		isAsk = 1
	}

	baseAmount := t.convertToBaseAmount(symbol, quantity)
	lighterPrice := t.convertToLighterPrice(symbol, takeProfitPrice)
	triggerPrice := t.convertToLighterPrice(symbol, takeProfitPrice)
	clientOrderIndex := time.Now().UnixNano() / 1000000
	orderExpiry := time.Now().Add(30 * 24 * time.Hour).UnixMilli()

	createOrderTx := &types.CreateOrderTxReq{
		MarketIndex:      marketIndex,
		ClientOrderIndex: clientOrderIndex,
		BaseAmount:       baseAmount,
		Price:            lighterPrice,
		IsAsk:            isAsk,
		Type:             txtypes.TakeProfitOrder,
		TimeInForce:      txtypes.ImmediateOrCancel,
		ReduceOnly:       1,
		TriggerPrice:     triggerPrice,
		OrderExpiry:      orderExpiry,
	}

	txInfo, err := t.txClient.GetCreateOrderTransaction(createOrderTx, nil)
	if err != nil {
		return fmt.Errorf("设置止盈失败: %w", err)
	}

	txHash, err := t.txClient.SendRawTx(t.ctx, txInfo, nil)
	if err != nil {
		return fmt.Errorf("提交止盈交易失败: %w", err)
	}

	log.Printf("  止盈价设置: %.4f tx: %s", takeProfitPrice, txHash)
	return nil
}

// FormatQuantity 格式化数量到正确的精度
func (t *LighterTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	coin := convertSymbolToLighterCoin(symbol)
	
	if marketInfo, exists := t.marketInfoCache[coin]; exists {
		formatStr := fmt.Sprintf("%%.%df", *marketInfo.SupportedSizeDecimals)
		return fmt.Sprintf(formatStr, quantity), nil
	}
	
	return fmt.Sprintf("%.4f", quantity), nil
}

// ===== 辅助方法 =====

// loadMarketInfo 加载市场信息到缓存
func (t *LighterTrader) loadMarketInfo() error {
	orderBooks, err := t.restClient.OrderBooks(t.ctx, nil)
	if err != nil {
		return fmt.Errorf("获取市场信息失败: %w", err)
	}

	if orderBooks.OrderBooks == nil {
		return fmt.Errorf("未返回市场信息")
	}

	// 将市场信息存入缓存
	for i := range *orderBooks.OrderBooks {
		market := &(*orderBooks.OrderBooks)[i]
		if market.Symbol != nil {
			t.marketInfoCache[*market.Symbol] = market
		}
	}

	log.Printf("✅ 加载了 %d 个市场信息", len(*orderBooks.OrderBooks))
	return nil
}

// getMarketIndex 获取市场索引
func (t *LighterTrader) getMarketIndex(symbol string) (uint8, error) {
	coin := convertSymbolToLighterCoin(symbol)
	
	marketInfo, exists := t.marketInfoCache[coin]
	if !exists {
		// 尝试重新加载市场信息
		if err := t.loadMarketInfo(); err != nil {
			return 0, fmt.Errorf("未找到市场 %s 的索引", symbol)
		}
		marketInfo, exists = t.marketInfoCache[coin]
		if !exists {
			return 0, fmt.Errorf("未找到市场 %s 的索引", symbol)
		}
	}
	
	return *marketInfo.MarketId, nil
}

// convertToBaseAmount 将数量转换为Lighter的base amount格式
func (t *LighterTrader) convertToBaseAmount(symbol string, quantity float64) int64 {
	coin := convertSymbolToLighterCoin(symbol)
	
	if marketInfo, exists := t.marketInfoCache[coin]; exists && marketInfo.SupportedSizeDecimals != nil {
		multiplier := 1.0
		for i := uint8(0); i < *marketInfo.SupportedSizeDecimals; i++ {
			multiplier *= 10.0
		}
		return int64(quantity * multiplier)
	}
	
	return int64(quantity * 10000)
}

// convertToLighterPrice 将价格转换为Lighter格式
func (t *LighterTrader) convertToLighterPrice(symbol string, price float64) uint32 {
	coin := convertSymbolToLighterCoin(symbol)
	
	if marketInfo, exists := t.marketInfoCache[coin]; exists && marketInfo.SupportedPriceDecimals != nil {
		multiplier := 1.0
		for i := uint8(0); i < *marketInfo.SupportedPriceDecimals; i++ {
			multiplier *= 10.0
		}
		return uint32(price * multiplier)
	}
	
	return uint32(price * 10000)
}

// convertSymbolToLighterCoin 将标准symbol转换为Lighter coin名称
func convertSymbolToLighterCoin(symbol string) string {
	if len(symbol) > 4 && symbol[len(symbol)-4:] == "USDT" {
		return symbol[:len(symbol)-4]
	}
	return symbol
}

// absFloat64 返回float64的绝对值
func absFloat64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

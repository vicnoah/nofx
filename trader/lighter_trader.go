package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	lighterClient "github.com/elliottech/lighter-go/client"
	lighterHTTP "github.com/elliottech/lighter-go/client/http"
	"github.com/elliottech/lighter-go/types"
	"github.com/elliottech/lighter-go/types/txtypes"
)

// LighterTrader Lighter交易所交易器
type LighterTrader struct {
	txClient     *lighterClient.TxClient
	httpClient   lighterClient.MinimalHTTPClient
	ctx          context.Context
	accountIndex int64
	apiKeyIndex  uint8
	chainID      uint32
	endpoint     string // API端点，用于获取市场数据
	
	// 市场信息缓存
	marketInfoCache map[string]*lighterOrderBook // symbol -> market info
}

// lighterAccountPosition 持仓信息
type lighterAccountPosition struct {
	MarketID              uint8  `json:"market_id"`
	Symbol                string `json:"symbol"`
	InitialMarginFraction string `json:"initial_margin_fraction"`
	Sign                  int32  `json:"sign"`
	Position              string `json:"position"`
	AvgEntryPrice         string `json:"avg_entry_price"`
	PositionValue         string `json:"position_value"`
	UnrealizedPnl         string `json:"unrealized_pnl"`
	RealizedPnl           string `json:"realized_pnl"`
	LiquidationPrice      string `json:"liquidation_price"`
	MarginMode            int32  `json:"margin_mode"`
	AllocatedMargin       string `json:"allocated_margin"`
}

// lighterAccount 账户信息
type lighterAccount struct {
	Index            int64                      `json:"index"`
	AvailableBalance string                     `json:"available_balance"`
	Collateral       string                     `json:"collateral"`
	Positions        []lighterAccountPosition   `json:"positions"`
}

// lighterAccountResponse 账户API响应
type lighterAccountResponse struct {
	Code     int32             `json:"code"`
	Message  string            `json:"message"`
	Total    int64             `json:"total"`
	Accounts []lighterAccount  `json:"accounts"`
}

// lighterOrderBook 订单簿信息
type lighterOrderBook struct {
	Symbol                  string `json:"symbol"`
	MarketID                uint8  `json:"market_id"`
	Status                  string `json:"status"`
	TakerFee                string `json:"taker_fee"`
	MakerFee                string `json:"maker_fee"`
	LiquidationFee          string `json:"liquidation_fee"`
	MinBaseAmount           string `json:"min_base_amount"`
	MinQuoteAmount          string `json:"min_quote_amount"`
	SupportedSizeDecimals   uint8  `json:"supported_size_decimals"`
	SupportedPriceDecimals  uint8  `json:"supported_price_decimals"`
	SupportedQuoteDecimals  uint8  `json:"supported_quote_decimals"`
}

// lighterOrderBooksResponse 订单簿API响应
type lighterOrderBooksResponse struct {
	Code       int32               `json:"code"`
	Message    string              `json:"message"`
	OrderBooks []lighterOrderBook  `json:"order_books"`
}

// LighterConfig Lighter交易器配置
type LighterConfig struct {
	Endpoint       string // API端点 (例如: "https://testnet.zklighter.elliot.ai" 或 "https://api.lighter.xyz")
	APIKeyPrivKey  string // API密钥私钥 (hex格式)
	AccountIndex   int64  // 账户索引
	APIKeyIndex    uint8  // API密钥索引
	ChainID        uint32 // 链ID (testnet=1 mainnet=2)
}

// NewLighterTrader 创建Lighter交易器
func NewLighterTrader(config LighterConfig) (*LighterTrader, error) {
	// 创建HTTP客户端
	httpClient := lighterHTTP.NewClient(config.Endpoint)
	if httpClient == nil {
		return nil, fmt.Errorf("创建HTTP客户端失败")
	}

	// 创建交易客户端
	txClient, err := lighterClient.NewTxClient(
		httpClient,
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
		txClient:        txClient,
		httpClient:      httpClient,
		ctx:             context.Background(),
		accountIndex:    config.AccountIndex,
		apiKeyIndex:     config.APIKeyIndex,
		chainID:         config.ChainID,
		endpoint:        config.Endpoint,
		marketInfoCache: make(map[string]*lighterOrderBook),
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

	// 调用 /api/v1/account 接口
	url := fmt.Sprintf("%s/api/v1/account?by=index&value=%d", t.endpoint, t.accountIndex)
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("❌ Lighter API调用失败: %v", err)
		return nil, fmt.Errorf("获取账户信息失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API返回错误: %d, %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var accountResp lighterAccountResponse
	if err := json.Unmarshal(body, &accountResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if accountResp.Code != 200 {
		return nil, fmt.Errorf("API返回错误: %s", accountResp.Message)
	}

	if len(accountResp.Accounts) == 0 {
		return nil, fmt.Errorf("未找到账户信息")
	}

	account := accountResp.Accounts[0]

	// 解析余额和抵押品
	availableBalance, _ := strconv.ParseFloat(account.AvailableBalance, 64)
	collateral, _ := strconv.ParseFloat(account.Collateral, 64)

	// 计算未实现盈亏
	totalUnrealizedPnl := 0.0
	for _, pos := range account.Positions {
		unrealizedPnl, _ := strconv.ParseFloat(pos.UnrealizedPnl, 64)
		totalUnrealizedPnl += unrealizedPnl
	}

	// Lighter的collateral已经包含了所有资产（可用余额 + 使用中的保证金）
	// 为了兼容auto_trader.go的计算逻辑：totalEquity = totalWalletBalance + totalUnrealizedProfit
	// 我们需要返回不包含未实现盈亏的钱包余额
	walletBalanceWithoutUnrealized := collateral - totalUnrealizedPnl

	result := make(map[string]interface{})
	result["totalWalletBalance"] = walletBalanceWithoutUnrealized // 钱包余额（不含未实现盈亏）
	result["availableBalance"] = availableBalance                  // 可用余额
	result["totalUnrealizedProfit"] = totalUnrealizedPnl           // 未实现盈亏

	log.Printf("✅ Lighter 账户: 总净值=%.2f (钱包%.2f+未实现%.2f), 可用=%.2f",
		collateral,
		walletBalanceWithoutUnrealized,
		totalUnrealizedPnl,
		availableBalance)

	return result, nil
}

// GetPositions 获取所有持仓
func (t *LighterTrader) GetPositions() ([]map[string]interface{}, error) {
	// 调用 /api/v1/account 接口获取持仓信息
	url := fmt.Sprintf("%s/api/v1/account?by=index&value=%d", t.endpoint, t.accountIndex)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("获取账户信息失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API返回错误: %d, %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var accountResp lighterAccountResponse
	if err := json.Unmarshal(body, &accountResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if accountResp.Code != 200 {
		return nil, fmt.Errorf("API返回错误: %s", accountResp.Message)
	}

	if len(accountResp.Accounts) == 0 {
		return []map[string]interface{}{}, nil
	}

	account := accountResp.Accounts[0]
	var result []map[string]interface{}

	// 遍历所有持仓
	for _, pos := range account.Positions {
		position, _ := strconv.ParseFloat(pos.Position, 64)
		
		// 跳过无持仓的
		if position == 0 {
			continue
		}

		posMap := make(map[string]interface{})
		
		// 标准化symbol格式（Lighter使用如"ETH"，我们转换为"ETHUSDT"）
		symbol := pos.Symbol + "USDT"
		posMap["symbol"] = symbol

		// 持仓数量和方向
		if pos.Sign > 0 {
			posMap["side"] = "long"
			posMap["positionAmt"] = position
		} else {
			posMap["side"] = "short"
			posMap["positionAmt"] = absFloat64(position)
		}

		// 价格信息
		entryPrice, _ := strconv.ParseFloat(pos.AvgEntryPrice, 64)
		positionValue, _ := strconv.ParseFloat(pos.PositionValue, 64)
		unrealizedPnl, _ := strconv.ParseFloat(pos.UnrealizedPnl, 64)
		liquidationPrice, _ := strconv.ParseFloat(pos.LiquidationPrice, 64)
		
		// 计算mark price（positionValue / abs(position)）
		var markPrice float64
		if position != 0 {
			markPrice = positionValue / absFloat64(position)
		}

		// 从 InitialMarginFraction 计算杠杆（IMF = 1 / leverage）
		imf, _ := strconv.ParseFloat(pos.InitialMarginFraction, 64)
		var leverage float64
		if imf > 0 {
			leverage = 100.0 / imf // IMF是百分比形式
		}

		posMap["entryPrice"] = entryPrice
		posMap["markPrice"] = markPrice
		posMap["unRealizedProfit"] = unrealizedPnl
		posMap["leverage"] = leverage
		posMap["liquidationPrice"] = liquidationPrice

		result = append(result, posMap)
	}

	return result, nil
}

// SetMarginMode 设置仓位模式
// Lighter使用InitialMarginFraction来控制杠杆，而不是直接的仓位模式
// 这个方法在Lighter中可能不需要实现，或者通过UpdateLeverage来间接实现
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

	// 计算InitialMarginFraction
	// IMF = 1 / leverage
	// Lighter使用万分之一作为单位，所以需要乘以10000
	imf := uint16(10000 / leverage)

	// 设置为全仓模式（CrossMargin = 0）
	updateLeverageTx := &types.UpdateLeverageTxReq{
		MarketIndex:           marketIndex,
		InitialMarginFraction: imf,
		MarginMode:            txtypes.CrossMargin, // 默认使用全仓模式
	}

	txInfo, err := t.txClient.GetUpdateLeverageTransaction(updateLeverageTx, nil)
	if err != nil {
		return fmt.Errorf("设置杠杆失败: %w", err)
	}

	// 发送交易（需要实现发送逻辑）
	// TODO: 实现sendTransaction方法
	log.Printf("  ✓ %s 杠杆交易已创建: hash=%s (leverage=%dx, imf=%d)", 
		symbol, txInfo.SignedHash, leverage, imf)

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

	// 计算base amount (Lighter使用最小单位)
	baseAmount := t.convertToBaseAmount(symbol, quantity)

	// 转换价格为Lighter格式
	lighterPrice := t.convertToLighterPrice(symbol, price*1.01) // 稍微激进的价格

	// 生成客户端订单ID（使用时间戳）
	clientOrderIndex := time.Now().UnixNano() / 1000000 // 毫秒时间戳

	// 创建市价单（使用IOC限价单模拟）
	createOrderTx := &types.CreateOrderTxReq{
		MarketIndex:      marketIndex,
		ClientOrderIndex: clientOrderIndex,
		BaseAmount:       baseAmount,
		Price:            lighterPrice,
		IsAsk:            0, // 0=买入(多仓), 1=卖出
		Type:             txtypes.LimitOrder,
		TimeInForce:      txtypes.ImmediateOrCancel, // IOC类似市价单
		ReduceOnly:       0,
		TriggerPrice:     txtypes.NilOrderTriggerPrice,
		OrderExpiry:      txtypes.NilOrderExpiry,
	}

	txInfo, err := t.txClient.GetCreateOrderTransaction(createOrderTx, nil)
	if err != nil {
		return nil, fmt.Errorf("开多仓失败: %w", err)
	}

	log.Printf("✓ 开多仓成功: %s 数量: %.4f hash: %s", symbol, quantity, txInfo.SignedHash)

	result := make(map[string]interface{})
	result["orderId"] = clientOrderIndex
	result["symbol"] = symbol
	result["status"] = "PENDING"
	result["hash"] = txInfo.SignedHash

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

	// 计算base amount
	baseAmount := t.convertToBaseAmount(symbol, quantity)

	// 转换价格为Lighter格式
	lighterPrice := t.convertToLighterPrice(symbol, price*0.99) // 稍微激进的价格

	// 生成客户端订单ID
	clientOrderIndex := time.Now().UnixNano() / 1000000

	// 创建市价单（使用IOC限价单模拟）
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

	log.Printf("✓ 开空仓成功: %s 数量: %.4f hash: %s", symbol, quantity, txInfo.SignedHash)

	result := make(map[string]interface{})
	result["orderId"] = clientOrderIndex
	result["symbol"] = symbol
	result["status"] = "PENDING"
	result["hash"] = txInfo.SignedHash

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

	// 获取当前价格
	price, err := t.GetMarketPrice(symbol)
	if err != nil {
		return nil, err
	}

	baseAmount := t.convertToBaseAmount(symbol, quantity)
	lighterPrice := t.convertToLighterPrice(symbol, price*0.99)
	clientOrderIndex := time.Now().UnixNano() / 1000000

	// 创建平仓订单（卖出 + ReduceOnly）
	createOrderTx := &types.CreateOrderTxReq{
		MarketIndex:      marketIndex,
		ClientOrderIndex: clientOrderIndex,
		BaseAmount:       baseAmount,
		Price:            lighterPrice,
		IsAsk:            1, // 卖出平多
		Type:             txtypes.LimitOrder,
		TimeInForce:      txtypes.ImmediateOrCancel,
		ReduceOnly:       1, // 只平仓
		TriggerPrice:     txtypes.NilOrderTriggerPrice,
		OrderExpiry:      txtypes.NilOrderExpiry,
	}

	txInfo, err := t.txClient.GetCreateOrderTransaction(createOrderTx, nil)
	if err != nil {
		return nil, fmt.Errorf("平多仓失败: %w", err)
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
	result["hash"] = txInfo.SignedHash

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

	// 获取当前价格
	price, err := t.GetMarketPrice(symbol)
	if err != nil {
		return nil, err
	}

	baseAmount := t.convertToBaseAmount(symbol, quantity)
	lighterPrice := t.convertToLighterPrice(symbol, price*1.01)
	clientOrderIndex := time.Now().UnixNano() / 1000000

	// 创建平仓订单（买入 + ReduceOnly）
	createOrderTx := &types.CreateOrderTxReq{
		MarketIndex:      marketIndex,
		ClientOrderIndex: clientOrderIndex,
		BaseAmount:       baseAmount,
		Price:            lighterPrice,
		IsAsk:            0, // 买入平空
		Type:             txtypes.LimitOrder,
		TimeInForce:      txtypes.ImmediateOrCancel,
		ReduceOnly:       1, // 只平仓
		TriggerPrice:     txtypes.NilOrderTriggerPrice,
		OrderExpiry:      txtypes.NilOrderExpiry,
	}

	txInfo, err := t.txClient.GetCreateOrderTransaction(createOrderTx, nil)
	if err != nil {
		return nil, fmt.Errorf("平空仓失败: %w", err)
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
	result["hash"] = txInfo.SignedHash

	return result, nil
}

// CancelAllOrders 取消该币种的所有挂单
func (t *LighterTrader) CancelAllOrders(symbol string) error {
	// 使用CancelAllOrders交易
	cancelAllTx := &types.CancelAllOrdersTxReq{
		TimeInForce: txtypes.ImmediateCancelAll,
		Time:        time.Now().UnixMilli(),
	}

	txInfo, err := t.txClient.GetCancelAllOrdersTransaction(cancelAllTx, nil)
	if err != nil {
		return fmt.Errorf("取消挂单失败: %w", err)
	}

	log.Printf("  ✓ 已取消所有挂单 hash: %s", txInfo.SignedHash)
	return nil
}

// GetMarketPrice 获取市场价格
func (t *LighterTrader) GetMarketPrice(symbol string) (float64, error) {
	// 从symbol提取coin（去掉USDT后缀）
	coin := convertSymbolToLighterCoin(symbol)
	
	// 从缓存获取市场信息
	marketInfo, exists := t.marketInfoCache[coin]
	if !exists {
		return 0, fmt.Errorf("未找到市场 %s 的信息", symbol)
	}
	
	// 调用 /api/v1/orderbooks 接口获取订单簿详情（包含最新价格）
	url := fmt.Sprintf("%s/api/v1/orderBookDetails?market_id=%d", t.endpoint, marketInfo.MarketID)
	resp, err := http.Get(url)
	if err != nil {
		return 0, fmt.Errorf("获取市场价格失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("API返回错误: %d, %s", resp.StatusCode, string(body))
	}

	// 简单解析，获取mid price或使用asks[0]和bids[0]计算中间价
	var orderBookDetail map[string]interface{}
	if err := json.Unmarshal(body, &orderBookDetail); err != nil {
		return 0, fmt.Errorf("解析响应失败: %w", err)
	}
	
	// 尝试从mark_price字段获取（如果有）
	if markPriceStr, ok := orderBookDetail["mark_price"].(string); ok {
		if markPrice, err := strconv.ParseFloat(markPriceStr, 64); err == nil {
			return markPrice, nil
		}
	}
	
	// 如果没有mark_price，使用asks和bids计算中间价
	var askPrice, bidPrice float64
	if asks, ok := orderBookDetail["asks"].([]interface{}); ok && len(asks) > 0 {
		if ask, ok := asks[0].(map[string]interface{}); ok {
			if priceStr, ok := ask["price"].(string); ok {
				askPrice, _ = strconv.ParseFloat(priceStr, 64)
			}
		}
	}
	if bids, ok := orderBookDetail["bids"].([]interface{}); ok && len(bids) > 0 {
		if bid, ok := bids[0].(map[string]interface{}); ok {
			if priceStr, ok := bid["price"].(string); ok {
				bidPrice, _ = strconv.ParseFloat(priceStr, 64)
			}
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
		isAsk = 1 // 多仓止损=卖出
	}

	baseAmount := t.convertToBaseAmount(symbol, quantity)
	lighterPrice := t.convertToLighterPrice(symbol, stopPrice)
	triggerPrice := t.convertToLighterPrice(symbol, stopPrice)
	clientOrderIndex := time.Now().UnixNano() / 1000000

	// 设置订单过期时间（30天后）
	orderExpiry := time.Now().Add(30 * 24 * time.Hour).UnixMilli()

	// 创建止损单
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

	log.Printf("  止损价设置: %.4f hash: %s", stopPrice, txInfo.SignedHash)
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
		isAsk = 1 // 多仓止盈=卖出
	}

	baseAmount := t.convertToBaseAmount(symbol, quantity)
	lighterPrice := t.convertToLighterPrice(symbol, takeProfitPrice)
	triggerPrice := t.convertToLighterPrice(symbol, takeProfitPrice)
	clientOrderIndex := time.Now().UnixNano() / 1000000

	// 设置订单过期时间（30天后）
	orderExpiry := time.Now().Add(30 * 24 * time.Hour).UnixMilli()

	// 创建止盈单
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

	log.Printf("  止盈价设置: %.4f hash: %s", takeProfitPrice, txInfo.SignedHash)
	return nil
}

// FormatQuantity 格式化数量到正确的精度
func (t *LighterTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	// TODO: 根据市场信息获取精度
	return fmt.Sprintf("%.4f", quantity), nil
}

// ===== 辅助方法 =====

// loadMarketInfo 加载市场信息到缓存
func (t *LighterTrader) loadMarketInfo() error {
	url := fmt.Sprintf("%s/api/v1/orderBooks", t.endpoint)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("获取市场信息失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API返回错误: %d, %s", resp.StatusCode, string(body))
	}

	var orderBooksResp lighterOrderBooksResponse
	if err := json.Unmarshal(body, &orderBooksResp); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}

	if orderBooksResp.Code != 200 {
		return fmt.Errorf("API返回错误: %s", orderBooksResp.Message)
	}

	// 将市场信息存入缓存
	for i := range orderBooksResp.OrderBooks {
		market := &orderBooksResp.OrderBooks[i]
		t.marketInfoCache[market.Symbol] = market
	}

	log.Printf("✅ 加载了 %d 个市场信息", len(orderBooksResp.OrderBooks))
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
	
	return marketInfo.MarketID, nil
}

// convertToBaseAmount 将数量转换为Lighter的base amount格式
func (t *LighterTrader) convertToBaseAmount(symbol string, quantity float64) int64 {
	coin := convertSymbolToLighterCoin(symbol)
	
	// 从缓存获取精度信息
	if marketInfo, exists := t.marketInfoCache[coin]; exists {
		// 使用supported_size_decimals计算
		multiplier := 1.0
		for i := uint8(0); i < marketInfo.SupportedSizeDecimals; i++ {
			multiplier *= 10.0
		}
		return int64(quantity * multiplier)
	}
	
	// 默认使用4位小数
	return int64(quantity * 10000)
}

// convertToLighterPrice 将价格转换为Lighter格式
func (t *LighterTrader) convertToLighterPrice(symbol string, price float64) uint32 {
	coin := convertSymbolToLighterCoin(symbol)
	
	// 从缓存获取精度信息
	if marketInfo, exists := t.marketInfoCache[coin]; exists {
		// 使用supported_price_decimals计算
		multiplier := 1.0
		for i := uint8(0); i < marketInfo.SupportedPriceDecimals; i++ {
			multiplier *= 10.0
		}
		return uint32(price * multiplier)
	}
	
	// 默认使用4位小数
	return uint32(price * 10000)
}

// convertSymbolToLighterCoin 将标准symbol转换为Lighter coin名称
func convertSymbolToLighterCoin(symbol string) string {
	// 去掉USDT后缀
	if len(symbol) > 4 && symbol[len(symbol)-4:] == "USDT" {
		return symbol[:len(symbol)-4]
	}
	return symbol
}

// debugJSON 辅助函数：打印JSON调试信息
func debugJSON(prefix string, v interface{}) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Printf("%s: JSON序列化失败: %v", prefix, err)
		return
	}
	log.Printf("%s:\n%s", prefix, string(data))
}

// absInt64 返回int64的绝对值
func absInt64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// absFloat64 返回float64的绝对值
func absFloat64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
	}
	return symbol
}

// debugJSON 辅助函数：打印JSON调试信息
func debugJSON(prefix string, v interface{}) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Printf("%s: JSON序列化失败: %v", prefix, err)
		return
	}
	log.Printf("%s:\n%s", prefix, string(data))
}

// absInt64 返回int64的绝对值
func absInt64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// absFloat64 返回float64的绝对值
func absFloat64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

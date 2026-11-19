// 支付服务：发起支付（alipay/wechat/epay/balance）+ 三个回调 + 支付成功事务。
//
// 事务边界（核心）：
//   - 三渠道回调与余额支付共用 settleOrderPaid：单事务内
//     支付记录 SUCCESS → 订单 PAID（悲观锁+状态机守卫）→ 佣金流水；
//     事务提交后发布 order.paid 事件（成就/通知/邀请域监听）。
//   - PAID 幂等：支付记录已 SUCCESS 或订单已 PAID 时直接返回成功。
//
// 凭据：全部从 config 表（key-value）读取，带内容 hash 缓存，配置更新事件触发重载。
package service

import (
	"crypto/md5"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/queue"
	"github.com/smartwalle/alipay/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// dbLocking SELECT ... FOR UPDATE（MySQL/PG 通用悲观锁子句）。
func dbLocking() clause.Locking { return clause.Locking{Strength: "UPDATE"} }

// CommissionConfig 分成配置（对齐原 commission.service.getCommissionConfig）。
type CommissionConfig struct {
	InviterRate  float64 // 默认 0.05（邀请分成 5%）
	PlatformRate float64 // 默认 0.10
	AuthorRate   float64 // 默认 0.85
}

// PaymentService 支付业务。
type PaymentService struct {
	DB    *gorm.DB
	asynq *queue.Client
	order *OrderService

	// SDK 凭据缓存（config hash 变更时重建）
	mu        sync.Mutex
	cfgHash   string
	payCfg    PayConfig
	alipaySDK *alipay.Client
}

// PayConfig config 表聚合出的支付配置（key 见 loadPayConfig）。
type PayConfig struct {
	NotifyURL string
	ReturnURL string

	AlipayEnabled bool
	Alipay        AlipayCfg
	WechatEnabled bool
	Wechat        WechatCfg
	EpayEnabled   bool
	Epay          EpayCfg
}

// AlipayCfg 支付宝配置。
type AlipayCfg struct {
	AppID      string
	PrivateKey string
	PublicKey  string // 支付宝公钥（验签用）
	Gateway    string
}

// WechatCfg 微信支付 v3 配置。
type WechatCfg struct {
	AppID      string
	MchID      string
	PrivateKey string // 商户 API 私钥 PEM
	SerialNo   string // 商户证书序列号
	APIv3Key   string
	PublicKey  string // 微信平台公钥/证书 PEM（回调验签）
}

// EpayCfg 易支付配置。
type EpayCfg struct {
	AppID     string
	AppKey    string
	Gateway   string
	NotifyURL string
}

// NewPaymentService 构造。
func NewPaymentService(db *gorm.DB, asynq *queue.Client) *PaymentService {
	return &PaymentService{DB: db, asynq: asynq, order: NewOrderService(db)}
}

// ---------- config 表读取 ----------

// loadPayConfig 从 config 表读支付凭据（每次调用现读，简单可靠；配置项少，开销可忽略）。
func (s *PaymentService) loadPayConfig() PayConfig {
	var rows []model.ConfigKV
	if err := s.DB.Where("\"group\" IN ?", []string{"payment", "pay"}).Find(&rows).Error; err != nil && s.cfgHash == "" {
		// config 表可能尚未建表：降级为空配置
	}
	kv := map[string]string{}
	for _, r := range rows {
		kv[r.Key] = r.Value
	}
	get := func(k, def string) string {
		if v, ok := kv[k]; ok && v != "" {
			return v
		}
		return def
	}
	boolv := func(k string) bool { return get(k, "") == "true" }

	cfg := PayConfig{
		NotifyURL: get("payment_notify_url", ""),
		ReturnURL: get("payment_return_url", ""),
		AlipayEnabled: boolv("alipay_enabled"),
		Alipay: AlipayCfg{
			AppID:      get("alipay_app_id", ""),
			PrivateKey: get("alipay_private_key", ""),
			PublicKey:  get("alipay_public_key", ""),
			Gateway:    get("alipay_gateway", "https://openapi.alipay.com/gateway.do"),
		},
		WechatEnabled: boolv("wechat_enabled"),
		Wechat: WechatCfg{
			AppID:      get("wechat_app_id", ""),
			MchID:      get("wechat_mch_id", ""),
			PrivateKey: get("wechat_private_key", ""),
			SerialNo:   get("wechat_serial_no", ""),
			APIv3Key:   get("wechat_api_v3_key", ""),
			PublicKey:  get("wechat_public_key", ""),
		},
		EpayEnabled: boolv("epay_enabled"),
		Epay: EpayCfg{
			AppID:     get("epay_app_id", ""),
			AppKey:    get("epay_app_key", ""),
			Gateway:   get("epay_gateway", ""),
			NotifyURL: get("epay_notify_url", ""),
		},
	}
	// 内容 hash 缓存：与原 lastConfigHash 语义一致
	raw, _ := json.Marshal(cfg)
	sum := md5.Sum(raw)
	hash := hex.EncodeToString(sum[:])
	s.mu.Lock()
	if hash != s.cfgHash {
		s.cfgHash = hash
		s.payCfg = cfg
		s.alipaySDK = nil // 触发重建
	}
	s.mu.Unlock()
	return cfg
}

// getAlipayClient 懒构建支付宝客户端（smartwalle/alipay/v3，验签自带）。
func (s *PaymentService) getAlipayClient(cfg PayConfig) (*alipay.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.alipaySDK != nil {
		return s.alipaySDK, nil
	}
	if cfg.Alipay.AppID == "" || cfg.Alipay.PrivateKey == "" {
		return nil, errcode.ErrBadRequest.With("支付宝配置不完整")
	}
	client, err := alipay.New(cfg.Alipay.AppID, cfg.Alipay.PrivateKey, !strings.Contains(cfg.Alipay.Gateway, "sandbox"))
	if err != nil {
		return nil, errcode.ErrBadRequest.Withf("支付宝 SDK 初始化失败: %v", err)
	}
	if err = client.LoadAliPayPublicKey(cfg.Alipay.PublicKey); err != nil {
		return nil, errcode.ErrBadRequest.Withf("支付宝公钥加载失败: %v", err)
	}
	s.alipaySDK = client
	return client, nil
}

// getCommissionConfig 分成费率（config 表 commission 组；默认 5%/10%/85%）。
func (s *PaymentService) getCommissionConfig() CommissionConfig {
	var rows []model.ConfigKV
	_ = s.DB.Where("\"group\" = ?", "commission").Find(&rows).Error
	cc := CommissionConfig{InviterRate: 0.05, PlatformRate: 0.10, AuthorRate: 0.85}
	for _, r := range rows {
		var v float64
		if _, err := fmt.Sscanf(r.Value, "%f", &v); err != nil {
			continue
		}
		switch r.Key {
		case "commission_inviter_rate":
			cc.InviterRate = v
		case "commission_platform_rate":
			cc.PlatformRate = v
		case "commission_author_rate":
			cc.AuthorRate = v
		}
	}
	return cc
}

// ---------- 发起支付 ----------

// CreatePaymentInput POST /payment/create 入参。
type CreatePaymentInput struct {
	OrderID       string `json:"order_id" binding:"required"`
	PaymentMethod string `json:"payment_method" binding:"required,oneof=ALIPAY WECHAT EPAY BALANCE"`
	ReturnURL     string `json:"return_url"`
	Type          string `json:"type"` // epay 子类型：alipay/wxpay/usdt
}

// CreatePayment 创建支付，返回支付凭据（paymentUrl / codeUrl）。
func (s *PaymentService) CreatePayment(userID string, in CreatePaymentInput) (map[string]interface{}, error) {
	var order model.Order
	if err := s.DB.First(&order, "id = ? AND user_id = ?", in.OrderID, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.ErrNotFound.With("订单不存在")
		}
		return nil, err
	}
	if order.Status == model.OrderStatusPaid {
		return nil, errcode.ErrBadRequest.With("订单已支付")
	}

	cfg := s.loadPayConfig()
	switch in.PaymentMethod {
	case model.PayMethodAlipay:
		if !cfg.AlipayEnabled {
			return nil, errcode.ErrBadRequest.With("支付宝支付未启用")
		}
	case model.PayMethodWechat:
		if !cfg.WechatEnabled {
			return nil, errcode.ErrBadRequest.With("微信支付未启用")
		}
	case model.PayMethodEpay:
		if !cfg.EpayEnabled {
			return nil, errcode.ErrBadRequest.With("易支付未启用")
		}
	case model.PayMethodBalance:
		// 余额支付无需外部凭据
	default:
		return nil, errcode.ErrBadRequest.With("不支持的支付方式")
	}

	record := &model.Payment{
		OrderID:       order.ID,
		UserID:        userID,
		PaymentMethod: in.PaymentMethod,
		Amount:        order.Amount,
		Status:        model.PaymentStatusPending,
		Details:       map[string]interface{}{"orderNo": order.OrderNo, "amount": order.Amount},
	}
	if err := s.DB.Create(record).Error; err != nil {
		return nil, err
	}

	switch in.PaymentMethod {
	case model.PayMethodAlipay:
		return s.createAlipayPayment(record, &order, cfg, in.ReturnURL)
	case model.PayMethodWechat:
		return s.createWechatPayment(record, &order, cfg)
	case model.PayMethodEpay:
		return s.createEpayPayment(record, &order, cfg, in.ReturnURL, in.Type)
	default: // BALANCE
		return s.createBalancePayment(record, &order)
	}
}

// createAlipayPayment 支付宝电脑网站支付（alipay.trade.page.pay）。
func (s *PaymentService) createAlipayPayment(record *model.Payment, order *model.Order, cfg PayConfig, returnUrl string) (map[string]interface{}, error) {
	client, err := s.getAlipayClient(cfg)
	if err != nil {
		return nil, err
	}
	var p = alipay.TradePagePay{}
	p.OutTradeNo = order.OrderNo
	p.TotalAmount = fmt.Sprintf("%.2f", order.Amount)
	p.Subject = order.Title
	p.ProductCode = "FAST_INSTIAL_TRADE_PAY"
	p.ProductCode = "FAST_INSTANT_TRADE_PAY"
	p.NotifyURL = cfg.NotifyURL
	p.ReturnURL = returnUrl
	if p.ReturnURL == "" {
		p.ReturnURL = cfg.ReturnURL
	}
	payURL, err := client.TradePagePay(p)
	if err != nil {
		return nil, errcode.ErrBadRequest.Withf("创建支付宝支付失败: %v", err)
	}
	_ = s.DB.Model(record).Update("details", map[string]interface{}{
		"alipayUrl": payURL.String(), "orderNo": order.OrderNo, "amount": order.Amount,
	}).Error
	return map[string]interface{}{
		"paymentId": record.ID, "paymentUrl": payURL.String(), "paymentMethod": "ALIPAY",
	}, nil
}

// createWechatPayment 微信 Native 下单（v3；SDK 证书管理与验签自带）。
func (s *PaymentService) createWechatPayment(record *model.Payment, order *model.Order, cfg PayConfig) (map[string]interface{}, error) {
	w := cfg.Wechat
	if w.AppID == "" || w.MchID == "" || w.PrivateKey == "" {
		return nil, errcode.ErrBadRequest.With("微信支付配置不完整")
	}
	codeURL, prepayID, err := s.wechatNativeOrder(&w, order, cfg.NotifyURL+"/wechat")
	if err != nil {
		return nil, errcode.ErrBadRequest.Withf("创建微信支付失败: %v", err)
	}
	_ = s.DB.Model(record).Update("details", map[string]interface{}{
		"wechatCodeUrl": codeURL, "orderNo": order.OrderNo, "amount": order.Amount,
		"appId": w.AppID, "mchId": w.MchID, "prepayId": prepayID,
	}).Error
	return map[string]interface{}{
		"paymentId": record.ID, "codeUrl": codeURL, "paymentMethod": "WECHAT",
	}, nil
}

// wechatNativeOrder 调微信 v3 Native 下单（wechatpay-go 官方 SDK）。
func (s *PaymentService) wechatNativeOrder(w *WechatCfg, order *model.Order, notifyURL string) (codeURL, prepayID string, err error) {
	privKey, err := parseRSAPrivateKeyPEM(w.PrivateKey)
	if err != nil {
		return "", "", err
	}
	ctx := contextBackground()
	// 懒初始化/downloader 证书自动更新由 SDK 承担
	h := &wechatPayHelper{mchID: w.MchID, serialNo: w.SerialNo, privateKey: privKey, apiV3Key: w.APIv3Key}
	return h.nativeOrder(ctx, order, notifyURL)
}

// createEpayPayment 易支付（MD5 签名跳转）。
func (s *PaymentService) createEpayPayment(record *model.Payment, order *model.Order, cfg PayConfig, returnUrl, payType string) (map[string]interface{}, error) {
	e := cfg.Epay
	if e.AppID == "" || e.AppKey == "" || e.Gateway == "" {
		return nil, errcode.ErrBadRequest.With("易支付配置不完整")
	}
	if payType == "" {
		payType = "alipay"
	}
	params := map[string]string{
		"pid":          e.AppID,
		"type":         payType,
		"out_trade_no": order.OrderNo,
		"notify_url":   e.NotifyURL,
		"return_url":   returnUrl,
		"name":         order.Title,
		"money":        fmt.Sprintf("%.2f", order.Amount),
		"sign_type":    "MD5",
	}
	if params["return_url"] == "" {
		params["return_url"] = cfg.ReturnURL
	}
	params["sign"] = epaySign(params, e.AppKey)

	vals := url.Values{}
	for k, v := range params {
		vals.Set(k, v)
	}
	payURL := strings.TrimRight(e.Gateway, "/") + "/submit.php?" + vals.Encode()
	_ = s.DB.Model(record).Update("details", map[string]interface{}{
		"epayUrl": payURL, "orderNo": order.OrderNo, "amount": order.Amount, "appId": e.AppID,
	}).Error
	return map[string]interface{}{
		"paymentId": record.ID, "paymentUrl": payURL, "paymentMethod": "EPAY",
	}, nil
}

// epaySign 易支付 MD5 签名：按 key 升序拼接 a=b&c=d（排除 sign/sign_type/空值）+ appKey。
func epaySign(params map[string]string, appKey string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if v == "" || k == "sign" || k == "sign_type" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	sum := md5.Sum([]byte(strings.Join(parts, "&") + appKey))
	return hex.EncodeToString(sum[:])
}

// createBalancePayment 余额支付：单事务（扣余额→支付记录 SUCCESS→订单 PAID→佣金流水）。
func (s *PaymentService) createBalancePayment(record *model.Payment, order *model.Order) (map[string]interface{}, error) {
	var balance float64
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var user model.UserPaymentView
		if err := tx.Clauses(dbLocking()).First(&user, "id = ?", order.UserID).Error; err != nil {
			return errcode.ErrNotFound.With("用户不存在")
		}
		if user.Wallet < order.Amount {
			return errcode.ErrInsufficientWallet
		}
		newBalance := user.Wallet - order.Amount
		if err := tx.Model(&model.UserPaymentView{}).Where("id = ?", user.ID).
			Update("wallet", newBalance).Error; err != nil {
			return err
		}
		balance = newBalance

		now := time.Now()
		record.Status = model.PaymentStatusSuccess
		record.PaidAt = &now
		record.Details = map[string]interface{}{
			"balancePayment": true, "previousBalance": user.Wallet, "currentBalance": newBalance, "amount": order.Amount,
		}
		if err := tx.Save(record).Error; err != nil {
			return err
		}
		// 订单 PAID + 佣金流水（同事务）
		_, _, err := s.settleInTx(tx, record, order, model.PayMethodBalance, "")
		return err
	})
	if err != nil {
		_ = s.DB.Model(record).Updates(map[string]interface{}{
			"status": model.PaymentStatusFailed, "error_message": err.Error(),
		}).Error
		return nil, err
	}
	return map[string]interface{}{
		"paymentId": record.ID, "status": "SUCCESS", "paymentMethod": "BALANCE", "balance": balance,
	}, nil
}

// ---------- 支付成功落账（回调/余额共用，单事务） ----------

// settleInTx 事务内：订单 PAID（状态机守卫+悲观锁）→ 佣金流水。
// 返回 (order, commission, err)。订单已 PAID 时幂等返回（err=nil）。
func (s *PaymentService) settleInTx(tx *gorm.DB, record *model.Payment, order *model.Order, method, thirdNo string) (*model.Order, *event.CommissionInfo, error) {
	paid, err := MarkPaidInTx(tx, order.ID, method, thirdNo)
	if err != nil {
		return nil, nil, err
	}
	// 佣金流水：仅当订单从未 PAID（幂等回调不会重复写，order_id 唯一索引兜底）
	if paid.PaidAt != nil && record.Status == model.PaymentStatusSuccess {
		commission, err := s.writeCommissionInTx(tx, paid)
		if err != nil {
			return nil, nil, err
		}
		return paid, commission, nil
	}
	return paid, nil, nil
}

// writeCommissionInTx 事务内写佣金流水 + 更新邀请人钱包/收益。
func (s *PaymentService) writeCommissionInTx(tx *gorm.DB, order *model.Order) (*event.CommissionInfo, error) {
	// 幂等：该订单已有流水则直接返回
	var exist model.CommissionRecord
	err := tx.First(&exist, "order_id = ?", order.ID).Error
	if err == nil {
		info := event.CommissionInfo{InviterID: exist.InviterID, InviterAmount: exist.InviterAmount, CommissionRecordID: exist.ID}
		return &info, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	cc := s.getCommissionConfig()
	info := &event.CommissionInfo{
		AuthorRate:     cc.AuthorRate,
		AuthorAmount:   order.Amount * cc.AuthorRate,
		PlatformRate:   cc.PlatformRate,
		PlatformAmount: order.Amount * cc.PlatformRate,
	}

	// 作者入账（平台单 authorId=1 也入账）
	if order.AuthorID != "" {
		if err := tx.Model(&model.UserPaymentView{}).Where("id = ?", order.AuthorID).
			Update("wallet", gorm.Expr("wallet + ?", info.AuthorAmount)).Error; err != nil {
			return nil, err
		}
	}

	// 邀请分成：买家有邀请人时按 inviterRate 分成
	now := time.Now()
	rec := &model.CommissionRecord{
		OrderID: order.ID, OrderNo: order.OrderNo,
		UserID: order.UserID, AuthorID: order.AuthorID,
		OrderType: order.Type, OrderAmount: order.Amount,
		InviterRate: cc.InviterRate, AuthorRate: cc.AuthorRate, PlatformRate: cc.PlatformRate,
		AuthorAmount: info.AuthorAmount, PlatformAmount: info.PlatformAmount,
		Status: "PAID", PaidAt: &now,
	}
	var buyer model.UserPaymentView
	if err := tx.First(&buyer, "id = ?", order.UserID).Error; err == nil && buyer.InviterID != "" {
		rec.InviterID = buyer.InviterID
		rec.InviterAmount = order.Amount * cc.InviterRate
		info.InviterID = buyer.InviterID
		info.InviterRate = cc.InviterRate
		info.InviterAmount = rec.InviterAmount
		if err := tx.Model(&model.UserPaymentView{}).Where("id = ?", buyer.InviterID).Updates(map[string]interface{}{
			"wallet":          gorm.Expr("wallet + ?", rec.InviterAmount),
			"invite_earnings": gorm.Expr("invite_earnings + ?", rec.InviterAmount),
		}).Error; err != nil {
			return nil, err
		}
	}
	if err := tx.Create(rec).Error; err != nil {
		return nil, err
	}
	info.CommissionRecordID = rec.ID
	return info, nil
}

// settleOrderPaid 回调收口：验证后的落账（新事务）+ 事件发布。
func (s *PaymentService) settleOrderPaid(orderNo, method, thirdNo string) (*model.Order, error) {
	var order model.Order
	if err := s.DB.First(&order, "order_no = ?", orderNo).Error; err != nil {
		return nil, errcode.ErrNotFound.With("订单不存在")
	}
	var record model.Payment
	if err := s.DB.First(&record, "order_id = ? AND payment_method = ?", order.ID, method).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.ErrNotFound.With("支付记录不存在")
		}
		return nil, err
	}
	// 幂等：支付记录已成功
	if record.Status == model.PaymentStatusSuccess {
		return &order, nil
	}

	now := time.Now()
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		record.Status = model.PaymentStatusSuccess
		record.ThirdPartyOrderNo = thirdNo
		record.PaidAt = &now
		if err := tx.Save(&record).Error; err != nil {
			return err
		}
		paid, commission, err := s.settleInTx(tx, &record, &order, method, thirdNo)
		if err != nil {
			return err
		}
		order = *paid
		publishOrderPaid(paid, commission)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// ---------- 三个回调 ----------

// HandleAlipayNotify 支付宝回调（SDK 验签自带）。
func (s *PaymentService) HandleAlipayNotify(form url.Values) (bool, string) {
	cfg := s.loadPayConfig()
	client, err := s.getAlipayClient(cfg)
	if err != nil {
		return false, "SDK 未初始化"
	}
	notify, err := client.GetTradeNotification(form) // 内部完成验签
	if err != nil {
		return false, "签名验证失败"
	}
	if notify.TradeStatus != alipay.TradeStatusSuccess && notify.TradeStatus != alipay.TradeStatusFinished {
		return true, ""
	}
	if _, err := s.settleOrderPaid(notify.OutTradeNo, model.PayMethodAlipay, notify.TradeNo); err != nil {
		return false, err.Error()
	}
	return true, ""
}

// HandleWechatNotify 微信 v3 回调（SDK 验签自带）。
func (s *PaymentService) HandleWechatNotify(headers map[string]string, body []byte) (bool, string) {
	cfg := s.loadPayConfig()
	w := cfg.Wechat
	if w.MchID == "" || w.APIv3Key == "" {
		return false, "微信支付配置不完整"
	}
	outTradeNo, tradeID, err := s.wechatVerifyNotify(&w, headers, body)
	if err != nil {
		return false, "签名验证失败: " + err.Error()
	}
	if outTradeNo == "" {
		return true, ""
	}
	if _, err := s.settleOrderPaid(outTradeNo, model.PayMethodWechat, tradeID); err != nil {
		return false, err.Error()
	}
	return true, ""
}

// HandleEpayNotify 易支付回调（GET query；MD5 验签）。
func (s *PaymentService) HandleEpayNotify(query map[string]string) (bool, string) {
	cfg := s.loadPayConfig()
	e := cfg.Epay
	if e.AppKey == "" {
		return false, "易支付配置不完整"
	}
	expect := epaySign(query, e.AppKey)
	if query["sign"] != expect {
		return false, "签名验证失败"
	}
	if query["trade_status"] != "TRADE_SUCCESS" {
		return true, ""
	}
	if _, err := s.settleOrderPaid(query["out_trade_no"], model.PayMethodEpay, query["trade_no"]); err != nil {
		return false, err.Error()
	}
	return true, ""
}

// ---------- 支付记录查询 ----------

// GetPaymentRecord 支付记录详情。
func (s *PaymentService) GetPaymentRecord(id, userID string) (*model.Payment, error) {
	var p model.Payment
	if err := s.DB.First(&p, "id = ? AND user_id = ?", id, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.ErrNotFound.With("支付记录不存在")
		}
		return nil, err
	}
	return &p, nil
}

// GetPaymentByOrder 按订单查支付记录。
func (s *PaymentService) GetPaymentByOrder(orderID, userID string) (*model.Payment, error) {
	var p model.Payment
	if err := s.DB.First(&p, "order_id = ? AND user_id = ?", orderID, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// ListUserPayments 用户支付记录分页。
func (s *PaymentService) ListUserPayments(userID string, page, limit int) ([]model.Payment, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	db := s.DB.Model(&model.Payment{}).Where("user_id = ?", userID)
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.Payment
	if err := db.Order("created_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// ---------- 辅助 ----------

// parseRSAPrivateKeyPEM 解析 PEM 私钥（PKCS#1 / PKCS#8）。
func parseRSAPrivateKeyPEM(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("私钥 PEM 解析失败")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	k8, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	if k, ok := k8.(*rsa.PrivateKey); ok {
		return k, nil
	}
	return nil, errors.New("非 RSA 私钥")
}

func contextBackground() contextType { return contextType{} }

// contextType 占位（微信 SDK 需要 context.Context；见 wechatPayHelper）。
type contextType = struct{}

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
	"context"
	"crypto/md5"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/hibiken/asynq"
	"github.com/smartwalle/alipay/v3"
	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
	"github.com/wechatpay-apiv3/wechatpay-go/core/downloader"
	"github.com/wechatpay-apiv3/wechatpay-go/core/notify"
	"github.com/wechatpay-apiv3/wechatpay-go/core/option"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
	"github.com/wechatpay-apiv3/wechatpay-go/utils"
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
	asynq *asynq.Client
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
func NewPaymentService(db *gorm.DB, c *asynq.Client) *PaymentService {
	return &PaymentService{DB: db, asynq: c, order: NewOrderService(db)}
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
		NotifyURL:     get("payment_notify_url", ""),
		ReturnURL:     get("payment_return_url", ""),
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
		return nil, errcode.ErrBadRequest.WithKey(i18n.PaymentConfigMissing)
	}
	client, err := alipay.New(cfg.Alipay.AppID, cfg.Alipay.PrivateKey, !strings.Contains(cfg.Alipay.Gateway, "sandbox"))
	if err != nil {
		return nil, errcode.ErrBadRequest.WithKeyf(i18n.PaymentAlipaySDKInitFailed, err)
	}
	if err = client.LoadAliPayPublicKey(cfg.Alipay.PublicKey); err != nil {
		return nil, errcode.ErrBadRequest.WithKeyf(i18n.PaymentAlipayPubkeyFailed, err)
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
	OrderID       string `json:"orderId" binding:"required"`
	PaymentMethod string `json:"paymentMethod" binding:"required,oneof=ALIPAY WECHAT EPAY BALANCE"`
	ReturnURL     string `json:"returnUrl"`
	Type          string `json:"type"` // epay 子类型：alipay/wxpay/usdt
}

// CreatePayment 创建支付，返回支付凭据（paymentUrl / codeUrl）。
func (s *PaymentService) CreatePayment(userID string, in CreatePaymentInput) (map[string]interface{}, error) {
	var order model.Order
	if err := s.DB.First(&order, "id = ? AND user_id = ?", in.OrderID, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.ErrNotFound.WithKey(i18n.OrderNotFound)
		}
		return nil, err
	}
	if order.Status == model.OrderStatusPaid {
		return nil, errcode.ErrBadRequest.WithKey(i18n.OrderAlreadyPaid)
	}

	cfg := s.loadPayConfig()
	switch in.PaymentMethod {
	case model.PayMethodAlipay:
		if !cfg.AlipayEnabled {
			return nil, errcode.ErrBadRequest.WithKey(i18n.PaymentChannelDisabled)
		}
	case model.PayMethodWechat:
		if !cfg.WechatEnabled {
			return nil, errcode.ErrBadRequest.WithKey(i18n.PaymentChannelDisabled)
		}
	case model.PayMethodEpay:
		if !cfg.EpayEnabled {
			return nil, errcode.ErrBadRequest.WithKey(i18n.PaymentChannelDisabled)
		}
	case model.PayMethodBalance:
		// 余额支付无需外部凭据
	default:
		return nil, errcode.ErrBadRequest.WithKeyf(i18n.PaymentNotSupported, in.PaymentMethod)
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
		return nil, errcode.ErrBadRequest.WithKeyf(i18n.PaymentAlipayCreateFailed, err)
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
		return nil, errcode.ErrBadRequest.WithKey(i18n.PaymentConfigMissing)
	}
	codeURL, prepayID, err := s.wechatNativeOrder(&w, order, cfg.NotifyURL+"/wechat")
	if err != nil {
		return nil, errcode.ErrBadRequest.WithKeyf(i18n.PaymentWechatCreateFailed, err)
	}
	_ = s.DB.Model(record).Update("details", map[string]interface{}{
		"wechatCodeUrl": codeURL, "orderNo": order.OrderNo, "amount": order.Amount,
		"appId": w.AppID, "mchId": w.MchID, "prepayId": prepayID,
	}).Error
	return map[string]interface{}{
		"paymentId": record.ID, "codeUrl": codeURL, "paymentMethod": "WECHAT",
	}, nil
}

// wechatNativeOrder 调微信 v3 Native 下单（wechatpay-go 官方 SDK；
// 平台证书自动更新由 WithWechatPayAutoAuthCipher 承担）。
func (s *PaymentService) wechatNativeOrder(w *WechatCfg, order *model.Order, notifyURL string) (codeURL, prepayID string, err error) {
	privKey, err := utils.LoadPrivateKey(w.PrivateKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse WeChat merchant private key: %w", err)
	}
	ctx := context.Background()
	client, err := core.NewClient(ctx,
		option.WithWechatPayAutoAuthCipher(w.MchID, w.SerialNo, privKey, w.APIv3Key))
	if err != nil {
		return "", "", fmt.Errorf("failed to initialize WeChat pay client: %w", err)
	}
	svc := native.NativeApiService{Client: client}
	resp, _, err := svc.Prepay(ctx, native.PrepayRequest{
		Appid:       core.String(w.AppID),
		Mchid:       core.String(w.MchID),
		Description: core.String(order.Title),
		OutTradeNo:  core.String(order.OrderNo),
		NotifyUrl:   core.String(notifyURL),
		Amount:      &native.Amount{Total: core.Int64(int64(order.Amount * 100))},
	})
	if err != nil {
		return "", "", err
	}
	if resp.CodeUrl == nil {
		return "", "", errors.New("WeChat prepay response missing code_url")
	}
	// v3 Native 下单响应只含 code_url；prepay_id 不含在本响应内
	return *resp.CodeUrl, "", nil
}

// createEpayPayment 易支付（MD5 签名跳转）。
func (s *PaymentService) createEpayPayment(record *model.Payment, order *model.Order, cfg PayConfig, returnUrl, payType string) (map[string]interface{}, error) {
	e := cfg.Epay
	if e.AppID == "" || e.AppKey == "" || e.Gateway == "" {
		return nil, errcode.ErrBadRequest.WithKey(i18n.PaymentConfigMissing)
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

// createBalancePayment 余额支付：单事务（扣余额→支付记录 SUCCESS→订单 PAID→佣金流水），
// 事务提交成功后才发布 order.paid 事件（订阅方读得到已提交数据）。
func (s *PaymentService) createBalancePayment(record *model.Payment, order *model.Order) (map[string]interface{}, error) {
	var balance float64
	var paidOrder *model.Order
	var commission *event.CommissionInfo
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var user model.UserPaymentView
		if err := tx.Clauses(dbLocking()).First(&user, "id = ?", order.UserID).Error; err != nil {
			return errcode.ErrNotFound.WithKey(i18n.UserNotFound)
		}
		if user.Wallet < order.Amount {
			return errcode.ErrInsufficientBalance
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
		p, c, err := s.settleInTx(tx, record, order, model.PayMethodBalance, "")
		if err != nil {
			return err
		}
		paidOrder, commission = p, c
		return nil
	})
	if err != nil {
		_ = s.DB.Model(record).Updates(map[string]interface{}{
			"status": model.PaymentStatusFailed, "error_message": err.Error(),
		}).Error
		return nil, err
	}
	// 事务已提交：发布事件（成就/通知/邀请域监听）
	if paidOrder != nil {
		publishOrderPaid(paidOrder, commission)
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

// CommissionSplit 一次分成计算的纯结果（金额已四舍五入到分）。
//
// 抽取为纯函数的目的：分成是最容易算错、也最需要单测的一块，
// 但它原先埋在「读 config 表 + 写 DB」的事务里，无法脱离数据库测试。
type CommissionSplit struct {
	AuthorAmount   float64
	PlatformAmount float64
	InviterAmount  float64
	AuthorRate     float64
	PlatformRate   float64
	InviterRate    float64
}

// round2 金额四舍五入到分（2 位小数）。
//
// 防什么 bug：19.9*0.85 = 16.914999999999999，直接落 decimal(10,2) 会被 PG
// 截断为 16.91，而 Go 里回读/累加用的是原始 float，两边对不上账；
// 统一先 round2 再写库，保证「余额累加值」与「流水落库值」完全一致。
func round2(v float64) float64 {
	if v > 0 {
		return math.Round(v*100) / 100
	}
	return -math.Round(-v*100) / 100 // 负数对称舍入
}

// splitCommission 计算三方分成（作者/平台/邀请人），全部 round2。
//
// 边界语义：
//   - amount = 0 → 三方金额均为 0（不产生负零）；
//   - 费率 0 → 该方金额 0；
//   - 费率 1 → 该方拿全额（作者 100% 时平台/邀请人为 0，取决于配置本身是否自洽）；
//   - inviterAmount 只在 hasInviter 时算出，否则为 0（避免「无邀请人也写 5% 分成」）。
func splitCommission(amount float64, cc CommissionConfig, hasInviter bool) CommissionSplit {
	out := CommissionSplit{
		AuthorRate:   cc.AuthorRate,
		PlatformRate: cc.PlatformRate,
		InviterRate:  cc.InviterRate,
	}
	if amount < 0 {
		amount = 0 // 防御：负金额不应进入分成（订单金额在创建时已校验 > 0）
	}
	out.AuthorAmount = round2(amount * cc.AuthorRate)
	out.PlatformAmount = round2(amount * cc.PlatformRate)
	if hasInviter {
		out.InviterAmount = round2(amount * cc.InviterRate)
	}
	return out
}

// isNotifyAlreadySettled 回调幂等判定：支付记录已 SUCCESS 即视为已落账。
//
// 防什么 bug：支付网关在未收到 success 应答时会重复回调（微信最多重试 15 次），
// 若每次都重新扣佣金/改订单，就会出现重复分成。此判定是幂等的第一道闸门。
func isNotifyAlreadySettled(record *model.Payment) bool {
	return record != nil && record.Status == model.PaymentStatusSuccess
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

	// 买家是否有邀请人（决定是否产生邀请分成）
	var buyer model.UserPaymentView
	hasInviter := tx.First(&buyer, "id = ?", order.UserID).Error == nil && buyer.InviterID != ""
	split := splitCommission(order.Amount, cc, hasInviter)

	info := &event.CommissionInfo{
		AuthorRate:     split.AuthorRate,
		AuthorAmount:   split.AuthorAmount,
		PlatformRate:   split.PlatformRate,
		PlatformAmount: split.PlatformAmount,
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
		InviterRate: split.InviterRate, AuthorRate: split.AuthorRate, PlatformRate: split.PlatformRate,
		AuthorAmount: split.AuthorAmount, PlatformAmount: split.PlatformAmount,
		Status: "PAID", PaidAt: &now,
	}
	if hasInviter {
		rec.InviterID = buyer.InviterID
		rec.InviterAmount = split.InviterAmount
		info.InviterID = buyer.InviterID
		info.InviterRate = split.InviterRate
		info.InviterAmount = split.InviterAmount
		if err := tx.Model(&model.UserPaymentView{}).Where("id = ?", buyer.InviterID).Updates(map[string]interface{}{
			"wallet":          gorm.Expr("wallet + ?", split.InviterAmount),
			"invite_earnings": gorm.Expr("invite_earnings + ?", split.InviterAmount),
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
		return nil, errcode.ErrNotFound.WithKey(i18n.OrderNotFound)
	}
	var record model.Payment
	if err := s.DB.First(&record, "order_id = ? AND payment_method = ?", order.ID, method).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.ErrNotFound.WithKey(i18n.PaymentRecordNotFound)
		}
		return nil, err
	}
	// 幂等：支付记录已成功
	if record.Status == model.PaymentStatusSuccess {
		return &order, nil
	}

	now := time.Now()
	var paidOrder *model.Order
	var commission *event.CommissionInfo
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		record.Status = model.PaymentStatusSuccess
		record.ThirdPartyOrderNo = thirdNo
		record.PaidAt = &now
		if err := tx.Save(&record).Error; err != nil {
			return err
		}
		paid, comm, err := s.settleInTx(tx, &record, &order, method, thirdNo)
		if err != nil {
			return err
		}
		paidOrder, commission = paid, comm
		return nil
	})
	if err != nil {
		return nil, err
	}
	if paidOrder != nil {
		order = *paidOrder
		publishOrderPaid(paidOrder, commission) // 事务提交后发布
	}
	return &order, nil
}

// ---------- 三个回调 ----------

// HandleAlipayNotify 支付宝回调（SDK 验签自带；r 为原始请求，SDK 内部解析 form 并验签）。
func (s *PaymentService) HandleAlipayNotify(r *http.Request) (bool, string) {
	cfg := s.loadPayConfig()
	client, err := s.getAlipayClient(cfg)
	if err != nil {
		return false, "Alipay SDK not initialized"
	}
	notification, err := client.GetTradeNotification(r) // 内部完成验签
	if err != nil {
		return false, "Signature verification failed"
	}
	if notification.TradeStatus != alipay.TradeStatusSuccess && notification.TradeStatus != alipay.TradeStatusFinished {
		return true, ""
	}
	if _, err := s.settleOrderPaid(notification.OutTradeNo, model.PayMethodAlipay, notification.TradeNo); err != nil {
		return false, err.Error()
	}
	return true, ""
}

// HandleWechatNotify 微信 v3 回调（SDK 验签自带；r 为原始请求，供 SDK 读取头与报文）。
func (s *PaymentService) HandleWechatNotify(r *http.Request) (bool, string) {
	cfg := s.loadPayConfig()
	w := cfg.Wechat
	if w.MchID == "" || w.APIv3Key == "" {
		return false, "WeChat pay is not configured"
	}
	ctx := context.Background()
	certVisitor := downloader.MgrInstance().GetCertificateVisitor(w.MchID)
	handler, err := notify.NewRSANotifyHandler(w.APIv3Key, verifiers.NewSHA256WithRSAVerifier(certVisitor))
	if err != nil {
		return false, "Failed to initialize notify handler: " + err.Error()
	}
	transaction := new(payments.Transaction)
	if _, err := handler.ParseNotifyRequest(ctx, r, transaction); err != nil {
		return false, "Signature verification failed: " + err.Error()
	}
	// 交易状态非 SUCCESS 不落账（幂等返回成功，微信不再重发）
	if transaction.TradeState == nil || *transaction.TradeState != "SUCCESS" {
		return true, ""
	}
	outTradeNo, tradeID := "", ""
	if transaction.OutTradeNo != nil {
		outTradeNo = *transaction.OutTradeNo
	}
	if transaction.TransactionId != nil {
		tradeID = *transaction.TransactionId
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
		return false, "Epay is not configured"
	}
	expect := epaySign(query, e.AppKey)
	if query["sign"] != expect {
		return false, "Signature verification failed"
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

// TestEpaySignature 易支付签名自测：按 config 表 appKey 计算 MD5 签名（不真实下单）。
func (s *PaymentService) TestEpaySignature(params map[string]string) (string, error) {
	cfg := s.loadPayConfig()
	if cfg.Epay.AppKey == "" {
		return "", errcode.ErrBadRequest.WithKey(i18n.PaymentConfigMissing)
	}
	if params == nil {
		params = map[string]string{}
	}
	return epaySign(params, cfg.Epay.AppKey), nil
}

// GetPaymentRecord 支付记录详情。
func (s *PaymentService) GetPaymentRecord(id, userID string) (*model.Payment, error) {
	var p model.Payment
	if err := s.DB.First(&p, "id = ? AND user_id = ?", id, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.ErrNotFound.WithKey(i18n.PaymentRecordNotFound)
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

// parseRSAPrivateKeyPEM 保留：本地解析 PEM 私钥（非 SDK 路径备用）。
func parseRSAPrivateKeyPEM(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("failed to parse private key PEM")
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
	return nil, errors.New("not an RSA private key")
}

// 支付域实体：支付记录（对齐原 payment.entity.ts）+ 佣金流水（交易域自有表，
// 邀请域如需对账可订阅 order.paid 事件或读本表）。
package model

import (
	"time"
)

// 支付状态。
const (
	PaymentStatusPending   = "PENDING"
	PaymentStatusSuccess   = "SUCCESS"
	PaymentStatusFailed    = "FAILED"
	PaymentStatusCancelled = "CANCELLED"
)

// 支付方式。
const (
	PayMethodAlipay  = "ALIPAY"
	PayMethodWechat  = "WECHAT"
	PayMethodEpay    = "EPAY"
	PayMethodBalance = "BALANCE"
)

// Payment 支付记录。
type Payment struct {
	BaseModel
	OrderID           string                 `gorm:"type:varchar(36);index;comment:订单ID" json:"order_id"`
	UserID            string                 `gorm:"type:varchar(36);index;comment:用户ID" json:"user_id"`
	PaymentMethod     string                 `gorm:"type:varchar(20);comment:支付方式" json:"payment_method"`
	Amount            float64                `gorm:"type:decimal(10,2);comment:支付金额（元）" json:"amount"`
	ThirdPartyOrderNo string                 `gorm:"type:varchar(128);comment:第三方支付订单号" json:"third_party_order_no"`
	Status            string                 `gorm:"type:varchar(20);index;default:PENDING;comment:支付状态" json:"status" example:"PUBLISHED"`
	PaidAt            *time.Time             `gorm:"comment:支付时间" json:"paid_at"`
	Details           map[string]interface{} `gorm:"type:jsonb;serializer:json;comment:支付详情" json:"details"`
	ErrorMessage      string                 `gorm:"type:text;comment:错误信息" json:"error_message"`
}

// TableName 表名。
func (Payment) TableName() string { return "payments" }

// CommissionRecord 佣金流水（支付成功事务内写入；邀请分成 5% 默认费率从 config 表读）。
type CommissionRecord struct {
	BaseModel
	OrderID        string     `gorm:"type:varchar(36);uniqueIndex;comment:订单ID（一笔订单一笔流水，天然幂等）" json:"order_id"`
	OrderNo        string     `gorm:"type:varchar(64);index" json:"order_no"`
	UserID         string     `gorm:"type:varchar(36);index;comment:买家ID" json:"user_id"`
	AuthorID       string     `gorm:"type:varchar(36);comment:作者ID" json:"author_id"`
	InviterID      string     `gorm:"type:varchar(36);index;comment:邀请人ID（无邀请关系为空）" json:"inviter_id"`
	OrderType      string     `gorm:"type:varchar(20)" json:"order_type"`
	OrderAmount    float64    `gorm:"type:decimal(10,2)" json:"order_amount"`
	InviterRate    float64    `gorm:"type:decimal(6,4);comment:邀请人分成比例" json:"inviter_rate"`
	InviterAmount  float64    `gorm:"type:decimal(10,2);comment:邀请人分成金额" json:"inviter_amount"`
	AuthorRate     float64    `gorm:"type:decimal(6,4)" json:"author_rate"`
	AuthorAmount   float64    `gorm:"type:decimal(10,2)" json:"author_amount"`
	PlatformRate   float64    `gorm:"type:decimal(6,4)" json:"platform_rate"`
	PlatformAmount float64    `gorm:"type:decimal(10,2)" json:"platform_amount"`
	Status         string     `gorm:"type:varchar(20);default:PAID" json:"status" example:"PUBLISHED"`
	PaidAt         *time.Time `json:"paid_at"`
}

// TableName 表名。
func (CommissionRecord) TableName() string { return "payment_commissions" }

// ConfigKV 系统配置表局部映射（key-value；权威模型归 infra/系统配置域，
// 此处仅声明支付域读取所需的列，AutoMigrate 由 infra 侧统一执行）。
type ConfigKV struct {
	Key   string `gorm:"type:varchar(128);primaryKey;column:key" json:"key"`
	Value string `gorm:"type:text;column:value" json:"value"`
	Group string `gorm:"type:varchar(64);column:group" json:"group"`
	Type  string `gorm:"type:varchar(20);column:type" json:"type" example:"image"`
}

// TableName 表名（对齐原 config 实体）。
func (ConfigKV) TableName() string { return "config" }

// UserPaymentView 用户表交易域局部视图（仅声明订单/支付域读写所需列；
// 权威 user 模型归 auth-user 域，本结构不参与 AutoMigrate）。
type UserPaymentView struct {
	ID                  string     `gorm:"type:varchar(36);primaryKey;column:id" json:"id"`
	Wallet              float64    `gorm:"type:decimal(10,2);column:wallet" json:"wallet" example:"100.50"`
	InviterID           string     `gorm:"type:varchar(36);column:inviter_id" json:"inviter_id"`
	InviteEarnings      float64    `gorm:"type:decimal(10,2);column:invite_earnings" json:"invite_earnings"`
	MembershipStatus    string     `gorm:"type:varchar(20);column:membership_status" json:"membership_status"`
	MembershipLevel     int        `gorm:"column:membership_level" json:"membership_level"`
	MembershipLevelName string     `gorm:"type:varchar(64);column:membership_level_name" json:"membership_level_name"`
	MembershipStartDate *time.Time `gorm:"column:membership_start_date" json:"membership_start_date"`
	MembershipEndDate   *time.Time `gorm:"column:membership_end_date" json:"membership_end_date"`
}

// TableName 表名。
// 注意：用户主体表名是 user（单数），写成 users 会 relation does not exist（曾致 /order/wallet/balance 500）。
func (UserPaymentView) TableName() string { return "user" }

// PaymentModels 支付域迁移实体（含佣金流水；config/users 为跨域只读视图，不参与迁移）。
func PaymentModels() []interface{} { return []interface{}{&Payment{}, &CommissionRecord{}} }

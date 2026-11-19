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
	OrderID           string                 `gorm:"type:char(36);index;comment:订单ID" json:"orderId"`
	UserID            string                 `gorm:"type:char(36);index;comment:用户ID" json:"userId"`
	PaymentMethod     string                 `gorm:"type:varchar(20);comment:支付方式" json:"paymentMethod"`
	Amount            float64                `gorm:"type:decimal(10,2);comment:支付金额（元）" json:"amount"`
	ThirdPartyOrderNo string                 `gorm:"type:varchar(128);comment:第三方支付订单号" json:"thirdPartyOrderNo"`
	Status            string                 `gorm:"type:varchar(20);index;default:PENDING;comment:支付状态" json:"status"`
	PaidAt            *time.Time             `gorm:"comment:支付时间" json:"paidAt"`
	Details           map[string]interface{} `gorm:"type:jsonb;serializer:json;comment:支付详情" json:"details"`
	ErrorMessage      string                 `gorm:"type:text;comment:错误信息" json:"errorMessage"`
}

// TableName 表名。
func (Payment) TableName() string { return "payments" }

// CommissionRecord 佣金流水（支付成功事务内写入；邀请分成 5% 默认费率从 config 表读）。
type CommissionRecord struct {
	BaseModel
	OrderID        string     `gorm:"type:char(36);uniqueIndex;comment:订单ID（一笔订单一笔流水，天然幂等）" json:"orderId"`
	OrderNo        string     `gorm:"type:varchar(64);index" json:"orderNo"`
	UserID         string     `gorm:"type:char(36);index;comment:买家ID" json:"userId"`
	AuthorID       string     `gorm:"type:char(36);comment:作者ID" json:"authorId"`
	InviterID      string     `gorm:"type:char(36);index;comment:邀请人ID（无邀请关系为空）" json:"inviterId"`
	OrderType      string     `gorm:"type:varchar(20)" json:"orderType"`
	OrderAmount    float64    `gorm:"type:decimal(10,2)" json:"orderAmount"`
	InviterRate    float64    `gorm:"type:decimal(6,4);comment:邀请人分成比例" json:"inviterRate"`
	InviterAmount  float64    `gorm:"type:decimal(10,2);comment:邀请人分成金额" json:"inviterAmount"`
	AuthorRate     float64    `gorm:"type:decimal(6,4)" json:"authorRate"`
	AuthorAmount   float64    `gorm:"type:decimal(10,2)" json:"authorAmount"`
	PlatformRate   float64    `gorm:"type:decimal(6,4)" json:"platformRate"`
	PlatformAmount float64    `gorm:"type:decimal(10,2)" json:"platformAmount"`
	Status         string     `gorm:"type:varchar(20);default:PAID" json:"status"`
	PaidAt         *time.Time `json:"paidAt"`
}

// TableName 表名。
func (CommissionRecord) TableName() string { return "payment_commissions" }

// ConfigKV 系统配置表局部映射（key-value；权威模型归 infra/系统配置域，
// 此处仅声明支付域读取所需的列，AutoMigrate 由 infra 侧统一执行）。
type ConfigKV struct {
	Key   string `gorm:"type:varchar(128);primaryKey;column:key" json:"key"`
	Value string `gorm:"type:text;column:value" json:"value"`
	Group string `gorm:"type:varchar(64);column:group" json:"group"`
	Type  string `gorm:"type:varchar(20);column:type" json:"type"`
}

// TableName 表名（对齐原 config 实体）。
func (ConfigKV) TableName() string { return "config" }

// UserPaymentView 用户表交易域局部视图（仅声明订单/支付域读写所需列；
// 权威 user 模型归 auth-user 域，本结构不参与 AutoMigrate）。
type UserPaymentView struct {
	ID                  string     `gorm:"type:char(36);primaryKey;column:id" json:"id"`
	Wallet              float64    `gorm:"type:decimal(10,2);column:wallet" json:"wallet"`
	InviterID           string     `gorm:"type:char(36);column:inviter_id" json:"inviterId"`
	InviteEarnings      float64    `gorm:"type:decimal(10,2);column:invite_earnings" json:"inviteEarnings"`
	MembershipStatus    string     `gorm:"type:varchar(20);column:membership_status" json:"membershipStatus"`
	MembershipLevel     int        `gorm:"column:membership_level" json:"membershipLevel"`
	MembershipLevelName string     `gorm:"type:varchar(64);column:membership_level_name" json:"membershipLevelName"`
	MembershipStartDate *time.Time `gorm:"column:membership_start_date" json:"membershipStartDate"`
	MembershipEndDate   *time.Time `gorm:"column:membership_end_date" json:"membershipEndDate"`
}

// TableName 表名。
// 注意：用户主体表名是 user（单数），写成 users 会 relation does not exist（曾致 /order/wallet/balance 500）。
func (UserPaymentView) TableName() string { return "user" }

// PaymentModels 支付域迁移实体（含佣金流水；config/users 为跨域只读视图，不参与迁移）。
func PaymentModels() []interface{} { return []interface{}{&Payment{}, &CommissionRecord{}} }

// 邀请域实体（对齐原 invite 模块：邀请码 + 邀请分成记录）。
package model

import "time"

// 邀请状态（对齐原 enum）。
const (
	InviteStatusPending = "PENDING"
	InviteStatusUsed    = "USED"
	InviteStatusExpired = "EXPIRED"
)

// 邀请类型（对齐原 enum）。
const (
	InviteTypeGeneral = "GENERAL"
	InviteTypeVIP     = "VIP"
)

// 分成状态（对齐原 enum）。
const (
	InviteCommissionPending   = "PENDING"
	InviteCommissionPaid      = "PAID"
	InviteCommissionCancelled = "CANCELLED"
)

// Invite 邀请码。
type Invite struct {
	BaseModel
	InviterID      string     `gorm:"type:char(36);index;comment:邀请人ID" json:"inviterId"`
	InviteeID      string     `gorm:"type:char(36);index;comment:被邀请人ID" json:"inviteeId"`
	InviteCode     string     `gorm:"type:varchar(64);index;comment:邀请码" json:"inviteCode"`
	Status         string     `gorm:"type:varchar(20);default:PENDING;index;comment:状态" json:"status"`
	Type           string     `gorm:"type:varchar(20);default:GENERAL;comment:邀请类型" json:"type"`
	CommissionRate float64    `gorm:"type:decimal(3,2);default:0.05;comment:分成比例（0-1）" json:"commissionRate"`
	InviteURL      string     `gorm:"type:varchar(500);comment:邀请链接" json:"inviteUrl"`
	UsedAt         *time.Time `json:"usedAt"`
	ExpiredAt      *time.Time `json:"expiredAt"`
	Remark         string     `gorm:"type:text;comment:备注" json:"remark"`
}

// TableName 表名。
func (Invite) TableName() string { return "invites" }

// InviteCommission 邀请分成记录。
type InviteCommission struct {
	BaseModel
	InviteID         string     `gorm:"type:char(36);index;comment:邀请ID" json:"inviteId"`
	InviterID        string     `gorm:"type:char(36);index;comment:邀请人ID" json:"inviterId"`
	InviteeID        string     `gorm:"type:char(36);index;comment:被邀请人ID" json:"inviteeId"`
	OrderID          string     `gorm:"type:char(36);index;comment:订单ID" json:"orderId"`
	OrderType        string     `gorm:"type:varchar(20);comment:订单类型" json:"orderType"`
	OrderAmount      float64    `gorm:"type:decimal(10,2);comment:订单金额" json:"orderAmount"`
	CommissionRate   float64    `gorm:"type:decimal(3,2);comment:分成比例" json:"commissionRate"`
	CommissionAmount float64    `gorm:"type:decimal(10,2);comment:分成金额" json:"commissionAmount"`
	Status           string     `gorm:"type:varchar(20);default:PENDING;index;comment:分成状态" json:"status"`
	PaidAt           *time.Time `json:"paidAt"`
	Remark           string     `gorm:"type:text;comment:备注" json:"remark"`
}

// TableName 表名。
func (InviteCommission) TableName() string { return "invite_commissions" }

// InviteCreateInput 创建邀请码入参。
type InviteCreateInput struct {
	Type           *string    `json:"type"`
	CommissionRate *float64   `json:"commissionRate"`
	ExpiredAt      *time.Time `json:"expiredAt"`
	Remark         *string    `json:"remark"`
}

// InviteUseInput 使用邀请码入参。
type InviteUseInput struct {
	InviteCode string `json:"inviteCode" binding:"required"`
}

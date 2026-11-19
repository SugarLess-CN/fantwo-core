// Package model 钱包交易实体（对齐原 wallet-transaction.entity.ts）。
package model

// 钱包交易类型（对齐原 WalletTransactionType）。
const (
	WalletTxRecharge   = "RECHARGE"   // 充值
	WalletTxWithdraw   = "WITHDRAW"   // 提现
	WalletTxPayment    = "PAYMENT"    // 消费
	WalletTxIncome     = "INCOME"     // 收益
	WalletTxRefund     = "REFUND"     // 退款
	WalletTxCommission = "COMMISSION" // 抽成收益
	WalletTxInvite     = "INVITE"     // 邀请奖励
	WalletTxAdjust     = "ADJUST"     // 系统调整
)

// WalletTransaction 钱包流水表。
type WalletTransaction struct {
	BaseModel
	UserID        string  `gorm:"column:user_id;type:char(36);index:idx_wallet_user,priority:1;not null" json:"userId"`
	Type          string  `gorm:"column:type;type:varchar(32);index:idx_wallet_user,priority:2;not null" json:"type"`
	Amount        float64 `gorm:"column:amount;type:numeric(12,2);not null" json:"amount"`
	BalanceBefore float64 `gorm:"column:balance_before;type:numeric(12,2)" json:"balanceBefore"`
	BalanceAfter  float64 `gorm:"column:balance_after;type:numeric(12,2)" json:"balanceAfter"`
	OrderID       *string `gorm:"column:order_id;type:char(36)" json:"orderId"`
	PaymentID     *string `gorm:"column:payment_id;type:char(36)" json:"paymentId"`
	Description   *string `gorm:"column:description;type:varchar(255)" json:"description"`
	Remark        *string `gorm:"column:remark;type:varchar(255)" json:"remark"`
}

// TableName 表名。
func (WalletTransaction) TableName() string { return "wallet_transaction" }

// Package service 用户域对外提供的钱包扣款能力（WalletDebitor 实现）。
//
// 装饰品域（ops）购买装饰品时跨域扣钱包：接口由其定义（internal/service/
// decoration_service.go 的 WalletDebitor），实现归用户域——钱包表归用户域持有，
// 跨域不直接写 users.wallet。
package service

import (
	"errors"
	"fmt"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
)

// errInsufficientBalance 余额不足（跨域接口只回 error，语义与 errcode 400 一致）。
var errInsufficientBalance = errors.New("余额不足")

// UserWallet 钱包服务（扣款/入账 + 流水），实现 decoration.WalletDebitor。
type UserWallet struct {
	d *Deps
}

// NewUserWallet 构造。
func NewUserWallet(d *Deps) *UserWallet { return &UserWallet{d: d} }

// DeductBalance 扣减余额并写流水（同一事务）；余额不足返回 errInsufficientBalance。
// source 为业务来源标记（如 decoration_purchase），落 WalletTransaction.Remark。
func (w *UserWallet) DeductBalance(userID string, amount float64, source, description string) error {
	if w == nil || w.d == nil || w.d.DB == nil {
		return fmt.Errorf("钱包服务不可用")
	}
	if amount <= 0 {
		return fmt.Errorf("扣款金额必须大于 0")
	}
	return w.d.DB.Transaction(func(tx *gorm.DB) error {
		user, err := w.d.Users.FindPlainByID(tx, userID)
		if err != nil {
			return err
		}
		if user.Wallet < amount {
			return errInsufficientBalance
		}
		rows, err := w.d.Users.IncrWallet(tx, userID, -amount)
		if err != nil {
			return err
		}
		if rows == 0 {
			return errInsufficientBalance
		}
		desc := description
		if desc == "" {
			desc = "钱包消费"
		}
		src := source
		return tx.Create(&model.WalletTransaction{
			UserID: userID, Type: model.WalletTxPayment, Amount: -amount,
			BalanceBefore: user.Wallet, BalanceAfter: user.Wallet - amount,
			Description: &desc, Remark: &src,
		}).Error
	})
}

// CreditBalance 入账并写流水（收益/退款/佣金等跨域调用）。
func (w *UserWallet) CreditBalance(userID string, amount float64, txType, source, description string) error {
	if w == nil || w.d == nil || w.d.DB == nil {
		return fmt.Errorf("钱包服务不可用")
	}
	if amount <= 0 {
		return fmt.Errorf("入账金额必须大于 0")
	}
	if txType == "" {
		txType = model.WalletTxIncome
	}
	return w.d.DB.Transaction(func(tx *gorm.DB) error {
		user, err := w.d.Users.FindPlainByID(tx, userID)
		if err != nil {
			return err
		}
		if _, err := w.d.Users.IncrWallet(tx, userID, amount); err != nil {
			return err
		}
		desc := description
		if desc == "" {
			desc = "钱包入账"
		}
		src := source
		return tx.Create(&model.WalletTransaction{
			UserID: userID, Type: txType, Amount: amount,
			BalanceBefore: user.Wallet, BalanceAfter: user.Wallet + amount,
			Description: &desc, Remark: &src,
		}).Error
	})
}

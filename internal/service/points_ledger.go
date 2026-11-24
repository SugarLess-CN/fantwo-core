// 积分账本：余额变动的唯一入口。
//
// 为什么单独抽一层：余额变更是运营域唯一需要强一致的地方（并发扣减不能出现负数、
// 流水必须与余额同事务落库）。抽成接口后：
//   - 生产实现 GormLedger 用 PostgreSQL 事务 + SELECT ... FOR UPDATE 行锁；
//   - 测试实现（见 ops_points_ledger_test.go）用互斥锁做内存替身，
//     可以对同一套契约做并发压测，不需要 PG/Redis。
package service

import (
	"log"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LedgerMeta 一次积分变动的元信息。
type LedgerMeta struct {
	Source      string
	Description string
	RelatedType string
	RelatedID   string
	// ExpiredAt 仅发放时可能带（validDays>0）。
	ExpiredAt *time.Time
}

// PointsLedger 积分账本契约。
//
// Change 语义：
//   - delta > 0：发放，余额必须增加 delta；
//   - delta < 0：扣减，余额不足时返回业务错误且**不写流水**、余额不变；
//   - delta == 0：非法，返回参数错误；
//   - 返回变动后余额与本次落库的流水（含 balance 快照）。
//
// 契约要求（两种实现都必须满足，单测按此断言）：
//  1. 任何时刻余额不得为负；
//  2. 成功与否与「是否写流水」严格一致（失败不留流水）；
//  3. 并发调用下，最终余额 == 初始余额 + Σ成功 delta，且流水条数 == 成功次数。
type PointsLedger interface {
	Change(userID string, delta int, meta LedgerMeta) (balance int, record *model.PointsTransaction, err error)
}

// GormLedger 生产实现：事务 + 行锁 + 追加流水。
type GormLedger struct {
	DB *gorm.DB
}

// Change 见 PointsLedger 契约。
func (l GormLedger) Change(userID string, delta int, meta LedgerMeta) (int, *model.PointsTransaction, error) {
	if delta == 0 {
		return 0, nil, errcode.ErrBadRequest.WithKeyf(i18n.CommonInvalidParam, "amount must not be zero")
	}
	var (
		balance int
		record  *model.PointsTransaction
	)
	err := l.DB.Transaction(func(tx *gorm.DB) error {
		acc, err := ensurePointsAccount(tx, userID)
		if err != nil {
			return err
		}
		if delta < 0 {
			if !balanceSufficient(acc.Balance, -delta) {
				return errcode.ErrBadRequest.WithKey(i18n.PointsInsufficient)
			}
		}

		acc.Balance += delta
		if delta > 0 {
			acc.TotalEarned += int64(delta)
		} else {
			acc.TotalSpent += int64(-delta)
		}
		if err := tx.Save(acc).Error; err != nil {
			return err
		}

		row := &model.PointsTransaction{
			UserID:      userID,
			Amount:      delta,
			Balance:     acc.Balance,
			Type:        model.PointsTxEarn,
			Source:      meta.Source,
			RelatedType: meta.RelatedType,
			RelatedID:   meta.RelatedID,
			Description: meta.Description,
		}
		if delta < 0 {
			row.Type = model.PointsTxSpend
		}
		if delta > 0 {
			row.ExpiredAt = meta.ExpiredAt
		}
		if err := (repository.PointsTxRepo{}).Create(tx, row); err != nil {
			return err
		}
		balance = acc.Balance
		record = row
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	return balance, record, nil
}

// ensurePointsAccount 取（必要时创建）用户积分账户并在事务内加行锁。
// OnConflict DoNothing + user_id 唯一索引保证并发下只建一行。
func ensurePointsAccount(tx *gorm.DB, userID string) (*model.PointsAccount, error) {
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoNothing: true,
	}).Create(&model.PointsAccount{UserID: userID}).Error; err != nil {
		return nil, err
	}
	acc, err := (repository.PointsAccountRepo{}).FindByUserForUpdate(tx, userID)
	if err != nil {
		return nil, err
	}
	if acc == nil {
		log.Printf("[ERROR] 积分账户创建后仍查不到 user=%s（唯一索引冲突或事务可见性问题）", userID)
		return nil, errcode.ErrInternal.WithKey(i18n.PointsAccountCreateFail)
	}
	return acc, nil
}

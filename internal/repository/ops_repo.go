// Package repository 运营域数据访问层（积分/成就/表情包/装饰品/轮播/举报/邀请）。
//
// 约定（对齐 docs/04 §2）：repository 只做 GORM 读写封装，不做业务判断；
// 方法一律接受 *gorm.DB（可为事务 tx），返回 error 由上层 service 决策。
// 分页统一返回 (list, total, err)。
package repository

import (
	"errors"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PageQuery 通用分页参数（service 层已做默认值与上限收敛）。
type PageQuery struct {
	Page  int
	Limit int
}

// Offset 计算偏移。
func (q PageQuery) Offset() int { return (q.Page - 1) * q.Limit }

// ---------- 积分域 ----------

// PointsAccountRepo 积分账户读写。
type PointsAccountRepo struct{}

// FindByUser 按 user_id 查账户（不存在返回 nil, nil）。
func (PointsAccountRepo) FindByUser(db *gorm.DB, userID string) (*model.PointsAccount, error) {
	var acc model.PointsAccount
	err := db.First(&acc, "user_id = ?", userID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &acc, nil
}

// Create 建账户。
func (PointsAccountRepo) Create(db *gorm.DB, acc *model.PointsAccount) error {
	return db.Create(acc).Error
}

// FindByUserForUpdate 事务内 SELECT ... FOR UPDATE 行锁（消费/发放用）。
func (PointsAccountRepo) FindByUserForUpdate(db *gorm.DB, userID string) (*model.PointsAccount, error) {
	var acc model.PointsAccount
	err := db.Clauses(dbLockClause()).First(&acc, "user_id = ?", userID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &acc, nil
}

// AdjustBalance 原子调整余额（amount 可负），并把累计口径同步落账。
// 带条件更新：扣减时 balance >= -amount，不足返回 affected=0（由上层报错）。
func (PointsAccountRepo) AdjustBalance(db *gorm.DB, userID string, amount int) (int64, error) {
	res := db.Model(&model.PointsAccount{}).Where("user_id = ?", userID).
		Updates(map[string]interface{}{
			"balance":      gorm.Expr("balance + ?", amount),
			"total_earned": gorm.Expr("total_earned + ?", maxInt(amount, 0)),
			"total_spent":  gorm.Expr("total_spent + ?", maxInt(-amount, 0)),
		})
	return res.RowsAffected, res.Error
}

// PointsActivityRepo 积分活动读写。
type PointsActivityRepo struct{}

// FindByCode 按 code 查（不含软删）。
func (PointsActivityRepo) FindByCode(db *gorm.DB, code string) (*model.PointsActivity, error) {
	var a model.PointsActivity
	err := db.First(&a, "code = ?", code).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

// FindByID 按 ID 查。
func (PointsActivityRepo) FindByID(db *gorm.DB, id string) (*model.PointsActivity, error) {
	var a model.PointsActivity
	err := db.First(&a, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

// Create 建活动。
func (PointsActivityRepo) Create(db *gorm.DB, a *model.PointsActivity) error {
	return db.Create(a).Error
}

// Save 保存（更新）。
func (PointsActivityRepo) Save(db *gorm.DB, a *model.PointsActivity) error {
	return db.Save(a).Error
}

// Delete 软删。
func (PointsActivityRepo) Delete(db *gorm.DB, id string) error {
	return db.Delete(&model.PointsActivity{}, "id = ?", id).Error
}

// ListPublic 启用中的活动列表（type/keyword 过滤，sort ASC + createdAt DESC）。
func (PointsActivityRepo) ListPublic(db *gorm.DB, actType, keyword string) ([]model.PointsActivity, error) {
	q := db.Where("is_active = ?", true)
	if actType != "" {
		q = q.Where("type = ?", actType)
	}
	if keyword != "" {
		q = q.Where("name LIKE ?", "%"+keyword+"%")
	}
	var list []model.PointsActivity
	err := q.Order("sort ASC").Order("created_at DESC").Find(&list).Error
	return list, err
}

// PointsTaskRecordRepo 任务记录读写。
type PointsTaskRecordRepo struct{}

// FindByUserAndActivity 查用户某活动记录（不存在返回 nil, nil）。
func (PointsTaskRecordRepo) FindByUserAndActivity(db *gorm.DB, userID, activityID string) (*model.PointsTaskRecord, error) {
	var r model.PointsTaskRecord
	err := db.First(&r, "user_id = ? AND activity_id = ?", userID, activityID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// Save 保存记录。
func (PointsTaskRecordRepo) Save(db *gorm.DB, r *model.PointsTaskRecord) error {
	return db.Save(r).Error
}

// ListByUser 用户全部任务记录（created_at DESC）。
func (PointsTaskRecordRepo) ListByUser(db *gorm.DB, userID string) ([]model.PointsTaskRecord, error) {
	var list []model.PointsTaskRecord
	err := db.Where("user_id = ?", userID).Order("created_at DESC").Find(&list).Error
	return list, err
}

// PointsTxRepo 积分流水读写（只追加）。
type PointsTxRepo struct{}

// Create 写流水。
func (PointsTxRepo) Create(db *gorm.DB, tx *model.PointsTransaction) error {
	return db.Create(tx).Error
}

// List 分页查流水（userID 为空表示全量，管理端用）。
func (PointsTxRepo) List(db *gorm.DB, q model.PointsTxQuery) ([]model.PointsTransaction, int64, error) {
	dbq := db.Model(&model.PointsTransaction{})
	if q.UserID != "" {
		dbq = dbq.Where("user_id = ?", q.UserID)
	}
	if q.Type != "" {
		dbq = dbq.Where("type = ?", q.Type)
	}
	if q.Source != "" {
		dbq = dbq.Where("source = ?", q.Source)
	}
	var total int64
	if err := dbq.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.PointsTransaction
	page, limit := normPage(q.Page, q.Limit)
	err := dbq.Order("created_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return list, total, err
}

// CountTodayBySource 统计用户当日某来源流水数（每日限额用）。
func (PointsTxRepo) CountTodayBySource(db *gorm.DB, userID, source string, dayStart time.Time) (int64, error) {
	var n int64
	err := db.Model(&model.PointsTransaction{}).
		Where("user_id = ? AND source = ? AND created_at >= ?", userID, source, dayStart).
		Count(&n).Error
	return n, err
}

// CountBySource 统计用户某来源累计流水数（总限额用）。
func (PointsTxRepo) CountBySource(db *gorm.DB, userID, source string) (int64, error) {
	var n int64
	err := db.Model(&model.PointsTransaction{}).
		Where("user_id = ? AND source = ?", userID, source).
		Count(&n).Error
	return n, err
}

// ---------- 成就域 ----------

// AchievementRepo 成就定义读写。
type AchievementRepo struct{}

// FindByCode / FindByID 查询。
func (AchievementRepo) FindByCode(db *gorm.DB, code string) (*model.Achievement, error) {
	var a model.Achievement
	err := db.First(&a, "code = ?", code).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

// FindByID 按 ID 查。
func (AchievementRepo) FindByID(db *gorm.DB, id string) (*model.Achievement, error) {
	var a model.Achievement
	err := db.First(&a, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

// FindByCodes 批量按 code 查（启用的成就）。
func (AchievementRepo) FindByCodes(db *gorm.DB, codes []string) ([]model.Achievement, error) {
	var list []model.Achievement
	err := db.Where("code IN ? AND enabled = ?", codes, true).Find(&list).Error
	return list, err
}

// Create / Save / Delete。
func (AchievementRepo) Create(db *gorm.DB, a *model.Achievement) error { return db.Create(a).Error }
func (AchievementRepo) Save(db *gorm.DB, a *model.Achievement) error   { return db.Save(a).Error }
func (AchievementRepo) Delete(db *gorm.DB, id string) error {
	return db.Delete(&model.Achievement{}, "id = ?", id).Error
}

// List 启用中的成就列表（keyword 过滤；sort ASC / createdAt 排序）。
func (AchievementRepo) List(db *gorm.DB, keyword, sortBy, sortOrder string) ([]model.Achievement, error) {
	q := db.Where("enabled = ?", true)
	if keyword != "" {
		q = q.Where("name LIKE ?", "%"+keyword+"%")
	}
	if sortBy == "createdAt" && (sortOrder == "ASC" || sortOrder == "DESC") {
		q = q.Order("created_at " + sortOrder)
	} else {
		q = q.Order("sort ASC").Order("id ASC")
	}
	var list []model.Achievement
	err := q.Find(&list).Error
	return list, err
}

// CountEnabled 启用成就总数。
func (AchievementRepo) CountEnabled(db *gorm.DB) (int64, error) {
	var n int64
	err := db.Model(&model.Achievement{}).Where("enabled = ?", true).Count(&n).Error
	return n, err
}

// UserAchievementRepo 用户成就进度读写。
type UserAchievementRepo struct{}

// FindByUserAndAchievement 查进度（不存在返回 nil, nil）。
func (UserAchievementRepo) FindByUserAndAchievement(db *gorm.DB, userID, achievementID string) (*model.UserAchievement, error) {
	var ua model.UserAchievement
	err := db.First(&ua, "user_id = ? AND achievement_id = ?", userID, achievementID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &ua, nil
}

// FindByUserAndAchievements 批量查进度。
func (UserAchievementRepo) FindByUserAndAchievements(db *gorm.DB, userID string, achievementIDs []string) ([]model.UserAchievement, error) {
	var list []model.UserAchievement
	err := db.Where("user_id = ? AND achievement_id IN ?", userID, achievementIDs).Find(&list).Error
	return list, err
}

// Save 保存进度。
func (UserAchievementRepo) Save(db *gorm.DB, ua *model.UserAchievement) error {
	return db.Save(ua).Error
}

// Count 统计（completed/claimed 条件）。
func (UserAchievementRepo) Count(db *gorm.DB, userID string, completed, claimed bool) (int64, error) {
	var n int64
	q := db.Model(&model.UserAchievement{}).Where("user_id = ?", userID)
	if completed {
		q = q.Where("completed = ?", true)
	}
	if claimed {
		q = q.Where("claimed = ?", true)
	}
	err := q.Count(&n).Error
	return n, err
}

// ListClaimable 可领取列表（completed 且未 claimed）。
func (UserAchievementRepo) ListClaimable(db *gorm.DB, userID string) ([]model.UserAchievement, error) {
	var list []model.UserAchievement
	err := db.Where("user_id = ? AND completed = ? AND claimed = ?", userID, true, false).Find(&list).Error
	return list, err
}

// ---------- 表情包域 ----------

// EmojiRepo 表情读写。
type EmojiRepo struct{}

// FindByID 按 ID 查。
func (EmojiRepo) FindByID(db *gorm.DB, id string) (*model.Emoji, error) {
	var e model.Emoji
	err := db.First(&e, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &e, nil
}

// FindByCode 按表情代码查。
func (EmojiRepo) FindByCode(db *gorm.DB, code string) (*model.Emoji, error) {
	var e model.Emoji
	err := db.First(&e, "code = ?", code).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &e, nil
}

// Create / Save / Delete。
func (EmojiRepo) Create(db *gorm.DB, e *model.Emoji) error { return db.Create(e).Error }
func (EmojiRepo) Save(db *gorm.DB, e *model.Emoji) error   { return db.Save(e).Error }
func (EmojiRepo) Delete(db *gorm.DB, id string) error {
	return db.Delete(&model.Emoji{}, "id = ?", id).Error
}

// IncUseCount 使用次数自增。
func (EmojiRepo) IncUseCount(db *gorm.DB, id string) error {
	return db.Model(&model.Emoji{}).Where("id = ?", id).
		Update("use_count", gorm.Expr("use_count + 1")).Error
}

// List 表情列表（对齐原 findAll 过滤语义）。
func (EmojiRepo) List(db *gorm.DB, q model.EmojiQuery, onlyUserID string) ([]model.Emoji, int64, error) {
	dbq := db.Model(&model.Emoji{})
	if q.Type != "" {
		dbq = dbq.Where("type = ?", q.Type)
	}
	if q.Category != "" {
		dbq = dbq.Where("category = ?", q.Category)
	}
	if q.Keyword != "" {
		dbq = dbq.Where("name LIKE ? OR tags LIKE ?", "%"+q.Keyword+"%", "%"+q.Keyword+"%")
	}
	if q.IsPublic != nil {
		dbq = dbq.Where("is_public = ?", *q.IsPublic)
	}
	if q.Status != "" {
		dbq = dbq.Where("status = ?", q.Status)
	}
	if q.UserID != "" {
		dbq = dbq.Where("user_id = ?", q.UserID)
	}
	if onlyUserID != "" { // 未登录只看公开；登录则公开 ∪ 自己的
		dbq = dbq.Where("is_public = ? OR user_id = ?", true, onlyUserID)
	} else {
		dbq = dbq.Where("is_public = ?", true)
	}
	if q.OnlyFavorites { // favorites 过滤由 service 以子查询注入
		dbq = dbq.Where("id IN (?)", db.Model(&model.EmojiFavorite{}).Select("emoji_id").Where("user_id = ?", onlyUserID))
	}
	switch q.SortBy {
	case "use_count":
		order := "use_count"
		if q.SortOrder == "ASC" {
			order += " ASC"
		} else {
			order += " DESC"
		}
		dbq = dbq.Order(order)
	case "created_at":
		order := "created_at"
		if q.SortOrder == "ASC" {
			order += " ASC"
		} else {
			order += " DESC"
		}
		dbq = dbq.Order(order)
	default:
		dbq = dbq.Order("created_at DESC")
	}
	var total int64
	if err := dbq.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.Emoji
	page, limit := normPage(q.Page, q.Limit)
	err := dbq.Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return list, total, err
}

// ListPopular 热门表情（use_count DESC）。
func (EmojiRepo) ListPopular(db *gorm.DB, limit int) ([]model.Emoji, error) {
	var list []model.Emoji
	err := db.Where("is_public = ? AND status = ?", true, model.EmojiStatusActive).
		Order("use_count DESC").Limit(limit).Find(&list).Error
	return list, err
}

// ListRecent 最近添加（含用户自己的私有表情）。
func (EmojiRepo) ListRecent(db *gorm.DB, userID string, limit int) ([]model.Emoji, error) {
	q := db.Where("is_public = ? OR user_id = ?", true, userID)
	var list []model.Emoji
	err := q.Order("created_at DESC").Limit(limit).Find(&list).Error
	return list, err
}

// ListCategories 去重分类列表。
func (EmojiRepo) ListCategories(db *gorm.DB) ([]string, error) {
	var cats []string
	err := db.Model(&model.Emoji{}).Where("category IS NOT NULL AND category <> ''").
		Distinct().Order("category").Pluck("category", &cats).Error
	return cats, err
}

// EmojiFavoriteRepo 收藏读写。
type EmojiFavoriteRepo struct{}

// Exists 是否已收藏。
func (EmojiFavoriteRepo) Exists(db *gorm.DB, userID, emojiID string) (bool, error) {
	var n int64
	err := db.Model(&model.EmojiFavorite{}).Where("user_id = ? AND emoji_id = ?", userID, emojiID).Count(&n).Error
	return n > 0, err
}

// Create / Delete。
func (EmojiFavoriteRepo) Create(db *gorm.DB, f *model.EmojiFavorite) error { return db.Create(f).Error }
func (EmojiFavoriteRepo) Delete(db *gorm.DB, userID, emojiID string) error {
	return db.Delete(&model.EmojiFavorite{}, "user_id = ? AND emoji_id = ?", userID, emojiID).Error
}

// ListByUser 收藏列表（收藏时间倒序）。
func (EmojiFavoriteRepo) ListByUser(db *gorm.DB, userID string, page, limit int) ([]model.EmojiFavorite, int64, error) {
	q := db.Model(&model.EmojiFavorite{}).Where("user_id = ?", userID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.EmojiFavorite
	page, limit = normPage(page, limit)
	err := q.Order("created_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return list, total, err
}

// EmojiPackRepo 表情包读写。
type EmojiPackRepo struct{}

// FindByID 按 ID 查。
func (EmojiPackRepo) FindByID(db *gorm.DB, id string) (*model.EmojiPack, error) {
	var p model.EmojiPack
	err := db.First(&p, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// Create / Save / Delete。
func (EmojiPackRepo) Create(db *gorm.DB, p *model.EmojiPack) error { return db.Create(p).Error }
func (EmojiPackRepo) Save(db *gorm.DB, p *model.EmojiPack) error   { return db.Save(p).Error }
func (EmojiPackRepo) Delete(db *gorm.DB, id string) error {
	return db.Delete(&model.EmojiPack{}, "id = ?", id).Error
}

// ListVisible 公开 ∪ 自己的（keyword/status 过滤）。
func (EmojiPackRepo) ListVisible(db *gorm.DB, q model.EmojiPackQuery, userID string) ([]model.EmojiPack, int64, error) {
	dbq := db.Model(&model.EmojiPack{})
	if userID != "" {
		dbq = dbq.Where("is_public = ? OR user_id = ?", true, userID)
	} else {
		dbq = dbq.Where("is_public = ?", true)
	}
	if q.UserID != "" {
		dbq = dbq.Where("user_id = ?", q.UserID)
	}
	if q.Keyword != "" {
		dbq = dbq.Where("name LIKE ?", "%"+q.Keyword+"%")
	}
	if q.Status != "" {
		dbq = dbq.Where("status = ?", q.Status)
	}
	if q.IsPublic != nil {
		dbq = dbq.Where("is_public = ?", *q.IsPublic)
	}
	var total int64
	if err := dbq.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.EmojiPack
	page, limit := normPage(q.Page, q.Limit)
	err := dbq.Order("created_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return list, total, err
}

// EmojiPackItemRepo 包内条目读写。
type EmojiPackItemRepo struct{}

// Exists 是否已在包内。
func (EmojiPackItemRepo) Exists(db *gorm.DB, packID, emojiID string) (bool, error) {
	var n int64
	err := db.Model(&model.EmojiPackItem{}).Where("pack_id = ? AND emoji_id = ?", packID, emojiID).Count(&n).Error
	return n > 0, err
}

// Create / Delete。
func (EmojiPackItemRepo) Create(db *gorm.DB, i *model.EmojiPackItem) error { return db.Create(i).Error }
func (EmojiPackItemRepo) Delete(db *gorm.DB, packID, emojiID string) error {
	return db.Delete(&model.EmojiPackItem{}, "pack_id = ? AND emoji_id = ?", packID, emojiID).Error
}

// FindItem 查单条目。
func (EmojiPackItemRepo) FindItem(db *gorm.DB, packID, emojiID string) (*model.EmojiPackItem, error) {
	var i model.EmojiPackItem
	err := db.First(&i, "pack_id = ? AND emoji_id = ?", packID, emojiID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &i, nil
}

// ListByPack 包内条目（sort ASC）。
func (EmojiPackItemRepo) ListByPack(db *gorm.DB, packID string) ([]model.EmojiPackItem, error) {
	var list []model.EmojiPackItem
	err := db.Where("pack_id = ?", packID).Order("sort ASC").Order("created_at ASC").Find(&list).Error
	return list, err
}

// NextSort 包内下一排序号。
func (EmojiPackItemRepo) NextSort(db *gorm.DB, packID string) (int, error) {
	var maxSort int
	err := db.Model(&model.EmojiPackItem{}).Where("pack_id = ?", packID).
		Select("COALESCE(MAX(sort), -1)").Scan(&maxSort).Error
	return maxSort + 1, err
}

// ---------- 装饰品域 ----------

// DecorationRepo 装饰品读写。
type DecorationRepo struct{}

// FindByID / FindByAchievement。
func (DecorationRepo) FindByID(db *gorm.DB, id string) (*model.Decoration, error) {
	var d model.Decoration
	err := db.First(&d, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

// FindByAchievement 按来源成就查勋章。
func (DecorationRepo) FindByAchievement(db *gorm.DB, achievementID string) (*model.Decoration, error) {
	var d model.Decoration
	err := db.First(&d, "achievement_id = ? AND type = ?", achievementID, model.DecorationTypeAchievementBadge).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

// Create / Save / Delete。
func (DecorationRepo) Create(db *gorm.DB, d *model.Decoration) error { return db.Create(d).Error }
func (DecorationRepo) Save(db *gorm.DB, d *model.Decoration) error   { return db.Save(d).Error }
func (DecorationRepo) Delete(db *gorm.DB, id string) error {
	return db.Delete(&model.Decoration{}, "id = ?", id).Error
}

// List 装饰品列表（type/status/keyword 过滤）。
func (DecorationRepo) List(db *gorm.DB, q model.DecorationQuery) ([]model.Decoration, int64, error) {
	dbq := db.Model(&model.Decoration{})
	if q.Type != "" {
		dbq = dbq.Where("type = ?", q.Type)
	}
	if q.Status != "" {
		dbq = dbq.Where("status = ?", q.Status)
	}
	if q.Keyword != "" {
		dbq = dbq.Where("name LIKE ?", "%"+q.Keyword+"%")
	}
	var total int64
	if err := dbq.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.Decoration
	page, limit := normPage(q.Page, q.Limit)
	err := dbq.Order("sort DESC").Order("created_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return list, total, err
}

// CleanExpired 软删全部已过期（非永久且 expires_at < now）的用户装饰品。
func (DecorationRepo) CleanExpired(db *gorm.DB) (int64, error) {
	res := db.Where("is_permanent = ? AND expires_at IS NOT NULL AND expires_at < ?", false, time.Now()).
		Delete(&model.UserDecoration{})
	return res.RowsAffected, res.Error
}

// UserDecorationRepo 用户装饰品读写。
type UserDecorationRepo struct{}

// FindByUserAndDecoration 查持有记录（不存在返回 nil, nil）。
func (UserDecorationRepo) FindByUserAndDecoration(db *gorm.DB, userID, decorationID string) (*model.UserDecoration, error) {
	var ud model.UserDecoration
	err := db.First(&ud, "user_id = ? AND decoration_id = ?", userID, decorationID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &ud, nil
}

// FindUsingByType 查用户某类型正在使用的装饰品。
func (UserDecorationRepo) FindUsingByType(db *gorm.DB, userID, decoType string) (*model.UserDecoration, error) {
	var ud model.UserDecoration
	err := db.Joins("JOIN decorations ON decorations.id = user_decorations.decoration_id").
		First(&ud, "user_decorations.user_id = ? AND user_decorations.is_using = ? AND decorations.type = ?",
			userID, true, decoType).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &ud, nil
}

// UserUsingDecoration 批量查询的行结果（user_decorations JOIN decorations）。
//
// 单独一个结构而不是复用 model：JOIN 出来的行是「佩戴关系 + 装饰品本体」的
// 扁平静态视图，给 model.UserDecoration 加关联字段会污染实体（且装配时还要再判 nil）。
type UserUsingDecoration struct {
	UserID       string `gorm:"column:user_id"`
	DecorationID string `gorm:"column:decoration_id"`
	Type         string `gorm:"column:type"`
	Name         string `gorm:"column:name"`
	ImageURL     string `gorm:"column:image_url"`
	Rarity       string `gorm:"column:rarity"`
	BubbleColor  string `gorm:"column:bubble_color"`
}

// FindUsingByUsers 批量查多个用户「佩戴中」的装饰品（**一次 SQL，避免 N+1**）。
//
// 用于列表接口：N 个用户 × 2 类装饰逐个查 = 2N 次往返，一页 20 人就 40 次。
// 这里 IN 一次捞完，装配在 Go 侧做 map。
func (UserDecorationRepo) FindUsingByUsers(db *gorm.DB, userIDs []string) ([]UserUsingDecoration, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	var rows []UserUsingDecoration
	err := db.Table("user_decorations").
		Select("user_decorations.user_id, user_decorations.decoration_id, "+
			"decorations.type, decorations.name, decorations.image_url, "+
			"decorations.rarity, decorations.bubble_color").
		Joins("JOIN decorations ON decorations.id = user_decorations.decoration_id").
		Where("user_decorations.user_id IN ? AND user_decorations.is_using = ?", userIDs, true).
		// ⚠️ 软删判 `= 0` 不是 `IS NULL`。
		//
		// 本项目的 DeletedAt 是 `soft_delete.DeletedAt`（**int 型**，0=未删，非 0=删除时刻），
		// 库里存的是 0 而不是 NULL。写成 `IS NULL` 会永远匹配不上 → 查询恒返回空，
		// 而库里肉眼可见数据齐全，极难发现（用 gorm 日志看 `rows:0` 才能定位）。
		//
		// 同一个坑在造测试数据时也踩过：插入时 `deleted_at` 留 NULL 同样查不出来。
		Where("user_decorations.deleted_at = 0 AND decorations.deleted_at = 0").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// UnsetType 同类型全部取消使用（佩戴切换用）。
func (UserDecorationRepo) UnsetType(db *gorm.DB, userID, decoType string) error {
	return db.Exec(
		"UPDATE user_decorations SET is_using = false WHERE user_id = ? AND is_using = true "+
			"AND decoration_id IN (SELECT id FROM decorations WHERE type = ?)", userID, decoType).Error
}

// Create / Save。
func (UserDecorationRepo) Create(db *gorm.DB, ud *model.UserDecoration) error {
	return db.Create(ud).Error
}
func (UserDecorationRepo) Save(db *gorm.DB, ud *model.UserDecoration) error { return db.Save(ud).Error }

// ListByUser 用户装饰品列表（type 过滤，可带装饰品详情）。
func (UserDecorationRepo) ListByUser(db *gorm.DB, userID, decoType string, page, limit int) ([]model.UserDecoration, int64, error) {
	q := db.Model(&model.UserDecoration{}).Where("user_id = ?", userID)
	if decoType != "" {
		q = q.Joins("JOIN decorations ON decorations.id = user_decorations.decoration_id").
			Where("decorations.type = ?", decoType)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.UserDecoration
	page, limit = normPage(page, limit)
	err := q.Order("created_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return list, total, err
}

// DecorationActivityRepo 活动读写。
type DecorationActivityRepo struct{}

// FindByID 按 ID 查。
func (DecorationActivityRepo) FindByID(db *gorm.DB, id string) (*model.DecorationActivity, error) {
	var a model.DecorationActivity
	err := db.First(&a, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

// Create / Save / Delete。
func (DecorationActivityRepo) Create(db *gorm.DB, a *model.DecorationActivity) error {
	return db.Create(a).Error
}
func (DecorationActivityRepo) Save(db *gorm.DB, a *model.DecorationActivity) error {
	return db.Save(a).Error
}
func (DecorationActivityRepo) Delete(db *gorm.DB, id string) error {
	return db.Delete(&model.DecorationActivity{}, "id = ?", id).Error
}

// List 活动列表（status/type 过滤）。
func (DecorationActivityRepo) List(db *gorm.DB, status, actType string, page, limit int) ([]model.DecorationActivity, int64, error) {
	q := db.Model(&model.DecorationActivity{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if actType != "" {
		q = q.Where("type = ?", actType)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.DecorationActivity
	page, limit = normPage(page, limit)
	err := q.Order("created_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return list, total, err
}

// IncCounters 参与人数/完成人数自增。
func (DecorationActivityRepo) IncCounters(db *gorm.DB, id string, participants, completed int64) error {
	updates := map[string]interface{}{}
	if participants > 0 {
		updates["participant_count"] = gorm.Expr("participant_count + ?", participants)
	}
	if completed > 0 {
		updates["completed_count"] = gorm.Expr("completed_count + ?", completed)
	}
	if len(updates) == 0 {
		return nil
	}
	return db.Model(&model.DecorationActivity{}).Where("id = ?", id).Updates(updates).Error
}

// UserActivityProgressRepo 用户活动进度读写。
type UserActivityProgressRepo struct{}

// FindByUserAndActivity 查进度（不存在返回 nil, nil）。
func (UserActivityProgressRepo) FindByUserAndActivity(db *gorm.DB, userID, activityID string) (*model.UserActivityProgress, error) {
	var p model.UserActivityProgress
	err := db.First(&p, "user_id = ? AND activity_id = ?", userID, activityID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// ListByUser 我的全部活动进度。
func (UserActivityProgressRepo) ListByUser(db *gorm.DB, userID string) ([]model.UserActivityProgress, error) {
	var list []model.UserActivityProgress
	err := db.Where("user_id = ?", userID).Order("created_at DESC").Find(&list).Error
	return list, err
}

// Save 保存进度。
func (UserActivityProgressRepo) Save(db *gorm.DB, p *model.UserActivityProgress) error {
	return db.Save(p).Error
}

// ---------- 轮播域 ----------

// BannerRepo 轮播读写。
type BannerRepo struct{}

// FindByID 按 ID 查。
func (BannerRepo) FindByID(db *gorm.DB, id string) (*model.Banner, error) {
	var b model.Banner
	err := db.First(&b, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &b, nil
}

// Create / Save / Delete。
func (BannerRepo) Create(db *gorm.DB, b *model.Banner) error { return db.Create(b).Error }
func (BannerRepo) Save(db *gorm.DB, b *model.Banner) error   { return db.Save(b).Error }
func (BannerRepo) Delete(db *gorm.DB, id string) error {
	return db.Delete(&model.Banner{}, "id = ?", id).Error
}

// List 分页（status 过滤；sort_order ASC）。
func (BannerRepo) List(db *gorm.DB, q model.BannerQuery) ([]model.Banner, int64, error) {
	qq := db.Model(&model.Banner{})
	if q.Status != "" {
		qq = qq.Where("status = ?", q.Status)
	}
	var total int64
	if err := qq.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := "sort_order ASC"
	if q.SortOrder == "DESC" {
		order = "sort_order DESC"
	}
	var list []model.Banner
	page, limit := normPage(q.Page, q.Limit)
	err := qq.Order(order).Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return list, total, err
}

// ListActive 启用中的轮播（公开端点）。
func (BannerRepo) ListActive(db *gorm.DB) ([]model.Banner, error) {
	var list []model.Banner
	err := db.Where("status = ?", model.BannerStatusActive).Order("sort_order ASC").Find(&list).Error
	return list, err
}

// ---------- 举报域 ----------

// ReportRepo 举报读写。
type ReportRepo struct{}

// FindByID 按 ID 查。
func (ReportRepo) FindByID(db *gorm.DB, id string) (*model.Report, error) {
	var r model.Report
	err := db.First(&r, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// FindDuplicatePending 查同举报人同目标同类型的待处理举报（去重用）。
func (ReportRepo) FindDuplicatePending(db *gorm.DB, reporterID string, in model.ReportCreateInput) (*model.Report, error) {
	q := db.Where("reporter_id = ? AND type = ? AND status = ?", reporterID, in.Type, model.ReportStatusPending)
	switch in.Type {
	case model.ReportTypeUser:
		q = q.Where("reported_user_id = ?", in.ReportedUserID)
	case model.ReportTypeArticle:
		q = q.Where("reported_article_id = ?", in.ReportedArticleID)
	case model.ReportTypeComment:
		q = q.Where("reported_comment_id = ?", in.ReportedCommentID)
	}
	var r model.Report
	err := q.First(&r).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// Create / Save / Delete。
func (ReportRepo) Create(db *gorm.DB, r *model.Report) error { return db.Create(r).Error }
func (ReportRepo) Save(db *gorm.DB, r *model.Report) error   { return db.Save(r).Error }
func (ReportRepo) Delete(db *gorm.DB, id string) error {
	return db.Delete(&model.Report{}, "id = ?", id).Error
}

// List 举报列表（type/status/category/reporter/keyword 过滤）。
func (ReportRepo) List(db *gorm.DB, q model.ReportQuery) ([]model.Report, int64, error) {
	qq := db.Model(&model.Report{})
	if q.Type != "" {
		qq = qq.Where("type = ?", q.Type)
	}
	if q.Status != "" {
		qq = qq.Where("status = ?", q.Status)
	}
	if q.Category != "" {
		qq = qq.Where("category = ?", q.Category)
	}
	if q.ReporterID != "" {
		qq = qq.Where("reporter_id = ?", q.ReporterID)
	}
	if q.Keyword != "" {
		qq = qq.Where("reason LIKE ?", "%"+q.Keyword+"%")
	}
	if q.SortBy == "createdAt" && (q.SortOrder == "ASC" || q.SortOrder == "DESC") {
		qq = qq.Order("created_at " + q.SortOrder)
	} else {
		qq = qq.Order("created_at DESC")
	}
	var total int64
	if err := qq.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.Report
	page, limit := normPage(q.Page, q.Limit)
	err := qq.Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return list, total, err
}

// CountByStatus 按状态计数。
func (ReportRepo) CountByStatus(db *gorm.DB, status string) (int64, error) {
	var n int64
	err := db.Model(&model.Report{}).Where("status = ?", status).Count(&n).Error
	return n, err
}

// CountTotal 总数。
func (ReportRepo) CountTotal(db *gorm.DB) (int64, error) {
	var n int64
	err := db.Model(&model.Report{}).Count(&n).Error
	return n, err
}

// CountGroupBy 按列分组计数（type / category）。
func (ReportRepo) CountGroupBy(db *gorm.DB, column string) ([]map[string]interface{}, error) {
	var out []map[string]interface{}
	err := db.Model(&model.Report{}).
		Select(column + " AS key, COUNT(*) AS count").
		Group(column).Find(&out).Error
	return out, err
}

// ---------- 邀请域 ----------

// InviteRepo 邀请码读写。
type InviteRepo struct{}

// FindByID / FindByCode。
func (InviteRepo) FindByID(db *gorm.DB, id string) (*model.Invite, error) {
	var i model.Invite
	err := db.First(&i, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &i, nil
}

// FindByCode 按邀请码查。
func (InviteRepo) FindByCode(db *gorm.DB, code string) (*model.Invite, error) {
	var i model.Invite
	err := db.First(&i, "invite_code = ?", code).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &i, nil
}

// Create / Save。
func (InviteRepo) Create(db *gorm.DB, i *model.Invite) error { return db.Create(i).Error }
func (InviteRepo) Save(db *gorm.DB, i *model.Invite) error   { return db.Save(i).Error }

// ListByInviter 我的邀请列表（keyword/status 过滤）。
func (InviteRepo) ListByInviter(db *gorm.DB, inviterID, keyword, status string, page, limit int) ([]model.Invite, int64, error) {
	q := db.Model(&model.Invite{}).Where("inviter_id = ?", inviterID)
	if keyword != "" {
		q = q.Where("invite_code LIKE ?", "%"+keyword+"%")
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.Invite
	page, limit = normPage(page, limit)
	err := q.Order("created_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return list, total, err
}

// CountByStatus 按状态计数（邀请统计）。
func (InviteRepo) CountByStatus(db *gorm.DB, inviterID, status string) (int64, error) {
	var n int64
	err := db.Model(&model.Invite{}).Where("inviter_id = ? AND status = ?", inviterID, status).Count(&n).Error
	return n, err
}

// CountByInviter 邀请总数。
func (InviteRepo) CountByInviter(db *gorm.DB, inviterID string) (int64, error) {
	var n int64
	err := db.Model(&model.Invite{}).Where("inviter_id = ?", inviterID).Count(&n).Error
	return n, err
}

// InviteCommissionRepo 分成记录读写。
type InviteCommissionRepo struct{}

// Create / Save。
func (InviteCommissionRepo) Create(db *gorm.DB, c *model.InviteCommission) error {
	return db.Create(c).Error
}
func (InviteCommissionRepo) Save(db *gorm.DB, c *model.InviteCommission) error {
	return db.Save(c).Error
}

// ListByInviter 分成记录（inviter_id 或 invitee_id 视角）。
func (InviteCommissionRepo) ListByInviter(db *gorm.DB, userID string, page, limit int) ([]model.InviteCommission, int64, error) {
	q := db.Model(&model.InviteCommission{}).Where("inviter_id = ? OR invitee_id = ?", userID, userID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.InviteCommission
	page, limit = normPage(page, limit)
	err := q.Order("created_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return list, total, err
}

// SumEarnings 分成累计（inviter 视角，含状态过滤）。
func (InviteCommissionRepo) SumEarnings(db *gorm.DB, inviterID, status string) (float64, error) {
	var total float64
	q := db.Model(&model.InviteCommission{}).Where("inviter_id = ?", inviterID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	err := q.Select("COALESCE(SUM(commission_amount), 0)").Scan(&total).Error
	return total, err
}

// CountByStatus 分成计数（按状态）。
func (InviteCommissionRepo) CountByStatus(db *gorm.DB, inviterID, status string) (int64, error) {
	var n int64
	err := db.Model(&model.InviteCommission{}).Where("inviter_id = ? AND status = ?", inviterID, status).Count(&n).Error
	return n, err
}

// ---------- 内部工具 ----------

// normPage 分页参数收敛（默认 1/10，上限 100）。
func normPage(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
}

// maxInt 取大。
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// dbLockClause 行锁子句（SELECT ... FOR UPDATE）。
// 用 gorm clause 包生成，避免各仓储方法各写一份 SQL 片段。
func dbLockClause() clause.Locking {
	return clause.Locking{Strength: "UPDATE"}
}

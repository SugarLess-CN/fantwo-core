// QQ 机器人数据访问层（T-40）。
//
// # 为什么单独抽一个接口而不是直接写 s.d.DB
//
// 与 oauth_service.go 的 oauthDataAccess 同口径（T-37d 立的先例）：go.mod 里没有
// sqlite / sqlmock 驱动（go.mod 归 lead 独占，成员不得 go get），service 层测试
// 无法起内存库。于是把「本服务实际触达数据库的那一小撮查询」抽成接口：
//
//	生产   qqbotGormAccess 直接吃 *gorm.DB，逐行为等价（条件、软删语义、DoNothing）
//	测试   注入内存桩，覆盖池子状态机（尤其是 DEFERRED 不丢消息这条硬需求）
//
// 方法面 = qqbot_*.go 里实际用到的查询，不增不减（grep 可核对）。
package service

import (
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// qqbotAppFilter 申请单列表过滤条件。
type qqbotAppFilter struct {
	// Status 为空表示全部。
	Status string
	// Keyword 命中 open_id / nickname / content / invite_code（ILIKE，大小写不敏感）。
	Keyword string
	Page    int
	Limit   int
}

// qqbotOutboxFilter 发件池列表过滤条件。
type qqbotOutboxFilter struct {
	Status string
	Kind   string
	OpenID string
	Page   int
	Limit  int
}

// qqbotConversationRow 会话聚合行（group by open_id）。
type qqbotConversationRow struct {
	OpenID        string
	MessageCount  int64
	LastMessageAt time.Time
}

// qqbotDataAccess QqbotService 对持久层的最小依赖面。
type qqbotDataAccess interface {
	// ---- 会话消息 ----

	// MessageExists 平台消息 ID 是否已落库（webhook 幂等的第一道判定）。
	MessageExists(qqbotMsgID string) (bool, error)
	// CreateMessage 落一条消息；返回是否真的插入（命中唯一索引时 false）。
	//
	// 用 INSERT ... ON CONFLICT DO NOTHING：官方明确「相同 msg_id 可能多次推送」，
	// 应用层「先查后插」在两条 webhook 并发到达时仍会插两条，只有数据库约束可靠。
	CreateMessage(m *model.QqbotMessage) (bool, error)
	// CountMessages 某 openid 的消息总数。
	CountMessages(openID string) (int64, error)
	// ListMessages 某 openid 的消息流水（时间倒序，offset/limit 分页）。
	ListMessages(openID string, offset, limit int) ([]model.QqbotMessage, error)
	// LastMessage 某 openid 的最后一条消息（无则返回 nil）。
	LastMessage(openID string) (*model.QqbotMessage, error)
	// LastMessageByDirection 某方向（INBOUND / OUTBOUND）的最后一条消息。
	LastMessageByDirection(openID, direction string) (*model.QqbotMessage, error)
	// CountInboundAfter 某时刻之后的入站消息条数（会话列表「未读数」）。
	//
	// 未读口径：最后一条出站消息之后的入站条数；after 为零值时数全部入站。
	CountInboundAfter(openID string, after time.Time) (int64, error)

	// ---- 申请单 ----

	// CreateApplication 建申请单；返回是否真的插入（重复消息命中唯一索引时 false）。
	CreateApplication(a *model.QqbotInviteApplication) (bool, error)
	// FindApplicationByID 按主键取申请单；不存在返回 (nil, nil)。
	FindApplicationByID(id string) (*model.QqbotInviteApplication, error)
	// LatestApplicationByOpenID 该 openid 最近一条申请（取昵称用）。
	LatestApplicationByOpenID(openID string) (*model.QqbotInviteApplication, error)
	// ListApplications 分页查申请单。
	ListApplications(f qqbotAppFilter) ([]model.QqbotInviteApplication, int64, error)
	// UpdateApplication 局部更新（审批写 status / invite_code / reviewer 等）。
	UpdateApplication(id string, fields map[string]interface{}) error
	// CountApplications 按状态计数（status 为空表示全部）。
	CountApplications(status string) (int64, error)

	// ---- 发件池 ----

	// CreateOutbox 建池任务。
	CreateOutbox(o *model.QqbotOutbox) error
	// FindOutbox 按主键取池任务；不存在返回 (nil, nil)。
	FindOutbox(id string) (*model.QqbotOutbox, error)
	// ListOutbox 分页查池任务（后台排查用，含 DEFERRED / FAILED）。
	ListOutbox(f qqbotOutboxFilter) ([]model.QqbotOutbox, int64, error)
	// ListOutboxByStatuses 某 openid 下指定状态的任务（激活窗口时捞 DEFERRED 用）。
	ListOutboxByStatuses(openID string, statuses []string, limit int) ([]model.QqbotOutbox, error)
	// ListOutboxDue 定时扫描用：PENDING 且 (next_retry_at 为空或已到期)。
	ListOutboxDue(now time.Time, limit int) ([]model.QqbotOutbox, error)
	// UpdateOutbox 局部更新。
	UpdateOutbox(id string, fields map[string]interface{}) error
	// ClaimOutbox 状态机原子抢占：仅当当前状态在 from 里才改成 SENDING。
	//
	// 返回 false 表示「已被别的投递者抢先」（并发 webhook + 定时扫描同时到），
	// 此时调用方必须放弃本次投递，否则同一 msg_seq 会发两遍。
	ClaimOutbox(id string, from []string) (bool, error)
	// ReclaimStaleSending 把卡在 SENDING 超过 before 的任务拉回 PENDING（进程崩溃兜底）。
	ReclaimStaleSending(before time.Time) (int64, error)
	// CountOutbox 按状态计数。
	CountOutbox(status string) (int64, error)
	// CountOutboxByOpenID 某 openid 下指定状态的条数（会话列表「待发数」）。
	CountOutboxByOpenID(openID string, statuses []string) (int64, error)
	// LatestOutboxWithError 最近一条有失败记录的池任务（/status 展示最近失败原因）。
	LatestOutboxWithError() (*model.QqbotOutbox, error)

	// ---- 会话聚合 ----

	// ListConversationOpenIDs 按 openid 聚合的分页列表（消息数 + 最后消息时间）。
	ListConversationOpenIDs(offset, limit int) ([]qqbotConversationRow, int64, error)

	// ---- 邀请码（复用 invites 表，不另建）----

	// InviteCodeExists 邀请码是否已存在（碰撞重试用）。
	InviteCodeExists(code string) (bool, error)
	// CreateInvite 建邀请码记录。
	CreateInvite(inv *model.Invite) error
}

// qqbotGormAccess 生产实现：全部走 s.d.DB。
type qqbotGormAccess struct{ s *QqbotService }

func (g qqbotGormAccess) db() *gorm.DB { return g.s.d.DB }

// ---------- 会话消息 ----------

func (g qqbotGormAccess) MessageExists(qqbotMsgID string) (bool, error) {
	if qqbotMsgID == "" {
		return false, nil
	}
	var n int64
	err := g.db().Model(&model.QqbotMessage{}).Where("qqbot_msg_id = ?", qqbotMsgID).Limit(1).Count(&n).Error
	return n > 0, err
}

func (g qqbotGormAccess) CreateMessage(m *model.QqbotMessage) (bool, error) {
	if m == nil {
		return false, nil
	}
	res := g.db().Clauses(clause.OnConflict{DoNothing: true}).Create(m)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (g qqbotGormAccess) CountMessages(openID string) (int64, error) {
	var n int64
	err := g.db().Model(&model.QqbotMessage{}).Where("open_id = ?", openID).Count(&n).Error
	return n, err
}

func (g qqbotGormAccess) ListMessages(openID string, offset, limit int) ([]model.QqbotMessage, error) {
	var list []model.QqbotMessage
	err := g.db().Where("open_id = ?", openID).
		Order("created_at DESC").Offset(offset).Limit(limit).Find(&list).Error
	return list, err
}

func (g qqbotGormAccess) LastMessage(openID string) (*model.QqbotMessage, error) {
	return g.lastMessage(openID, "")
}

func (g qqbotGormAccess) LastMessageByDirection(openID, direction string) (*model.QqbotMessage, error) {
	return g.lastMessage(openID, direction)
}

func (g qqbotGormAccess) lastMessage(openID, direction string) (*model.QqbotMessage, error) {
	q := g.db().Where("open_id = ?", openID)
	if direction != "" {
		q = q.Where("direction = ?", direction)
	}
	var m model.QqbotMessage
	err := q.Order("created_at DESC").First(&m).Error
	if err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

func (g qqbotGormAccess) CountInboundAfter(openID string, after time.Time) (int64, error) {
	q := g.db().Model(&model.QqbotMessage{}).
		Where("open_id = ? AND direction = ?", openID, model.QqbotMsgInbound)
	if !after.IsZero() {
		q = q.Where("created_at > ?", after)
	}
	var n int64
	err := q.Count(&n).Error
	return n, err
}

// ---------- 申请单 ----------

func (g qqbotGormAccess) CreateApplication(a *model.QqbotInviteApplication) (bool, error) {
	if a == nil {
		return false, nil
	}
	res := g.db().Clauses(clause.OnConflict{DoNothing: true}).Create(a)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (g qqbotGormAccess) FindApplicationByID(id string) (*model.QqbotInviteApplication, error) {
	var a model.QqbotInviteApplication
	err := g.db().Where("id = ?", id).First(&a).Error
	if err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

func (g qqbotGormAccess) LatestApplicationByOpenID(openID string) (*model.QqbotInviteApplication, error) {
	var a model.QqbotInviteApplication
	err := g.db().Where("open_id = ?", openID).Order("created_at DESC").First(&a).Error
	if err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

func (g qqbotGormAccess) ListApplications(f qqbotAppFilter) ([]model.QqbotInviteApplication, int64, error) {
	q := g.db().Model(&model.QqbotInviteApplication{})
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Keyword != "" {
		kw := "%" + f.Keyword + "%"
		q = q.Where("open_id ILIKE ? OR nickname ILIKE ? OR content ILIKE ? OR invite_code ILIKE ?",
			kw, kw, kw, kw)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.QqbotInviteApplication
	err := q.Order("created_at DESC").Offset((f.Page - 1) * f.Limit).Limit(f.Limit).Find(&list).Error
	return list, total, err
}

func (g qqbotGormAccess) UpdateApplication(id string, fields map[string]interface{}) error {
	if len(fields) == 0 {
		return nil
	}
	return g.db().Model(&model.QqbotInviteApplication{}).Where("id = ?", id).Updates(fields).Error
}

func (g qqbotGormAccess) CountApplications(status string) (int64, error) {
	q := g.db().Model(&model.QqbotInviteApplication{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var n int64
	err := q.Count(&n).Error
	return n, err
}

// ---------- 发件池 ----------

func (g qqbotGormAccess) CreateOutbox(o *model.QqbotOutbox) error {
	if o == nil {
		return nil
	}
	return g.db().Create(o).Error
}

func (g qqbotGormAccess) FindOutbox(id string) (*model.QqbotOutbox, error) {
	var o model.QqbotOutbox
	err := g.db().Where("id = ?", id).First(&o).Error
	if err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &o, nil
}

func (g qqbotGormAccess) ListOutbox(f qqbotOutboxFilter) ([]model.QqbotOutbox, int64, error) {
	q := g.db().Model(&model.QqbotOutbox{})
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Kind != "" {
		q = q.Where("kind = ?", f.Kind)
	}
	if f.OpenID != "" {
		q = q.Where("open_id = ?", f.OpenID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.QqbotOutbox
	// 排序：先看「还没出池的」再看时间 —— 后台关心的是积压，不是历史。
	// 用 status 优先级表达（PENDING/DEFERRED/SENDING 在前），无需额外列。
	err := q.Order("CASE status WHEN 'PENDING' THEN 0 WHEN 'DEFERRED' THEN 1 WHEN 'SENDING' THEN 2 WHEN 'FAILED' THEN 3 ELSE 4 END").
		Order("created_at DESC").
		Offset((f.Page - 1) * f.Limit).Limit(f.Limit).Find(&list).Error
	return list, total, err
}

func (g qqbotGormAccess) ListOutboxByStatuses(openID string, statuses []string, limit int) ([]model.QqbotOutbox, error) {
	if len(statuses) == 0 || openID == "" {
		return nil, nil
	}
	var list []model.QqbotOutbox
	err := g.db().Where("open_id = ? AND status IN ?", openID, statuses).
		Order("created_at ASC").Limit(limit).Find(&list).Error
	return list, err
}

func (g qqbotGormAccess) ListOutboxDue(now time.Time, limit int) ([]model.QqbotOutbox, error) {
	var list []model.QqbotOutbox
	err := g.db().
		Where("status = ?", model.QqbotOutboxPending).
		Where("next_retry_at IS NULL OR next_retry_at <= ?", now).
		Order("created_at ASC").Limit(limit).Find(&list).Error
	return list, err
}

func (g qqbotGormAccess) UpdateOutbox(id string, fields map[string]interface{}) error {
	if len(fields) == 0 {
		return nil
	}
	return g.db().Model(&model.QqbotOutbox{}).Where("id = ?", id).Updates(fields).Error
}

// ClaimOutbox 原子状态迁移：WHERE status IN (from) 是并发安全的唯一保证。
//
// 用 Updates 的 RowsAffected 判定是否抢到 —— 两个投递者同时在跑时，
// 只有一个能让 status 变 SENDING，另一个拿到 0 行必须退出。
func (g qqbotGormAccess) ClaimOutbox(id string, from []string) (bool, error) {
	if len(from) == 0 {
		return false, nil
	}
	res := g.db().Model(&model.QqbotOutbox{}).
		Where("id = ? AND status IN ?", id, from).
		Update("status", model.QqbotOutboxSending)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (g qqbotGormAccess) ReclaimStaleSending(before time.Time) (int64, error) {
	res := g.db().Model(&model.QqbotOutbox{}).
		Where("status = ? AND updated_at < ?", model.QqbotOutboxSending, before).
		Update("status", model.QqbotOutboxPending)
	return res.RowsAffected, res.Error
}

func (g qqbotGormAccess) CountOutbox(status string) (int64, error) {
	var n int64
	err := g.db().Model(&model.QqbotOutbox{}).Where("status = ?", status).Count(&n).Error
	return n, err
}

func (g qqbotGormAccess) CountOutboxByOpenID(openID string, statuses []string) (int64, error) {
	if len(statuses) == 0 {
		return 0, nil
	}
	var n int64
	err := g.db().Model(&model.QqbotOutbox{}).
		Where("open_id = ? AND status IN ?", openID, statuses).Count(&n).Error
	return n, err
}

func (g qqbotGormAccess) LatestOutboxWithError() (*model.QqbotOutbox, error) {
	var o model.QqbotOutbox
	err := g.db().Where("last_kind <> ''").Order("updated_at DESC").First(&o).Error
	if err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &o, nil
}

// ---------- 会话聚合 ----------

func (g qqbotGormAccess) ListConversationOpenIDs(offset, limit int) ([]qqbotConversationRow, int64, error) {
	var total int64
	if err := g.db().Model(&model.QqbotMessage{}).Distinct("open_id").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []qqbotConversationRow
	err := g.db().Model(&model.QqbotMessage{}).
		Select("open_id, COUNT(*) AS message_count, MAX(created_at) AS last_message_at").
		Group("open_id").Order("last_message_at DESC").
		Offset(offset).Limit(limit).Scan(&rows).Error
	return rows, total, err
}

// ---------- 邀请码 ----------

func (g qqbotGormAccess) InviteCodeExists(code string) (bool, error) {
	if code == "" {
		return false, nil
	}
	var n int64
	// Unscoped：软删的邀请码仍占唯一索引（与 T-37e 绑定表的教训同源），碰撞判断要看全量。
	err := g.db().Unscoped().Model(&model.Invite{}).Where("invite_code = ?", code).Count(&n).Error
	return n > 0, err
}

func (g qqbotGormAccess) CreateInvite(inv *model.Invite) error {
	if inv == nil {
		return nil
	}
	return g.db().Create(inv).Error
}

// accessor 当前数据访问实现（测试可替换；生产恒为 qqbotGormAccess）。
func (s *QqbotService) accessor() qqbotDataAccess {
	if s.data != nil {
		return s.data
	}
	return qqbotGormAccess{s: s}
}

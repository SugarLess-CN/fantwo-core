// 基础设施域：搜索日志（热门搜索词来源）。
//
// 目的：content 域的 GET /article/hot-search 此前是「热门标签 + 热门分类」合成的
// 降级实现；本服务提供真实的检索行为日志与 top-N 聚合，供其替换。
//
// 设计要点：
//   - 落库在检索成功路径的旁路（异步、失败不影响检索响应）；
//   - 去重窗口 60s（同一用户/同一 IP 对同一关键词的重复检索只记一条），
//     避免刷榜与前端防抖失败造成的噪声；
//   - 聚合按 keyword 分组统计，配合 (keyword, created_at) 联合索引做范围扫描；
//   - 结果条数为 0 的关键词默认不计入热门（可关闭该过滤）。
package infra

import (
	"context"
	"strings"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// 搜索日志相关常量。
const (
	// searchKeywordMaxLen 关键词最大长度（与 search_logs.keyword 的 varchar(128) 对齐）。
	searchKeywordMaxLen = 128
	// searchDedupWindow 去重窗口：同主体同关键词在窗口内只记一条。
	searchDedupWindow = time.Minute
	// searchDedupPrefix Redis 去重键前缀。
	searchDedupPrefix = "search:dedup:"
	// hotKeywordsDefaultDays / hotKeywordsDefaultLimit 热门词默认窗口与条数。
	hotKeywordsDefaultDays  = 7
	hotKeywordsDefaultLimit = 10
	hotKeywordsMaxLimit     = 50
)

// SearchLogService 搜索日志记录与热门聚合。
type SearchLogService struct {
	db  *gorm.DB
	rdb *redis.Client
}

// NewSearchLogService 构造搜索日志服务（rdb 可为 nil，降级为 DB 判重）。
func NewSearchLogService(db *gorm.DB, rdb *redis.Client) *SearchLogService {
	return &SearchLogService{db: db, rdb: rdb}
}

// RecordInput 一次检索的日志入参。
type RecordInput struct {
	Keyword     string // 检索词（会做 trim + 截断）
	UserID      string // 登录用户 id；游客为空
	Scene       string // article / global（空值按 article）
	IP          string // 客户端 IP
	ResultCount int    // 本次命中结果数
}

// Record 记录一次检索（旁路调用，错误由调用方决定是否忽略）。
//
// 去重策略：Redis 可用时用 SETNX（窗口内重复直接返回 nil，不写库）；
// Redis 不可用时退化为一次 DB 查询判重（60s 内同主体同词已存在则跳过）。
func (s *SearchLogService) Record(ctx context.Context, in RecordInput) error {
	if s.db == nil {
		return nil // 检索是只读主路径，日志缺库时静默跳过
	}
	keyword := strings.TrimSpace(in.Keyword)
	if keyword == "" {
		return nil
	}
	if runes := []rune(keyword); len(runes) > searchKeywordMaxLen {
		keyword = string(runes[:searchKeywordMaxLen])
	}
	scene := in.Scene
	if scene == "" {
		scene = model.SearchSceneArticle
	}

	subject := in.UserID
	if subject == "" {
		subject = "ip:" + in.IP
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if s.rdb != nil {
		key := searchDedupPrefix + subject + ":" + keyword
		ok, err := s.rdb.SetNX(ctx, key, 1, searchDedupWindow).Result()
		if err == nil && !ok {
			return nil // 窗口内已记录
		}
	} else if s.recentlyLogged(ctx, keyword, in.UserID) {
		return nil
	}

	var userID *string
	if in.UserID != "" {
		uid := in.UserID
		userID = &uid
	}
	entry := &model.SearchLog{
		Keyword:     keyword,
		UserID:      userID,
		Scene:       scene,
		ResultCount: in.ResultCount,
		IP:          truncateBytes(in.IP, 64),
		CreatedAt:   time.Now(),
	}
	return s.db.WithContext(ctx).Create(entry).Error
}

// recentlyLogged 无 Redis 时的 DB 判重（60s 窗口）。
func (s *SearchLogService) recentlyLogged(ctx context.Context, keyword, userID string) bool {
	var cnt int64
	q := s.db.WithContext(ctx).Model(&model.SearchLog{}).
		Where("keyword = ? AND created_at >= ?", keyword, time.Now().Add(-searchDedupWindow))
	if userID != "" {
		q = q.Where("user_id = ?", userID)
	} else {
		q = q.Where("user_id IS NULL")
	}
	if err := q.Limit(1).Count(&cnt).Error; err != nil {
		return false // 判重失败不影响记录
	}
	return cnt > 0
}

// TopKeywords 近 days 天热门关键词 top N（days 1..90，limit 1..50）。
//
// minResults > 0 时只统计命中数 ≥ minResults 的词（默认 1，即剔除零结果词）。
func (s *SearchLogService) TopKeywords(ctx context.Context, days, limit int, minResults int) ([]model.HotKeyword, error) {
	return s.TopKeywordsByScene(ctx, "", days, limit, minResults)
}

// TopKeywordsByScene 按场景聚合（scene 为空表示全场景）。
func (s *SearchLogService) TopKeywordsByScene(ctx context.Context, scene string, days, limit, minResults int) ([]model.HotKeyword, error) {
	if s.db == nil {
		return nil, svcUnavailable("数据库不可用")
	}
	if days <= 0 {
		days = hotKeywordsDefaultDays
	}
	if days > 90 {
		days = 90
	}
	if limit <= 0 {
		limit = hotKeywordsDefaultLimit
	}
	if limit > hotKeywordsMaxLimit {
		limit = hotKeywordsMaxLimit
	}
	if minResults <= 0 {
		minResults = 1
	}

	// 联合索引 (keyword, created_at) 支撑 created_at 范围扫描 + keyword 分组
	sql := `SELECT keyword, COUNT(*) AS count FROM search_logs
		WHERE created_at >= ? AND keyword <> '' AND result_count >= ?`
	args := []interface{}{time.Now().AddDate(0, 0, -days), minResults}
	if scene != "" {
		sql += " AND scene = ?"
		args = append(args, scene)
	}
	sql += ` GROUP BY keyword ORDER BY count DESC, keyword ASC LIMIT ?`
	args = append(args, limit)

	var rows []model.HotKeyword
	if err := s.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		if isMissingTable(err) {
			return []model.HotKeyword{}, nil // 表未建（迁移未跑）时返回空集
		}
		return nil, svcInternal("聚合热门搜索词失败: %v", err)
	}
	if rows == nil {
		rows = []model.HotKeyword{}
	}
	return rows, nil
}

// Purge 清理 days 天前的搜索日志（运维用，可由定时任务调用）。
func (s *SearchLogService) Purge(ctx context.Context, olderThanDays int) (int64, error) {
	if s.db == nil {
		return 0, svcUnavailable("数据库不可用")
	}
	if olderThanDays <= 0 {
		olderThanDays = 90
	}
	cutoff := time.Now().AddDate(0, 0, -olderThanDays)
	res := s.db.WithContext(ctx).Where("created_at < ?", cutoff).Delete(&model.SearchLog{})
	if res.Error != nil {
		if isMissingTable(res.Error) {
			return 0, nil
		}
		return 0, svcInternal("清理搜索日志失败: %v", res.Error)
	}
	return res.RowsAffected, nil
}

// truncateBytes 按字节截断（避免超长 IP/UA 撑爆列宽；不切断多字节尾部）。
func truncateBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	b := []byte(s[:max])
	for len(b) > 0 && b[len(b)-1]&0xC0 == 0x80 { // 去掉悬挂的 UTF-8 续字节
		b = b[:len(b)-1]
	}
	return string(b)
}

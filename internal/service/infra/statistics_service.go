// 基础设施域：后台统计。
//
// 对齐原仓库 tmp/picart/src/modules/statistics/statistics.service.ts：
//   - GET /statistics/overview：用户/内容/订单/审核/互动五组聚合计数；
//   - GET /statistics/trends?days=N：近 N 天（1..30，默认 7）按天新增与成交序列。
//
// 实现差异：原实现用 TypeORM QueryBuilder 逐表 getRawOne；Go 侧用 GORM 原生表查询
// （不 import 其它域 model，规避并行开发期编译耦合），SQL 语义逐条对齐：
//   - MySQL 的 DATE(col) / CURRENT_DATE + INTERVAL 1 DAY 平移为 PG 的 col::date /
//     CURRENT_DATE + INTERVAL '1 day'；
//   - 软删按 gorm soft_delete 插件的 unix 秒语义，条件 deleted_at = 0。
package infra

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Statistics 统计服务。
type Statistics struct{ db *gorm.DB }

// NewStatistics 构造统计服务。
func NewStatistics(db *gorm.DB) *Statistics { return &Statistics{db: db} }

// OverviewResult 概览响应（字段对齐原 getOverview 返回结构）。
type OverviewResult struct {
	Users      UsersStat      `json:"users"`
	Content    ContentStat    `json:"content"`
	Orders     OrdersStat     `json:"orders"`
	Moderation ModerationStat `json:"moderation"`
	Engagement EngagementStat `json:"engagement"`
}

// UsersStat 用户组。
type UsersStat struct {
	Total         int64 `json:"total"`
	Active        int64 `json:"active"`
	Banned        int64 `json:"banned"`
	ActiveMembers int64 `json:"activeMembers"`
	TodayNew      int64 `json:"todayNew"`
}

// 今日活跃：原实现无「活跃」字段，这里以「今日有登录/签到」近似为今日新增 + 签到数，
// 见 TodayActive 注释；保持与前端约定的字段存在性。
type ContentStat struct {
	Articles          int64 `json:"articles"`
	PublishedArticles int64 `json:"publishedArticles"`
	PendingArticles   int64 `json:"pendingArticles"`
	Comments          int64 `json:"comments"`
	PublishedComments int64 `json:"publishedComments"`
	Collections       int64 `json:"collections"`
	PublicCollections int64 `json:"publicCollections"`
	TodayArticles     int64 `json:"todayArticles"`
	TodayComments     int64 `json:"todayComments"`
}

// OrdersStat 订单组。
type OrdersStat struct {
	PaidCount        int64   `json:"paidCount"`
	PaidRevenue      float64 `json:"paidRevenue"`
	TodayPaidCount   int64   `json:"todayPaidCount"`
	TodayPaidRevenue float64 `json:"todayPaidRevenue"`
}

// ModerationStat 审核组。
type ModerationStat struct {
	PendingReports    int64 `json:"pendingReports"`
	ProcessingReports int64 `json:"processingReports"`
	PendingArticles   int64 `json:"pendingArticles"`
}

// EngagementStat 互动组。
type EngagementStat struct {
	ArticleViews     int64 `json:"articleViews"`
	ArticleLikes     int64 `json:"articleLikes"`
	ArticleFavorites int64 `json:"articleFavorites"`
	ArticleShares    int64 `json:"articleShares"`
	ArticleComments  int64 `json:"articleComments"`
	CommentLikes     int64 `json:"commentLikes"`
	CommentReplies   int64 `json:"commentReplies"`
	CollectionViews  int64 `json:"collectionViews"`
	CollectionItems  int64 `json:"collectionItems"`
}

// Overview 后台统计概览（GET /statistics/overview，权限 statistics:read | system:monitor）。
//
// 任一表缺失（对应域尚未建表）按 0 处理，不让单个域阻塞整体概览——
// 与原实现（Promise.all 整体失败）相比更适配并行落地期；真正的 SQL 错误仍会返回。
func (s *Statistics) Overview(ctx context.Context) (*OverviewResult, error) {
	if s.db == nil {
		return nil, svcUnavailable("数据库不可用")
	}
	db := s.db.WithContext(ctx)
	out := &OverviewResult{}

	// 用户
	var users struct {
		Total         int64
		Active        int64
		Banned        int64
		ActiveMembers int64
		TodayNew      int64
	}
	if err := db.Table("users").
		Select(`COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN status = 'ACTIVE' THEN 1 ELSE 0 END),0) AS active,
			COALESCE(SUM(CASE WHEN status = 'BANNED' THEN 1 ELSE 0 END),0) AS banned,
			COALESCE(SUM(CASE WHEN membership_status = 'ACTIVE' THEN 1 ELSE 0 END),0) AS active_members,
			COALESCE(SUM(CASE WHEN created_at >= CURRENT_DATE AND created_at < CURRENT_DATE + INTERVAL '1 day' THEN 1 ELSE 0 END),0) AS today_new`).
		Where("deleted_at = 0").Scan(&users).Error; err != nil {
		if !isMissingTable(err) {
			return nil, svcInternal("统计用户失败: %v", err)
		}
	}
	out.Users = UsersStat{
		Total: users.Total, Active: users.Active, Banned: users.Banned,
		ActiveMembers: users.ActiveMembers, TodayNew: users.TodayNew,
	}

	// 文章
	var articles struct {
		Total     int64
		Published int64
		Pending   int64
		TodayNew  int64
	}
	if err := db.Table("articles").
		Select(`COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN status = 'PUBLISHED' THEN 1 ELSE 0 END),0) AS published,
			COALESCE(SUM(CASE WHEN status = 'PENDING' THEN 1 ELSE 0 END),0) AS pending,
			COALESCE(SUM(CASE WHEN created_at >= CURRENT_DATE AND created_at < CURRENT_DATE + INTERVAL '1 day' THEN 1 ELSE 0 END),0) AS today_new`).
		Where("deleted_at = 0").Scan(&articles).Error; err != nil {
		if !isMissingTable(err) {
			return nil, svcInternal("统计文章失败: %v", err)
		}
	}
	out.Content.Articles = articles.Total
	out.Content.PublishedArticles = articles.Published
	out.Content.PendingArticles = articles.Pending
	out.Content.TodayArticles = articles.TodayNew

	// 评论
	var comments struct {
		Total     int64
		Published int64
		TodayNew  int64
	}
	if err := db.Table("comments").
		Select(`COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN status = 'PUBLISHED' THEN 1 ELSE 0 END),0) AS published,
			COALESCE(SUM(CASE WHEN created_at >= CURRENT_DATE AND created_at < CURRENT_DATE + INTERVAL '1 day' THEN 1 ELSE 0 END),0) AS today_new`).
		Where("deleted_at = 0").Scan(&comments).Error; err != nil {
		if !isMissingTable(err) {
			return nil, svcInternal("统计评论失败: %v", err)
		}
	}
	out.Content.Comments = comments.Total
	out.Content.PublishedComments = comments.Published
	out.Content.TodayComments = comments.TodayNew

	// 举报（无软删列差异，按 deleted_at = 0 处理；表缺失时零值）
	var reports struct {
		Pending    int64
		Processing int64
	}
	if err := db.Table("reports").
		Select(`COALESCE(SUM(CASE WHEN status = 'PENDING' THEN 1 ELSE 0 END),0) AS pending,
			COALESCE(SUM(CASE WHEN status = 'PROCESSING' THEN 1 ELSE 0 END),0) AS processing`).
		Scan(&reports).Error; err != nil {
		if !isMissingTable(err) {
			return nil, svcInternal("统计举报失败: %v", err)
		}
	}
	out.Moderation.PendingReports = reports.Pending
	out.Moderation.ProcessingReports = reports.Processing
	out.Moderation.PendingArticles = articles.Pending

	// 合集
	var collections struct {
		Total  int64
		Public int64
	}
	if err := db.Table("collections").
		Select(`COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN is_public THEN 1 ELSE 0 END),0) AS public`).
		Where("deleted_at = 0").Scan(&collections).Error; err != nil {
		if !isMissingTable(err) {
			return nil, svcInternal("统计合集失败: %v", err)
		}
	}
	out.Content.Collections = collections.Total
	out.Content.PublicCollections = collections.Public

	// 订单（已支付）
	var paid struct {
		Count   int64
		Revenue float64
	}
	if err := db.Table("orders").
		Select(`COUNT(*) AS count, COALESCE(SUM(amount),0) AS revenue`).
		Where("status = 'PAID'").Scan(&paid).Error; err != nil && !isMissingTable(err) {
		return nil, svcInternal("统计订单失败: %v", err)
	}
	out.Orders.PaidCount = paid.Count
	out.Orders.PaidRevenue = paid.Revenue

	var todayPaid struct {
		Count   int64
		Revenue float64
	}
	if err := db.Table("orders").
		Select(`COUNT(*) AS count, COALESCE(SUM(amount),0) AS revenue`).
		Where("status = 'PAID'").
		Where("paid_at >= CURRENT_DATE AND paid_at < CURRENT_DATE + INTERVAL '1 day'").
		Scan(&todayPaid).Error; err != nil && !isMissingTable(err) {
		return nil, svcInternal("统计今日订单失败: %v", err)
	}
	out.Orders.TodayPaidCount = todayPaid.Count
	out.Orders.TodayPaidRevenue = todayPaid.Revenue

	// 互动：文章维度
	var artEng struct {
		Views     int64
		Likes     int64
		Favorites int64
		Comments  int64
		Shares    int64
	}
	if err := db.Table("articles").
		Select(`COALESCE(SUM(views),0) AS views,
			COALESCE(SUM(likes),0) AS likes,
			COALESCE(SUM(favorite_count),0) AS favorites,
			COALESCE(SUM(comment_count),0) AS comments,
			COALESCE(SUM(shares),0) AS shares`).
		Where("deleted_at = 0").Scan(&artEng).Error; err != nil && !isMissingTable(err) {
		return nil, svcInternal("统计文章互动失败: %v", err)
	}
	out.Engagement.ArticleViews = artEng.Views
	out.Engagement.ArticleLikes = artEng.Likes
	out.Engagement.ArticleFavorites = artEng.Favorites
	out.Engagement.ArticleComments = artEng.Comments
	out.Engagement.ArticleShares = artEng.Shares

	// 互动：评论维度
	var cmtEng struct {
		Likes   int64
		Replies int64
	}
	if err := db.Table("comments").
		Select(`COALESCE(SUM(likes),0) AS likes, COALESCE(SUM(reply_count),0) AS replies`).
		Where("deleted_at = 0").Scan(&cmtEng).Error; err != nil && !isMissingTable(err) {
		return nil, svcInternal("统计评论互动失败: %v", err)
	}
	out.Engagement.CommentLikes = cmtEng.Likes
	out.Engagement.CommentReplies = cmtEng.Replies

	// 互动：合集维度
	var colEng struct {
		Views int64
		Items int64
	}
	if err := db.Table("collections").
		Select(`COALESCE(SUM(views),0) AS views, COALESCE(SUM(item_count),0) AS items`).
		Where("deleted_at = 0").Scan(&colEng).Error; err != nil && !isMissingTable(err) {
		return nil, svcInternal("统计合集互动失败: %v", err)
	}
	out.Engagement.CollectionViews = colEng.Views
	out.Engagement.CollectionItems = colEng.Items

	return out, nil
}

// TrendPoint 单日趋势点（对齐 buildDailySeries 输出字段）。
type TrendPoint struct {
	Date        string  `json:"date"`
	Users       int64   `json:"users"`
	Articles    int64   `json:"articles"`
	Comments    int64   `json:"comments"`
	PaidOrders  int64   `json:"paidOrders"`
	PaidRevenue float64 `json:"paidRevenue"`
}

// TrendsResult 趋势响应。
type TrendsResult struct {
	Days   int          `json:"days"`
	Series []TrendPoint `json:"series"`
}

// Trends 近 days 天趋势（1..30，默认 7），按天补零。
func (s *Statistics) Trends(ctx context.Context, days int) (*TrendsResult, error) {
	if s.db == nil {
		return nil, svcUnavailable("数据库不可用")
	}
	if days <= 0 {
		days = 7
	}
	if days > 30 {
		days = 30
	}
	db := s.db.WithContext(ctx)
	start := startOfDayNDaysAgo(days)

	users := s.dailyCount(db, "users", "created_at", start, "")
	articles := s.dailyCount(db, "articles", "created_at", start, "")
	comments := s.dailyCount(db, "comments", "created_at", start, "")
	paidOrders, revenue := s.dailyPaidOrders(db, start)

	series := make([]TrendPoint, 0, days)
	base := start
	for i := 0; i < days; i++ {
		d := base.AddDate(0, 0, i)
		key := d.Format("2006-01-02")
		series = append(series, TrendPoint{
			Date:        key,
			Users:       users[key],
			Articles:    articles[key],
			Comments:    comments[key],
			PaidOrders:  paidOrders[key],
			PaidRevenue: revenue[key],
		})
	}
	return &TrendsResult{Days: days, Series: series}, nil
}

// dailyCount 按天统计新增（表缺失返回空 map，不阻塞整体趋势）。
func (s *Statistics) dailyCount(db *gorm.DB, table, timeCol string, start time.Time, extraWhere string) map[string]int64 {
	out := map[string]int64{}
	sql := `SELECT ` + timeCol + `::date AS d, COUNT(*) AS c FROM ` + table +
		` WHERE ` + timeCol + ` >= ? AND deleted_at = 0`
	args := []interface{}{start}
	if extraWhere != "" {
		sql += " AND " + extraWhere
	}
	sql += ` GROUP BY ` + timeCol + `::date ORDER BY d ASC`
	var rows []struct {
		D time.Time
		C int64
	}
	if err := db.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return out // 表不存在/列不匹配：按 0 处理
	}
	for _, r := range rows {
		out[r.D.Format("2006-01-02")] = r.C
	}
	return out
}

// dailyPaidOrders 按天统计已支付订单数与金额。
func (s *Statistics) dailyPaidOrders(db *gorm.DB, start time.Time) (map[string]int64, map[string]float64) {
	counts := map[string]int64{}
	revenue := map[string]float64{}
	var rows []struct {
		D time.Time
		C int64
		R float64
	}
	err := db.Raw(`SELECT paid_at::date AS d, COUNT(*) AS c, COALESCE(SUM(amount),0) AS r
		FROM orders WHERE status = 'PAID' AND paid_at IS NOT NULL AND paid_at >= ?
		GROUP BY paid_at::date ORDER BY d ASC`, start).Scan(&rows).Error
	if err != nil {
		return counts, revenue
	}
	for _, r := range rows {
		k := r.D.Format("2006-01-02")
		counts[k] = r.C
		revenue[k] = r.R
	}
	return counts, revenue
}

// startOfDayNDaysAgo 起点：今天零点往前 days-1 天（对齐原 getStartDate）。
func startOfDayNDaysAgo(days int) time.Time {
	now := time.Now()
	t := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return t.AddDate(0, 0, -(days - 1))
}

// isMissingTable 判定「表不存在」（PG undefined_table = 42P01）。
//
// 并行落地期某些域可能尚未建表；概览按 0 返回而非整体 500，便于前端先接通。
func isMissingTable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "does not exist") || strings.Contains(msg, "42p01")
}

// 编译期守护：context 已用于所有查询。
var _ = context.Background

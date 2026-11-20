// 基础设施域实体：搜索日志（search_logs）。
//
// 用途：热门搜索词聚合（近 N 天 top N 关键词），供内容域 /article/hot-search 使用。
// 原仓库无独立搜索日志实体（hot-search 走的是另一套统计），这是 Go 重构侧为
// 消除 content 域「热门标签合成」降级实现而新增的表，语义对齐主流社区做法：
// 每次检索落一条，按 keyword + 时间聚合。
//
// 不内嵌 BaseModel：搜索日志只需 id + created_at，且要在 (keyword, created_at)
// 上建联合索引（内嵌的 CreatedAt 无法挂复合索引标签）；同时日志表不做软删。
package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// 搜索场景（区分来源，便于分别统计）。
const (
	SearchSceneArticle = "article" // 文章检索（/search、/article/search）
	SearchSceneGlobal  = "global"  // 全站检索
)

// SearchLog 一次检索行为的落库记录。
type SearchLog struct {
	ID string `gorm:"column:id;type:varchar(36);primaryKey" json:"id"`

	// Keyword 检索词（去空白、截断 128，避免超长垃圾词撑爆索引）
	Keyword string `gorm:"column:keyword;type:varchar(128);not null;index:idx_search_logs_keyword_created,priority:1" json:"keyword"`
	// UserID 检索用户（游客为空；不建外键，跨域解耦）
	UserID *string `gorm:"column:user_id;type:varchar(36);index:idx_search_logs_user" json:"userId"`
	// Scene 检索场景（article / global）
	Scene string `gorm:"column:scene;type:varchar(16);not null;default:article;index:idx_search_logs_scene" json:"scene"`
	// ResultCount 本次命中的结果条数（用于剔除「零结果垃圾词」）
	ResultCount int `gorm:"column:result_count;not null;default:0" json:"resultCount"`
	// IP 客户端 IP（截断 64；仅用于风控排查，不参与聚合）
	IP string `gorm:"column:ip;type:varchar(64)" json:"ip"`

	// CreatedAt 检索时间（与 keyword 组成联合索引，支撑「近 N 天 top N」范围扫描）
	CreatedAt time.Time `gorm:"column:created_at;index:idx_search_logs_keyword_created,priority:2" json:"createdAt"`
}

// TableName 表名。
func (SearchLog) TableName() string { return "search_logs" }

// BeforeCreate 落库前生成 UUID 主键（与 BaseModel 同语义，但不继承软删列）。
func (s *SearchLog) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now()
	}
	return nil
}

// HotKeyword 热门关键词聚合结果。
type HotKeyword struct {
	Keyword string `json:"keyword"`
	Count   int64  `json:"count"`
}

package dto

// 本文件覆盖 collection 域（合集 / 合集条目）的全部对外请求与响应结构。
//
// 字段来源（逐条可追，不是想象）：
//   - 响应实体 ← internal/service/collection.go 的 CollectionView / CollectionItemView
//   - 响应实体 ← 同文件各方法里 map[string]interface{} 字面量的键（已同步改为具名结构）
//   - 请求体   ← 同文件 CreateCollectionRequest / UpdateCollectionRequest / ReorderCollectionItemsRequest
//   - 内嵌文章 ← dto.ArticleDto（service.ArticleView 的别名，由 content 域维护）
//
// 空值口径见 docs/21-JSON序列化规范.md：
//   - 字符串 → 值类型，空时 ""（不用 *string + omitempty，指针只会带来 null）
//   - 数组   → 必须非 nil，空时 []
//   - 对象   → *T + omitempty，空时整个字段不传

// ---------------------------------------------------------------------------
// 响应实体
// ---------------------------------------------------------------------------

// CollectionDto 合集基础视图（列表项 / 创建 / 更新 的返回）。
//
// 对应 service.CollectionView（已改为本类型的别名，两者不会漂移）。
// 详情多出的 items 在 CollectionDetailDto 里，不放这里 —— 否则列表接口每条
// 都会带一个 items 字段，25 条就是 25 个空壳。
type CollectionDto struct {
	ID   string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Name string `json:"name" example:"我的收藏"`

	// 字符串：值类型，空时 ""（docs/21 §一）。
	// 改前是 *string + omitempty（空时省略），两种都不是 null，但规范要求 ""。
	Description string `json:"description" example:"风景合集"`
	Avatar      string `json:"avatar" example:"https://cdn.example.com/col.png"`
	Cover       string `json:"cover" example:"https://cdn.example.com/cover.png"`

	IsPublic  bool   `json:"is_public" example:"true"`
	Sort      int    `json:"sort" example:"3"`
	UserID    string `json:"user_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	ItemCount int    `json:"item_count" example:"15"`
	Views     int    `json:"views" example:"100"`

	// 时间必有值（DB 记录的 created_at / updated_at），用 RFC3339 字符串。
	CreatedAt string `json:"created_at" format:"date-time" example:"2026-11-25T10:00:00Z"`
	UpdatedAt string `json:"updated_at" format:"date-time" example:"2026-11-25T10:00:00Z"`
}

// CollectionDetailDto 合集详情（GET /collection/{id}）：基础视图 + 内嵌条目。
//
// Items **没有 omitempty**：无条目时必须是 `[]` 而不是省略或 null（docs/21 §一 数组规则）。
// 唯一组装点 service.FindOne → buildItemViews 用 make([]T, 0, n) 保证非 nil。
type CollectionDetailDto struct {
	CollectionDto
	Items []CollectionItemDto `json:"items"`
}

// CollectionItemDto 合集条目视图（service.CollectionItemView 的镜像）。
type CollectionItemDto struct {
	ID        string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	ArticleID string `json:"article_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Sort      int    `json:"sort" example:"3"`

	// 字符串：空时 ""（改前是 *string + omitempty，空时省略）。
	Note string `json:"note" example:"这张最好"`

	// 内嵌文章：对象，空时整个字段不传（docs/21 §一）。
	// 类型是 dto.ArticleDto（service.ArticleView 的别名）—— 与 article 域共用一份定义，
	// 不在这里重复声明字段（那样两边一改就打架）。
	Article *ArticleDto `json:"article,omitempty"`

	CreatedAt string `json:"created_at" format:"date-time" example:"2026-11-25T10:00:00Z"`
}

// CollectionArticleAddResultDto 添加文章到合集的结果（POST /collection/{id}/article/{articleId}）。
//
// 重复添加是幂等的：返回 added=false 且 **不带 sort**（没有新排序值可给）。
// sort 用 *int + omitempty 表达「此字段条件存在」，不是「空值」——
// 用 int + omitempty 会把真实 sort=0 一起省掉，语义就糊了。
type CollectionArticleAddResultDto struct {
	Added bool `json:"added" example:"true"`
	Sort  *int `json:"sort,omitempty" example:"3"`
}

// CollectionArticleRemoveResultDto 从合集移除文章的结果。
type CollectionArticleRemoveResultDto struct {
	Removed bool `json:"removed" example:"true"`
}

// CollectionReorderResultDto 合集内文章重排的结果（reordered = 提交的条数）。
type CollectionReorderResultDto struct {
	Reordered int `json:"reordered" example:"3"`
}

// CollectionCheckDto 文章所在合集检查（GET /collection/check/{articleId}）。
type CollectionCheckDto struct {
	InCollection bool                      `json:"in_collection" example:"true"`
	Collections  []CollectionCheckBriefDto `json:"collections"`
}

// CollectionCheckBriefDto 检查结果里的合集摘要（4 字段，不含 userId）。
type CollectionCheckBriefDto struct {
	ID        string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Name      string `json:"name" example:"我的收藏"`
	IsPublic  bool   `json:"is_public" example:"true"`
	ItemCount int    `json:"item_count" example:"15"`
}

// ArticleCollectionInfoDto 文章所在合集信息（GET /collection/article/{articleId}/info）。
//
// 与 CollectionCheckDto 的区别：这里多 userId（要判断合集归属谁）、
// 少 itemCount（详情页跳转用不到）。刻意不复用同一个 brief 类型 ——
// 复用会让文档声明出运行时根本不存在的字段。
type ArticleCollectionInfoDto struct {
	ArticleID   string                      `json:"article_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Collections []ArticleCollectionBriefDto `json:"collections"`
}

// ArticleCollectionBriefDto 文章合集归属摘要（4 字段，含 userId）。
type ArticleCollectionBriefDto struct {
	ID       string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Name     string `json:"name" example:"我的收藏"`
	UserID   string `json:"user_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	IsPublic bool   `json:"is_public" example:"true"`
}

// ---------------------------------------------------------------------------
// 请求体
// ---------------------------------------------------------------------------

// CreateCollectionRequest 创建合集（POST /collection）。
//
// 与 service.CreateCollectionRequest 同名同形 —— service 里已改为本类型的别名，
// 只有一份定义，注解与实际绑定不会漂移。
type CreateCollectionRequest struct {
	Name        string  `json:"name" binding:"required,max=100" example:"我的收藏"`
	Description *string `json:"description" example:"风景合集"`
	Avatar      *string `json:"avatar" example:"https://cdn.example.com/col.png"`
	Cover       *string `json:"cover" example:"https://cdn.example.com/cover.png"`
	IsPublic    *bool   `json:"is_public" example:"true"`
	Sort        *int    `json:"sort" example:"0"`
}

// UpdateCollectionRequest 更新合集（PATCH /collection/{id}），全字段可选（nil = 不改）。
type UpdateCollectionRequest struct {
	Name        *string `json:"name" binding:"omitempty,max=100" example:"新名字"`
	Description *string `json:"description" example:"新描述"`
	Avatar      *string `json:"avatar" example:"https://cdn.example.com/a.png"`
	Cover       *string `json:"cover" example:"https://cdn.example.com/cover.png"`
	IsPublic    *bool   `json:"is_public" example:"true"`
	Sort        *int    `json:"sort" example:"1"`
}

// ReorderCollectionItemsRequest 合集内文章重排（PUT /collection/{id}/items/order）。
//
// articleIds 的**顺序即展示顺序**（越靠前越前），不是按 sort 值提交。
type ReorderCollectionItemsRequest struct {
	ArticleIDs []string `json:"article_ids" binding:"required,min=1" example:"[\"id-a\",\"id-b\"]"`
}

package dto

// 本文件覆盖 emoji 域（表情、表情包、包内条目、收藏、使用计数）的全部对外
// 请求与响应结构。
//
// 字段来源（不是想象，逐条可追）：
//   - EmojiDto / EmojiPackDto / EmojiPackItemDto ← internal/model/emoji.go 的 json tag
//   - EmojiPackDetailDto ← internal/service/emoji_service.go 的 EmojiPackDetail
//   - 请求体 ← internal/model/emoji.go 的 EmojiInput / EmojiPackInput（指针 = PATCH 语义）
//   - 动作类响应 ← internal/handler/emoji.go 里的 gin.H 字面量键
//
// 空值规则（docs/21-JSON序列化规范.md）：
//   - 字符串 → 值类型 string，空时 ""（model 里 emoji 的字段本就全是值类型，无 null 风险）
//   - 数组   → 去掉 omitempty，赋值时 make([]T, 0, n)，空时 []
//   - 时间   → string（RFC3339）；emoji 三张表的时间列 NOT NULL，无需 omitempty

// ---------------------------------------------------------------------------
// 表情（响应）
// ---------------------------------------------------------------------------

// EmojiDto 单个表情对外视图（model.Emoji，15 个业务字段 + id/时间）。
//
// 用于：GET /emoji（列表项）、GET /emoji/{id}、POST /emoji、PATCH /emoji/{id}、
// GET /emoji/popular/list、GET /emoji/recent/list、GET /emoji/favorites/list、
// 以及表情包详情里的 emojis 数组元素。
//
// 全部字符串字段用值类型：model.Emoji 本来就是 string（无 SQL NULL），
// 这一层只是把时间转成 RFC3339 文本。
type EmojiDto struct {
	ID        string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	CreatedAt string `json:"createdAt" format:"date-time" example:"2026-11-25T10:00:00Z"`
	UpdatedAt string `json:"updatedAt" format:"date-time" example:"2026-11-25T10:00:05Z"`

	Name     string `json:"name" example:"开心"`
	URL      string `json:"url" example:"https://cdn.example.com/emoji/happy.png"`
	Code     string `json:"code" example:":smile:"`
	Type     string `json:"type" example:"user"` // system / user
	UserID   string `json:"userId" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Category string `json:"category" example:"表情"`
	Tags     string `json:"tags" example:"开心,日常"` // 逗号分隔（原实现如此）
	UseCount int64  `json:"useCount" example:"128"`
	IsPublic bool   `json:"isPublic" example:"true"`
	Status   string `json:"status" example:"active"` // active / inactive / deleted
	Width    int    `json:"width" example:"240"`
	Height   int    `json:"height" example:"240"`
	FileSize int64  `json:"fileSize" example:"8192"`
	MimeType string `json:"mimeType" example:"image/png"`
}

// ---------------------------------------------------------------------------
// 表情包（响应）
// ---------------------------------------------------------------------------

// EmojiPackDto 表情包对外视图（model.EmojiPack，7 个业务字段 + id/时间）。
//
// 用于：GET /emoji/packs（列表项）、GET /emoji/packs/{id}（detail.pack）、
// POST /emoji/packs、PATCH /emoji/packs/{id}。
type EmojiPackDto struct {
	ID        string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	CreatedAt string `json:"createdAt" format:"date-time" example:"2026-11-25T10:00:00Z"`
	UpdatedAt string `json:"updatedAt" format:"date-time" example:"2026-11-25T10:00:05Z"`

	Name        string `json:"name" example:"日常表情包"`
	Description string `json:"description" example:"聊天常用的 12 个表情"`
	CoverURL    string `json:"coverUrl" example:"https://cdn.example.com/emoji/cover.png"`
	UserID      string `json:"userId" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	IsPublic    bool   `json:"isPublic" example:"true"`
	Status      string `json:"status" example:"active"`
}

// EmojiPackItemDto 表情包内条目对外视图（model.EmojiPackItem）。
//
// 用于：POST /emoji/packs/{id}/emojis、PATCH /emoji/packs/{id}/emojis/{emojiId}。
type EmojiPackItemDto struct {
	ID        string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	CreatedAt string `json:"createdAt" format:"date-time" example:"2026-11-25T10:00:00Z"`
	UpdatedAt string `json:"updatedAt" format:"date-time" example:"2026-11-25T10:00:05Z"`

	PackID  string `json:"packId" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	EmojiID string `json:"emojiId" example:"9f8c7b6a-5d4e-3f2a-1b0c-0123456789ab"`
	Sort    int    `json:"sort" example:"0"`
}

// EmojiPackDetailDto 表情包详情（service.EmojiPackDetail：包本体 + 包内表情）。
//
// Emojis 是集合：空时 []，不是 null、也不是省略（docs/21 §一）。
type EmojiPackDetailDto struct {
	Pack   EmojiPackDto `json:"pack"`
	Emojis []EmojiDto   `json:"emojis"`
}

// ---------------------------------------------------------------------------
// 请求体
// ---------------------------------------------------------------------------

// EmojiInputRequest 表情创建 / 更新入参（model.EmojiInput，12 个可选字段）。
//
// POST /emoji 与 PATCH /emoji/{id} 共用：全部字段是指针 = PATCH 语义（不传 = 不改）。
// 请求体里的可选字段保留指针是 docs/21 §四 的明确例外（入参不是出参，
// 「没传」与「传空串」语义不同）。
type EmojiInputRequest struct {
	Name     *string `json:"name" example:"开心"`
	URL      *string `json:"url" example:"https://cdn.example.com/emoji/happy.png"`
	Code     *string `json:"code" example:":smile:"`
	Type     *string `json:"type" example:"user"` // 只有管理员可传 system
	Category *string `json:"category" example:"表情"`
	Tags     *string `json:"tags" example:"开心,日常"`
	IsPublic *bool   `json:"isPublic" example:"true"`
	Width    *int    `json:"width" example:"240"`
	Height   *int    `json:"height" example:"240"`
	FileSize *int64  `json:"fileSize" example:"8192"`
	MimeType *string `json:"mimeType" example:"image/png"`
	Status   *string `json:"status" example:"active"`
}

// EmojiPackInputRequest 表情包创建 / 更新入参（model.EmojiPackInput，5 个可选字段）。
type EmojiPackInputRequest struct {
	Name        *string `json:"name" example:"日常表情包"`
	Description *string `json:"description" example:"聊天常用的 12 个表情"`
	CoverURL    *string `json:"coverUrl" example:"https://cdn.example.com/emoji/cover.png"`
	IsPublic    *bool   `json:"isPublic" example:"true"`
	Status      *string `json:"status" example:"active"`
}

// EmojiPackAddEmojiRequest 向表情包添加表情（emojiId 必填）。
type EmojiPackAddEmojiRequest struct {
	EmojiID string `json:"emojiId" binding:"required" example:"9f8c7b6a-5d4e-3f2a-1b0c-0123456789ab"`
}

// EmojiPackUpdateEmojiRequest 调整包内表情排序（sort 可传 0，故不加 required）。
type EmojiPackUpdateEmojiRequest struct {
	Sort int `json:"sort" example:"0"`
}

// ---------------------------------------------------------------------------
// 动作类响应（布尔结果）
// ---------------------------------------------------------------------------

// EmojiFavoriteDto 收藏 / 取消收藏结果。
//
// POST /emoji/{id}/favorite → {"favorited":true}；
// DELETE /emoji/{id}/favorite → {"favorited":false}。两个端点复用同一结构。
type EmojiFavoriteDto struct {
	Favorited bool `json:"favorited" example:"true"`
}

// EmojiUseDto 记录表情使用结果（使用次数 +1）。
type EmojiUseDto struct {
	Incremented bool `json:"incremented" example:"true"`
}

// EmojiPackRemoveEmojiDto 从表情包移除表情的结果。
type EmojiPackRemoveEmojiDto struct {
	Removed bool `json:"removed" example:"true"`
}

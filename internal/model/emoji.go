// 表情包域实体（对齐原 emoji 模块四张表：表情/收藏/表情包/包内条目）。
package model

// 表情类型（对齐原 enum）。
const (
	EmojiTypeSystem = "system"
	EmojiTypeUser   = "user"
)

// 通用状态（表情与表情包共用；对齐原 enum）。
const (
	EmojiStatusActive   = "active"
	EmojiStatusInactive = "inactive"
	EmojiStatusDeleted  = "deleted"
)

// Emoji 单个表情。
type Emoji struct {
	BaseModel
	Name     string `gorm:"type:varchar(100);not null;comment:表情名称" json:"name"`
	URL      string `gorm:"type:varchar(500);not null;comment:表情图片地址" json:"url"`
	Code     string `gorm:"type:varchar(50);index;comment:表情代码，如 :smile:" json:"code"`
	Type     string `gorm:"type:varchar(10);default:user;index;comment:system/user" json:"type"`
	UserID   string `gorm:"type:char(36);index;comment:所属用户（系统表情为空）" json:"user_id"`
	Category string `gorm:"type:varchar(50);index;comment:分类" json:"category"`
	Tags     string `gorm:"type:text;comment:标签，逗号分隔" json:"tags"`
	UseCount int64  `gorm:"not null;default:0;index;comment:使用次数" json:"use_count"`
	IsPublic bool   `gorm:"not null;default:true;comment:是否公开" json:"is_public"`
	Status   string `gorm:"type:varchar(10);default:active;index;comment:状态" json:"status"`
	Width    int    `gorm:"not null;default:0;comment:宽" json:"width"`
	Height   int    `gorm:"not null;default:0;comment:高" json:"height"`
	FileSize int64  `gorm:"not null;default:0;comment:文件大小（字节）" json:"file_size"`
	MimeType string `gorm:"type:varchar(64);comment:MIME 类型" json:"mime_type"`
}

// TableName 表名。
func (Emoji) TableName() string { return "emojis" }

// EmojiFavorite 表情收藏（user_id + emoji_id 逻辑唯一，应用层保证）。
type EmojiFavorite struct {
	BaseModel
	UserID  string `gorm:"type:char(36);index:idx_emoji_favorite,priority:1;comment:用户ID" json:"user_id"`
	EmojiID string `gorm:"type:char(36);index:idx_emoji_favorite,priority:2;comment:表情ID" json:"emoji_id"`
}

// TableName 表名。
func (EmojiFavorite) TableName() string { return "emoji_favorites" }

// EmojiPack 表情包。
type EmojiPack struct {
	BaseModel
	Name        string `gorm:"type:varchar(100);not null;comment:表情包名称" json:"name"`
	Description string `gorm:"type:text;comment:描述" json:"description"`
	CoverURL    string `gorm:"type:varchar(500);comment:封面地址" json:"cover_url"`
	UserID      string `gorm:"type:char(36);index;comment:创建者" json:"user_id"`
	IsPublic    bool   `gorm:"not null;default:false;comment:是否公开" json:"is_public"`
	Status      string `gorm:"type:varchar(10);default:active;index;comment:状态" json:"status"`
}

// TableName 表名。
func (EmojiPack) TableName() string { return "emoji_packs" }

// EmojiPackItem 表情包内条目（pack_id + emoji_id 逻辑唯一，应用层保证）。
type EmojiPackItem struct {
	BaseModel
	PackID  string `gorm:"type:char(36);index:idx_emoji_pack_item,priority:1;comment:表情包ID" json:"pack_id"`
	EmojiID string `gorm:"type:char(36);index:idx_emoji_pack_item,priority:2;comment:表情ID" json:"emoji_id"`
	Sort    int    `gorm:"not null;default:0;comment:排序" json:"sort"`
}

// TableName 表名。
func (EmojiPackItem) TableName() string { return "emoji_pack_items" }

// EmojiQuery 表情列表查询条件（对齐原 QueryEmojiDto）。
type EmojiQuery struct {
	Page          int    `form:"page"`
	Limit         int    `form:"limit"`
	Grouped       *bool  `form:"grouped"`
	Type          string `form:"type"`
	Category      string `form:"category"`
	Keyword       string `form:"keyword"`
	IsPublic      *bool  `form:"is_public"`
	Status        string `form:"status"`
	UserID        string `form:"user_id"`
	OnlyFavorites bool   `form:"only_favorites"`
	SortBy        string `form:"sort_by"`
	SortOrder     string `form:"sort_order"`
}

// EmojiInput 表情创建/更新入参。
type EmojiInput struct {
	Name     *string `json:"name"`
	URL      *string `json:"url"`
	Code     *string `json:"code"`
	Type     *string `json:"type"`
	Category *string `json:"category"`
	Tags     *string `json:"tags"`
	IsPublic *bool   `json:"is_public"`
	Width    *int    `json:"width"`
	Height   *int    `json:"height"`
	FileSize *int64  `json:"file_size"`
	MimeType *string `json:"mime_type"`
	Status   *string `json:"status"`
}

// EmojiPackQuery 表情包列表查询条件。
type EmojiPackQuery struct {
	Page     int    `form:"page"`
	Limit    int    `form:"limit"`
	UserID   string `form:"user_id"`
	Keyword  string `form:"keyword"`
	Status   string `form:"status"`
	IsPublic *bool  `form:"is_public"`
}

// EmojiPackInput 表情包创建/更新入参。
type EmojiPackInput struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	CoverURL    *string `json:"cover_url"`
	IsPublic    *bool   `json:"is_public"`
	Status      *string `json:"status"`
}

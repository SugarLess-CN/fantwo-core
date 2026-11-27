// 轮播域实体（对齐原 banner 模块 banners 表）。
package model

// 轮播状态（对齐原 enum）。
const (
	BannerStatusActive   = "active"
	BannerStatusInactive = "inactive"
)

// Banner 首页轮播图。
type Banner struct {
	BaseModel
	Title       string `gorm:"type:varchar(255);not null;comment:标题" json:"title"`
	Description string `gorm:"type:varchar(500);comment:描述" json:"description"`
	ImageURL    string `gorm:"type:varchar(500);not null;comment:图片地址" json:"image_url"`
	LinkURL     string `gorm:"type:varchar(500);comment:跳转链接" json:"link_url"`
	SortOrder   int    `gorm:"not null;default:0;index;comment:排序" json:"sort_order"`
	Status      string `gorm:"type:varchar(20);default:active;index;comment:状态" json:"status"`
}

// TableName 表名。
func (Banner) TableName() string { return "banners" }

// BannerInput 轮播创建/更新入参。
type BannerInput struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	ImageURL    *string `json:"image_url"`
	LinkURL     *string `json:"link_url"`
	SortOrder   *int    `json:"sort_order"`
	Status      *string `json:"status"`
}

// BannerQuery 轮播列表查询条件。
type BannerQuery struct {
	Page      int    `form:"page"`
	Limit     int    `form:"limit"`
	Status    string `form:"status"`
	SortBy    string `form:"sortBy"`
	SortOrder string `form:"sortOrder"`
}

package model

// 分类状态（对齐原 Category.status enum）。
const (
	CategoryStatusEnabled  = "ENABLED"
	CategoryStatusDisabled = "DISABLED"
)

// Category 分类实体（表 categories）；Name 全局唯一，ParentID 自引用构成两级分类树。
type Category struct {
	BaseModel

	Name        string  `gorm:"type:varchar(50);not null;uniqueIndex:uk_categories_name" json:"name" example:"示例名称"`
	Description *string `gorm:"type:varchar(200)" json:"description" example:"一句话说明"`
	ParentID    *string `gorm:"type:varchar(36);index:idx_categories_parent" json:"parent_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	Link        *string `gorm:"type:varchar(200)" json:"link" example:"https://example.com"`
	Avatar      *string `gorm:"type:text" json:"avatar" example:"https://cdn.example.com/a.png"`
	Background  *string `gorm:"type:text" json:"background" example:"https://cdn.example.com/bg.png"`
	Cover       *string `gorm:"type:text" json:"cover" example:"https://cdn.example.com/cover.png"`
	Sort        int     `gorm:"not null;default:0" json:"sort" example:"1"`
	Status      string  `gorm:"type:varchar(16);not null;default:ENABLED" json:"status" example:"PUBLISHED"`

	ArticleCount int `gorm:"not null;default:0" json:"article_count"`
	FollowCount  int `gorm:"not null;default:0" json:"follow_count"`
}

// TableName 表名。
func (Category) TableName() string { return "categories" }

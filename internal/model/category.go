package model

// 分类状态（对齐原 Category.status enum）。
const (
	CategoryStatusEnabled  = "ENABLED"
	CategoryStatusDisabled = "DISABLED"
)

// Category 分类实体（表 categories）；Name 全局唯一，ParentID 自引用构成两级分类树。
type Category struct {
	BaseModel

	Name        string  `gorm:"type:varchar(50);not null;uniqueIndex:uk_categories_name" json:"name"`
	Description *string `gorm:"type:varchar(200)" json:"description"`
	ParentID    *string `gorm:"type:char(36);index:idx_categories_parent" json:"parentId"`
	Link        *string `gorm:"type:varchar(200)" json:"link"`
	Avatar      *string `gorm:"type:text" json:"avatar"`
	Background  *string `gorm:"type:text" json:"background"`
	Cover       *string `gorm:"type:text" json:"cover"`
	Sort        int     `gorm:"not null;default:0" json:"sort"`
	Status      string  `gorm:"type:varchar(16);not null;default:ENABLED" json:"status"`

	ArticleCount int `gorm:"not null;default:0" json:"articleCount"`
	FollowCount  int `gorm:"not null;default:0" json:"followCount"`
}

// TableName 表名。
func (Category) TableName() string { return "categories" }

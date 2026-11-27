package model

// Collection 合集实体（表 collections）。
//
// 与原实现一致：合集归属某用户（UserID），可公开（IsPublic）或私有；
// 与 ArticleFavorite 的区别：合集是用户自建的分类收藏夹，可含多个文章并保序（Item.Sort）。
type Collection struct {
	BaseModel

	Name        string  `gorm:"type:varchar(100);not null" json:"name" example:"示例名称"`
	Description *string `gorm:"type:text" json:"description" example:"一句话说明"`
	Avatar      *string `gorm:"type:text" json:"avatar" example:"https://cdn.example.com/a.png"`
	Cover       *string `gorm:"type:text" json:"cover" example:"https://cdn.example.com/cover.png"`
	IsPublic    bool    `gorm:"not null;default:false" json:"is_public" example:"true"`
	Sort        int     `gorm:"not null;default:0" json:"sort" example:"1"`

	UserID    string `gorm:"type:varchar(36);not null;index:idx_collections_user" json:"user_id"`
	ItemCount int    `gorm:"not null;default:0" json:"item_count"`
	Views     int    `gorm:"not null;default:0" json:"views"`
}

// TableName 表名。
func (Collection) TableName() string { return "collections" }

// CollectionItem 合集内的文章项（表 collection_items）；同一合集内同一文章唯一。
type CollectionItem struct {
	BaseModel

	CollectionID string  `gorm:"type:varchar(36);not null;uniqueIndex:uk_collection_items_col_article,priority:1" json:"collection_id"`
	ArticleID    string  `gorm:"type:varchar(36);not null;uniqueIndex:uk_collection_items_col_article,priority:2" json:"article_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	UserID       string  `gorm:"type:varchar(36);not null" json:"user_id"`
	Sort         int     `gorm:"not null;default:0" json:"sort" example:"1"`
	Note         *string `gorm:"type:text" json:"note"`
}

// TableName 表名。
func (CollectionItem) TableName() string { return "collection_items" }

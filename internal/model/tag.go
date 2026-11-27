package model

// Tag 标签实体（表 tags）；Name 全局唯一（对齐原 unique 约束）。
// ArticleCount / FollowCount 为冗余计数，由文章保存与关注动作在同事务内维护。
type Tag struct {
	BaseModel

	Name        string  `gorm:"type:varchar(50);not null;uniqueIndex:uk_tags_name" json:"name" example:"示例名称"`
	Description *string `gorm:"type:varchar(200)" json:"description" example:"一句话说明"`
	Avatar      *string `gorm:"type:text" json:"avatar" example:"https://cdn.example.com/a.png"`
	Background  *string `gorm:"type:text" json:"background" example:"https://cdn.example.com/bg.png"`
	Cover       *string `gorm:"type:text" json:"cover" example:"https://cdn.example.com/cover.png"`
	Sort        int     `gorm:"not null;default:0" json:"sort" example:"1"`

	ArticleCount int `gorm:"not null;default:0" json:"article_count"`
	FollowCount  int `gorm:"not null;default:0" json:"follow_count"`
}

// TableName 表名。
func (Tag) TableName() string { return "tags" }

// TagFollow 标签关注（表 tag_follows）；同一用户对同一标签唯一。
type TagFollow struct {
	BaseModel
	UserID string `gorm:"type:varchar(36);not null;uniqueIndex:uk_tag_follows_user_tag,priority:1" json:"user_id"`
	TagID  string `gorm:"type:varchar(36);not null;uniqueIndex:uk_tag_follows_user_tag,priority:2" json:"tag_id" example:"1a2b3c4d-0000-4e5f-8a9b-000000000001"`
}

// TableName 表名。
func (TagFollow) TableName() string { return "tag_follows" }

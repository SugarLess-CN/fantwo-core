package model

// Tag 标签实体（表 tags）；Name 全局唯一（对齐原 unique 约束）。
// ArticleCount / FollowCount 为冗余计数，由文章保存与关注动作在同事务内维护。
type Tag struct {
	BaseModel

	Name        string  `gorm:"type:varchar(50);not null;uniqueIndex:uk_tags_name" json:"name"`
	Description *string `gorm:"type:varchar(200)" json:"description"`
	Avatar      *string `gorm:"type:text" json:"avatar"`
	Background  *string `gorm:"type:text" json:"background"`
	Cover       *string `gorm:"type:text" json:"cover"`
	Sort        int     `gorm:"not null;default:0" json:"sort"`

	ArticleCount int `gorm:"not null;default:0" json:"articleCount"`
	FollowCount  int `gorm:"not null;default:0" json:"followCount"`
}

// TableName 表名。
func (Tag) TableName() string { return "tags" }

// TagFollow 标签关注（表 tag_follows）；同一用户对同一标签唯一。
type TagFollow struct {
	BaseModel
	UserID string `gorm:"type:char(36);not null;uniqueIndex:uk_tag_follows_user_tag,priority:1" json:"userId"`
	TagID  string `gorm:"type:char(36);not null;uniqueIndex:uk_tag_follows_user_tag,priority:2" json:"tagId"`
}

// TableName 表名。
func (TagFollow) TableName() string { return "tag_follows" }

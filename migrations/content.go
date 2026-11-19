package migrations

import (
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
)

// ContentModels 内容域实体清单（文章 / 评论 / 合集 / 标签 / 分类）。
func ContentModels() []interface{} {
	return []interface{}{
		&model.Article{},
		&model.ArticleTag{},
		&model.ArticleLike{},
		&model.ArticleFavorite{},
		&model.ArticleDislike{},
		&model.BrowseHistory{},
		&model.Comment{},
		&model.CommentLike{},
		&model.Collection{},
		&model.CollectionItem{},
		&model.Tag{},
		&model.TagFollow{},
		&model.Category{},
	}
}

// MigrateContent 迁移内容域实体。
func MigrateContent(db *gorm.DB) error {
	return db.AutoMigrate(ContentModels()...)
}

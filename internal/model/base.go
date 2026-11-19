// Package model 存放全部 GORM 实体。
//
// 约定：
//   - 表名/列名统一 snake_case，与原 TypeORM 数据库零迁移对齐（见 docs/03 第七节）；
//   - 所有实体内嵌 BaseModel（ID/CreatedAt/UpdatedAt/DeletedAt 软删）；
//   - 实体按业务域拆文件：user.go、article.go 等（后续批次补充）。
package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/plugin/soft_delete"
)

// BaseModel 全实体公共字段。
//
// ID 采用 UUID 字符串（string(36)），与原 TypeORM 侧 uuid 主键语义对齐；
// DeletedAt 使用 soft_delete 插件的索引列，语义同 TypeORM 的 @DeleteDateColumn。
type BaseModel struct {
	ID        string               `gorm:"type:char(36);primaryKey" json:"id"`
	CreatedAt time.Time            `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time            `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt soft_delete.DeletedAt `gorm:"index" json:"-"`
}

// BeforeCreate 落库前自动生成 UUID 主键。
func (b *BaseModel) BeforeCreate(tx *gorm.DB) error {
	if b.ID == "" {
		b.ID = uuid.NewString()
	}
	return nil
}

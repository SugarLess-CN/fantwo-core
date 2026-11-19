package model

// Sample 骨架示例实体：仅用于验证 AutoMigrate / BaseModel / 软删链路，
// 后续业务实体落地后可删除。表名 samples。
type Sample struct {
	BaseModel
	Name  string `gorm:"type:varchar(128);not null" json:"name"`
	Remark string `gorm:"type:varchar(255)" json:"remark"`
}

// TableName 显式指定表名（snake_case，对齐 TypeORM 命名策略）。
func (Sample) TableName() string { return "samples" }

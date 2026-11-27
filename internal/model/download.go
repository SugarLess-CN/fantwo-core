// 下载资源表（对齐原 NestJS download.entity.ts）。
//
// 原版是**独立表 + @OneToMany**（article.downloads），不是 jsonb 字段。
// 我们此前只在 dto 里声明了 ArticleDownloadRequest，从未建表，导致
// ArticleDto.Downloads 永远返回 null —— 迁移缺口（docs/26 §四 缺口 1）。
//
// 三个设计要点：
//  1. visible_without_permission：文章设了门槛（付费/会员/关注）时，
//     该下载资源是否仍对无权限用户可见 —— 对齐原版 default false。
//  2. password 与 extraction_code 是两个不同字段：
//     password 是网盘的「提取密码」，extraction_code 是「提取码」，都可不填。
//  3. 文章删除时级联删除（GORM 用 Constraint:OnDelete:CASCADE）。
package model

// 下载类型（对齐原 DownloadType 枚举，10 个取值，全小写）。
const (
	DownloadTypeBaidu    = "baidu"    // 百度网盘
	DownloadTypeOneDrive = "onedrive" // OneDrive
	DownloadTypeGoogle   = "google"   // 谷歌网盘
	DownloadTypeQuark    = "quark"    // 夸克网盘
	DownloadTypeAliyun   = "aliyun"   // 阿里云
	DownloadTypeDropbox  = "dropbox"  // Dropbox
	DownloadTypeDirect   = "direct"   // 直链下载
	DownloadTypeLanzou   = "lanzou"   // 蓝奏云
	DownloadTypeMega     = "mega"     // Mega
	DownloadTypeOther    = "other"    // 其他
)

// Download 下载资源（表 downloads）。
type Download struct {
	BaseModel

	// ArticleID 所属文章（外键，文章删除时级联删除）。
	ArticleID string `gorm:"column:article_id;type:varchar(36);not null;index:idx_downloads_article" json:"article_id"`

	// Type 下载类型，取值见 DownloadType* 常量。
	Type string `gorm:"column:type;type:varchar(16);not null" json:"type"`

	// URL 下载链接。
	URL string `gorm:"column:url;type:text;not null" json:"url"`

	// Password 提取密码（网盘分享密码），可空。
	Password *string `gorm:"column:password;type:varchar(64)" json:"-"`

	// ExtractionCode 提取码，可空。
	//
	// 与 Password 的差别：password 是「访问网盘分享需要的密码」，
	// extraction_code 是「下载文件时的提取码」，两者可以同时存在。
	ExtractionCode *string `gorm:"column:extraction_code;type:varchar(64)" json:"-"`

	// VisibleWithoutPermission 文章设了门槛（付费/会员/关注）时，
	// 该下载资源是否仍对无权限用户可见。默认 false（对齐原版）。
	VisibleWithoutPermission bool `gorm:"column:visible_without_permission;not null;default:false" json:"visible_without_permission"`

	// Sort 展示顺序（原版无此字段，我们按数组顺序落库时需要它）。
	Sort int `gorm:"column:sort;not null;default:0" json:"sort"`
}

// TableName 表名（对齐原 TypeORM downloads）。
func (Download) TableName() string { return "downloads" }

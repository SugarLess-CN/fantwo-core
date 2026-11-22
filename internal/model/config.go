// 基础设施域实体与常量：系统配置表（config）。
//
// 对齐原 TypeORM 实体 tmp/picart/src/modules/config/entities/config.entity.ts。
// 配置按字符串键值存储，value 为 text（数字/布尔/JSON 均由 type 字段声明解释方式），
// group 用于分组批量读写，public 标记是否可被匿名接口 /config/public 读取。
package model

// 配置值类型（原 config.type，默认 string）。
const (
	ConfigTypeString  = "string"
	ConfigTypeNumber  = "number"
	ConfigTypeBoolean = "boolean"
	ConfigTypeJSON    = "json"
)

// 配置分组（原 config.group，默认 system）。
const (
	ConfigGroupSystem        = "system"
	ConfigGroupSite          = "site"
	ConfigGroupUpload        = "upload"
	ConfigGroupAdvertisement = "advertisement"
	ConfigGroupPayment       = "payment"
	ConfigGroupAudit         = "audit"
	ConfigGroupSearch        = "search"
	ConfigGroupInvite        = "invite"
	ConfigGroupArticle       = "article"
)

// 敏感词配置键：敏感词以 JSON 数组字符串存在该 key 下（对齐原 getSensitiveWords）。
const ConfigKeySensitiveWords = "sensitive_words"

// 上传限制类配置 key（后台可运行期调整，见 config_service 的 DB 优先读取）。
const (
	ConfigKeyUploadMaxImageSize = "upload_max_image_size"
	ConfigKeyUploadMaxVideoSize = "upload_max_video_size"
	ConfigKeyUploadMaxAudioSize = "upload_max_audio_size"
	ConfigKeyUploadMaxOtherSize = "upload_max_other_size"
	ConfigKeyUploadMaxFileCount = "upload_max_file_count"
	ConfigKeyUploadCompression  = "upload_compression_enabled"
	ConfigKeySearchEnabled      = "search_enabled"
)

// 图片审核开关（原 upload.service 读取的 content_audit_image_enabled）。
const ConfigKeyAuditImageEnabled = "content_audit_image_enabled"

// Config 系统配置表（原 config 表）。
type Config struct {
	BaseModel

	Key         string `gorm:"column:key;type:varchar(128);uniqueIndex;not null" json:"key"`
	Value       string `gorm:"column:value;type:text" json:"value"`
	Description string `gorm:"column:description;type:varchar(255)" json:"description"`
	Type        string `gorm:"column:type;type:varchar(16);not null;default:string" json:"type"`
	Group       string `gorm:"column:group;type:varchar(64);not null;default:system;index:idx_config_group" json:"group"`
	Public      bool   `gorm:"column:public;not null;default:false" json:"public"`
}

// TableName 表名（对齐原 TypeORM config 表）。
func (Config) TableName() string { return "config" }

// ConfigSeed 一条默认配置种子，用于首次启动初始化（原 config.service.initializeDatabase）。
type ConfigSeed struct {
	Key         string
	Value       string
	Description string
	Type        string
	Group       string
	Public      bool
}

// DefaultConfigSeeds 默认配置种子（核心子集，按原 initializeDatabase 的 key 命名平移）。
//
// 取舍说明：原文件 initializeDatabase 内联约 700 行业务配置（含支付密钥、邀请规则等近百项），
// 支付/邀请/审核的细项由对应业务域按需读取，本表只落「基础设施域自有 + 公开站点信息 +
// 广告位 + 上传限制 + 敏感词」这些需要由 /config 系列端点直接暴露的项。
func DefaultConfigSeeds() []ConfigSeed {
	return []ConfigSeed{
		// 站点信息（公开）
		{"site_name", "PicArt", "站点名称", ConfigTypeString, ConfigGroupSite, true},
		{"site_description", "PicArt 图片社区", "站点描述", ConfigTypeString, ConfigGroupSite, true},
		{"site_keywords", "图片,社区,壁纸,插画", "站点关键词", ConfigTypeString, ConfigGroupSite, true},
		{"site_logo", "", "站点 Logo URL", ConfigTypeString, ConfigGroupSite, true},
		{"site_favicon", "", "站点 favicon URL", ConfigTypeString, ConfigGroupSite, true},
		{"site_icp", "", "ICP 备案号", ConfigTypeString, ConfigGroupSite, true},
		{"site_police_record", "", "公安备案号", ConfigTypeString, ConfigGroupSite, true},
		{"site_mail", "", "联系邮箱", ConfigTypeString, ConfigGroupSite, true},
		{"site_contact", "", "联系方式", ConfigTypeString, ConfigGroupSite, true},
		{"site_copyright", "", "版权信息", ConfigTypeString, ConfigGroupSite, true},

		// 上传限制（公开：前端预校验需要）
		{"upload_max_image_size", "20", "图片大小上限（MB）", ConfigTypeNumber, ConfigGroupUpload, true},
		{"upload_max_video_size", "500", "视频大小上限（MB）", ConfigTypeNumber, ConfigGroupUpload, true},
		{"upload_max_audio_size", "50", "音频大小上限（MB）", ConfigTypeNumber, ConfigGroupUpload, true},
		{"upload_max_other_size", "100", "其它文件大小上限（MB）", ConfigTypeNumber, ConfigGroupUpload, true},
		{"upload_max_file_count", "10", "单次上传文件数上限", ConfigTypeNumber, ConfigGroupUpload, true},
		{"upload_compression_enabled", "true", "图片压缩开关", ConfigTypeBoolean, ConfigGroupUpload, false},
		{"upload_watermark_enabled", "true", "图片水印开关", ConfigTypeBoolean, ConfigGroupUpload, false},
		{"upload_watermark_text_template", "{nickname}", "水印文字模板（占位符：{nickname} {username} {site}）", ConfigTypeString, ConfigGroupUpload, false},
		{"upload_watermark_position", "bottom-right", "水印位置：bottom-right/bottom-left/top-right/top-left/center", ConfigTypeString, ConfigGroupUpload, false},
		{"upload_watermark_opacity_percent", "55", "水印透明度（0-100）", ConfigTypeNumber, ConfigGroupUpload, false},
		{"upload_watermark_font_size", "0", "水印字号（0=按图片短边自适应）", ConfigTypeNumber, ConfigGroupUpload, false},

		// 广告位（公开）
		{"advertisement_enabled", "false", "广告总开关", ConfigTypeBoolean, ConfigGroupAdvertisement, true},
		{"advertisement_home_banner", "", "首页横幅广告图 URL", ConfigTypeString, ConfigGroupAdvertisement, true},
		{"advertisement_home_banner_link", "", "首页横幅跳转链接", ConfigTypeString, ConfigGroupAdvertisement, true},
		{"advertisement_article_top", "", "文章页顶部广告图 URL", ConfigTypeString, ConfigGroupAdvertisement, true},
		{"advertisement_article_top_link", "", "文章页顶部广告跳转链接", ConfigTypeString, ConfigGroupAdvertisement, true},
		{"advertisement_article_bottom", "", "文章页底部广告图 URL", ConfigTypeString, ConfigGroupAdvertisement, true},
		{"advertisement_article_bottom_link", "", "文章页底部广告跳转链接", ConfigTypeString, ConfigGroupAdvertisement, true},
		{"advertisement_article_sidebar", "", "文章页侧边栏广告图 URL", ConfigTypeString, ConfigGroupAdvertisement, true},
		{"advertisement_article_sidebar_link", "", "文章页侧边栏广告跳转链接", ConfigTypeString, ConfigGroupAdvertisement, true},
		{"advertisement_global_notice", "", "全站公告富文本", ConfigTypeString, ConfigGroupAdvertisement, true},

		// 内容审核开关（非公开，供 upload/article 域读取）
		{ConfigKeyAuditImageEnabled, "false", "图片内容审核开关", ConfigTypeBoolean, ConfigGroupAudit, false},
		{"content_audit_text_enabled", "false", "文本内容审核开关", ConfigTypeBoolean, ConfigGroupAudit, false},

		// 搜索
		{"search_enabled", "true", "搜索功能开关", ConfigTypeBoolean, ConfigGroupSearch, true},

		// 敏感词（非公开）
		{ConfigKeySensitiveWords, "[]", "敏感词列表（JSON 数组）", ConfigTypeJSON, ConfigGroupSystem, false},
	}
}

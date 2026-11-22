// 基础设施域实体与常量：系统配置表（config）。
//
// 对齐原 TypeORM 实体 tmp/picart/src/modules/config/entities/config.entity.ts。
// 配置按字符串键值存储，value 为 text（数字/布尔/JSON 均由 type 字段声明解释方式），
// group 用于分组批量读写，public 标记是否可被匿名接口 /config/public 读取。
package model

import "strings"

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
	ConfigGroupStorage       = "storage"
	ConfigGroupCDN           = "cdn"
)

// 敏感词配置键：敏感词以 JSON 数组字符串存在该 key 下（对齐原 getSensitiveWords）。
const ConfigKeySensitiveWords = "sensitive_words"

// 上传限制 / 水印类配置 key（后台可运行期调整，见 config_service 的 DB 优先读取）。
const (
	ConfigKeyUploadMaxImageSize            = "upload_max_image_size"
	ConfigKeyUploadMaxVideoSize            = "upload_max_video_size"
	ConfigKeyUploadMaxAudioSize            = "upload_max_audio_size"
	ConfigKeyUploadMaxOtherSize            = "upload_max_other_size"
	ConfigKeyUploadMaxFileCount            = "upload_max_file_count"
	ConfigKeyUploadCompression             = "upload_compression_enabled"
	ConfigKeyUploadWatermarkEnabled        = "upload_watermark_enabled"
	ConfigKeyUploadWatermarkTextTemplate   = "upload_watermark_text_template"
	ConfigKeyUploadWatermarkPosition       = "upload_watermark_position"
	ConfigKeyUploadWatermarkOpacityPercent = "upload_watermark_opacity_percent"
	ConfigKeyUploadWatermarkFontSize       = "upload_watermark_font_size"
	ConfigKeySearchEnabled                 = "search_enabled"
)

// 存储 / S3 / CDN / 搜索连接类配置 key（后台可运行期调整，DB 优先、env 兜底）。
//
// 注意：storage_driver 不在此列，也**没有 key 常量** —— 它是启动期配置：minio client
// 在启动期按它选择本地/对象存储实现，改动必须重启，故不纳入运行期可调范围（也不写种子）。
//
// 注意：s3_* 这 7 个 key 目前**不写种子** —— minio client 在启动期创建，改了必须重启，
// 与 storage.driver 同类，不纳入「运行期可调」。库里留一条永远不生效的假配置比不迁更危险
// （运维改了以为生效，实际对象仍写旧桶）。保留常量是为了将来做
// 「配置签名变化 → 惰性重建 minio client」（对齐 Searcher 的做法）时直接启用。
const (
	ConfigKeyStorageLocalDir      = "storage_local_dir"
	ConfigKeyUploadThumbnailSpecs = "upload_thumbnail_specs"
	ConfigKeyS3Endpoint           = "s3_endpoint"
	ConfigKeyS3AccessKey          = "s3_access_key"
	ConfigKeyS3SecretKey          = "s3_secret_key"
	ConfigKeyS3Bucket             = "s3_bucket"
	ConfigKeyS3Region             = "s3_region"
	ConfigKeyS3UseSSL             = "s3_use_ssl"
	ConfigKeyS3ForcePathStyle     = "s3_force_path_style"
	ConfigKeyCDNBaseURL           = "cdn_base_url"
	ConfigKeySearchAddresses      = "search_addresses"
	ConfigKeySearchUsername       = "search_username"
	ConfigKeySearchPassword       = "search_password"
	ConfigKeySearchArticlesIndex  = "search_articles_index"
)

// 审核类配置 key（名字对齐原仓库 content-audit.service.ts 的 getCachedConfig，
// 读取点见 internal/service/audit_service.go 的 parseAuditConfig）。
//
// 注意：场景开关（*_enabled）默认 false，且 content_audit_provider 保持 none 时
// AuditConfig.sceneEnabled 会整段短路 —— 只有把 provider 改成真实厂商后开关才有意义。
const (
	// 行为 / 场景开关
	ConfigKeyAuditProvider       = "content_audit_provider"
	ConfigKeyAuditAutoBlock      = "content_audit_auto_block"
	ConfigKeyAuditReviewMode     = "content_audit_review_mode"
	ConfigKeyAuditSensitivity    = "content_audit_sensitivity"
	ConfigKeyAuditCommentEnabled = "content_audit_comment_enabled"
	ConfigKeyAuditAvatarEnabled  = "content_audit_avatar_enabled"
	ConfigKeyAuditImageEnabled   = "content_audit_image_enabled"
	ConfigKeyAuditArticleEnabled = "content_audit_article_enabled"

	// provider 凭据（敏感项：管理端返回打码。虽命名兜底也能命中 secret / access_key，
	// 但仍显式列入 ConfigSensitiveKeys —— 白名单才是权威口径，兜底只是保险）
	ConfigKeyTencentSecretID       = "tencent_secret_id"
	ConfigKeyTencentSecretKey      = "tencent_secret_key"
	ConfigKeyTencentRegion         = "tencent_region"
	ConfigKeyTencentTextBizType    = "tencent_text_biz_type"
	ConfigKeyTencentImageBizType   = "tencent_image_biz_type"
	ConfigKeyAliyunAccessKeyID     = "aliyun_access_key_id"
	ConfigKeyAliyunAccessKeySecret = "aliyun_access_key_secret"
	ConfigKeyAliyunRegion          = "aliyun_region"
)

// ConfigStartupOnlyKeys 启动期配置 key：改了必须重启才可能生效（当前实际不生效）。
//
// 这些 key 不进 DefaultConfigSeeds（避免库里出现永不生效的假配置），
// 同时管理端的 Create / UpdateOne 应拒绝写入 —— 详见 service 层对 IsStartupOnlyConfigKey 的调用点。
// 保留 key 常量是为了将来做「配置签名变化 → 惰性重建 client」时直接启用。
//
// storage_driver 这里用字符串字面量：它**没有 key 常量**（见上方常量块注释），
// 作为启动期配置从不写种子、也从不进运行期可调范围。
var ConfigStartupOnlyKeys = []string{
	"storage_driver", // minio client 类型选择（local / s3），启动期决定
	ConfigKeyS3Endpoint,
	ConfigKeyS3Bucket,
	ConfigKeyS3Region,
	ConfigKeyS3AccessKey,
	ConfigKeyS3SecretKey,
	ConfigKeyS3UseSSL,
	ConfigKeyS3ForcePathStyle,
}

// IsStartupOnlyConfigKey 判断 key 是否属于启动期配置（不允许经管理端创建 / 改写）。
//
// 与 IsSensitiveConfigKey 是两件不同的事，**不要混用、也不要在这里加子串兜底**：
// 敏感判定管「返回时打不打码」（取向宁可多打码），启动期判定管「能不能写库」
// （必须精确 —— 误判会让运维明明该改的配置被拒）。二者交集只有 S3 的
// access_key / secret_key（既是启动期配置，又要在管理端打码）。
func IsStartupOnlyConfigKey(key string) bool {
	for _, k := range ConfigStartupOnlyKeys {
		if key == k {
			return true
		}
	}
	return false
}

// ConfigSensitiveKeys 需要在管理端打码返回的配置 key 白名单。
// 加新密钥项时在这里补一条，不要在 service/handler 里各自硬编码字符串。
var ConfigSensitiveKeys = []string{
	// S3（启动期配置：历史数据 + 将来启用都要打码）
	ConfigKeyS3AccessKey,
	ConfigKeyS3SecretKey,
	// Elasticsearch
	ConfigKeySearchPassword,
	// 审核 provider 凭据：四项全部打码。
	// 注意 *ID / *KeyId 虽然语义上只是标识，但凭据类一律打码（不区分 id 与 secret，
	// 避免将来有人把 secret 错填进 id 字段就泄露）。
	ConfigKeyTencentSecretID,
	ConfigKeyTencentSecretKey,
	ConfigKeyAliyunAccessKeyID,
	ConfigKeyAliyunAccessKeySecret,
}

// sensitiveConfigKeywords IsSensitiveConfigKey 的命名兜底关键词（小写，子串匹配）。
// 取向：宁可多打码（管理端把疑似密钥项一律打码，运维仍可改值），也不漏掉新增的密钥项。
var sensitiveConfigKeywords = []string{
	"password",
	"secret",
	"access_key",
	"_token",
	"private_key",
}

// IsSensitiveConfigKey 判断配置项是否需要在管理端打码返回。
//
// ① 白名单（ConfigSensitiveKeys）命中直接返回 true；
// ② 命名兜底（大小写不敏感）：key 含 password / secret / access_key / _token / private_key
// 任一子串即视为敏感，防止以后新增的密钥项漏打码。
//
// 兜底边界（实测）：refresh_token ✅ 命中（含 _token）；tokenizer ❌ 不命中（token 前无下划线）；
// sensitive_words ❌ 不命中（sensitive 不在关键词表里）；search_username ❌ 不命中（用户名不是密钥）。
func IsSensitiveConfigKey(key string) bool {
	for _, k := range ConfigSensitiveKeys {
		if key == k {
			return true
		}
	}
	lower := strings.ToLower(key)
	for _, kw := range sensitiveConfigKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

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
// 广告位 + 上传限制 + 本地目录 / 缩略图规格 / CDN 域名 / 搜索连接参数 + 敏感词」
// 这些需要由 /config 系列端点直接暴露的项。
//
// 纳入运行期可调（改 DB 不重启即生效）：本地目录 / 缩略图规格 / CDN 域名 / 搜索连接参数
// （其中敏感项由 handler 层打码）。
// 不纳入（启动期配置，改了必须重启）：S3 七项（endpoint / bucket / access_key / secret_key /
// region / use_ssl / force_path_style）与 storage_driver —— 均绑定启动期创建的 minio client，
// 故**不写种子**：库里留一条永远不生效的假配置比不迁更危险（运维改了以为生效，实际写旧桶）。
// 所有种子值必须与 internal/config/infra.go 的 SetDefault 默认值保持一致。
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
		{ConfigKeyUploadWatermarkEnabled, "true", "图片水印开关", ConfigTypeBoolean, ConfigGroupUpload, false},
		{ConfigKeyUploadWatermarkTextTemplate, "{nickname}", "水印文字模板（占位符：{nickname} {username} {site}）", ConfigTypeString, ConfigGroupUpload, false},
		{ConfigKeyUploadWatermarkPosition, "bottom-right", "水印位置：bottom-right/bottom-left/top-right/top-left/center", ConfigTypeString, ConfigGroupUpload, false},
		{ConfigKeyUploadWatermarkOpacityPercent, "55", "水印透明度（0-100）", ConfigTypeNumber, ConfigGroupUpload, false},
		{ConfigKeyUploadWatermarkFontSize, "0", "水印字号（0=按图片短边自适应）", ConfigTypeNumber, ConfigGroupUpload, false},
		{"upload_thumbnail_specs", `[{"name":"thumb","size":200},{"name":"small","size":480},{"name":"medium","size":1024},{"name":"large","size":1920}]`, "缩略图规格（JSON 数组：name/size）", ConfigTypeJSON, ConfigGroupUpload, false},

		// 存储（非公开）：仅本地存储根目录。S3 七项为启动期配置，不写种子（见上方 key 常量块注释）。
		{"storage_local_dir", "./uploads", "本地存储根目录", ConfigTypeString, ConfigGroupStorage, false},

		// CDN（公开：前端拼接资源地址需要）
		{"cdn_base_url", "", "CDN 加速域名前缀（如 https://cdn.example.com）", ConfigTypeString, ConfigGroupCDN, true},

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

		// 内容审核（非公开，供 audit_service 读取；key 名对齐原 content-audit.service.ts）
		{ConfigKeyAuditImageEnabled, "false", "图片内容审核开关", ConfigTypeBoolean, ConfigGroupAudit, false},
		{ConfigKeyAuditProvider, "none", "审核 provider：none / tencent / aliyun", ConfigTypeString, ConfigGroupAudit, false},
		{ConfigKeyAuditAutoBlock, "true", "命中违禁内容时自动拦截（false=仅标记待复审）", ConfigTypeBoolean, ConfigGroupAudit, false},
		{ConfigKeyAuditReviewMode, "auto", "复审模式：auto / manual", ConfigTypeString, ConfigGroupAudit, false},
		{ConfigKeyAuditSensitivity, "medium", "审核灵敏度：low / medium / high", ConfigTypeString, ConfigGroupAudit, false},
		{ConfigKeyAuditCommentEnabled, "false", "评论内容审核开关", ConfigTypeBoolean, ConfigGroupAudit, false},
		{ConfigKeyAuditAvatarEnabled, "false", "头像内容审核开关", ConfigTypeBoolean, ConfigGroupAudit, false},
		{ConfigKeyAuditArticleEnabled, "false", "文章内容审核开关", ConfigTypeBoolean, ConfigGroupAudit, false},

		// 审核 provider 凭据（非公开；敏感项由管理端 handler 打码返回）
		{ConfigKeyTencentSecretID, "", "腾讯云 SecretId（敏感：管理端返回打码）", ConfigTypeString, ConfigGroupAudit, false},
		{ConfigKeyTencentSecretKey, "", "腾讯云 SecretKey（敏感：管理端返回打码）", ConfigTypeString, ConfigGroupAudit, false},
		{ConfigKeyTencentRegion, "ap-beijing", "腾讯云地域", ConfigTypeString, ConfigGroupAudit, false},
		{ConfigKeyTencentTextBizType, "", "腾讯云文本审核 BizType", ConfigTypeString, ConfigGroupAudit, false},
		{ConfigKeyTencentImageBizType, "", "腾讯云图片审核 BizType", ConfigTypeString, ConfigGroupAudit, false},
		{ConfigKeyAliyunAccessKeyID, "", "阿里云 AccessKeyId（敏感：管理端返回打码）", ConfigTypeString, ConfigGroupAudit, false},
		{ConfigKeyAliyunAccessKeySecret, "", "阿里云 AccessKeySecret（敏感：管理端返回打码）", ConfigTypeString, ConfigGroupAudit, false},
		{ConfigKeyAliyunRegion, "cn-beijing", "阿里云地域", ConfigTypeString, ConfigGroupAudit, false},

		// 搜索
		{"search_enabled", "true", "搜索功能开关", ConfigTypeBoolean, ConfigGroupSearch, true},
		{"search_addresses", "[]", "Elasticsearch 地址列表（JSON 数组）", ConfigTypeJSON, ConfigGroupSearch, false},
		{"search_username", "", "Elasticsearch 用户名", ConfigTypeString, ConfigGroupSearch, false},
		{"search_password", "", "Elasticsearch 密码（敏感：管理端返回打码）", ConfigTypeString, ConfigGroupSearch, false},
		{"search_articles_index", "picart_articles", "文章索引名", ConfigTypeString, ConfigGroupSearch, false},

		// 敏感词（非公开）
		{ConfigKeySensitiveWords, "[]", "敏感词列表（JSON 数组）", ConfigTypeJSON, ConfigGroupSystem, false},
	}
}

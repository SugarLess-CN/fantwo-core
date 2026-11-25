package dto

import "time"

// 本文件覆盖 config 域（系统配置 / 公开配置 / 广告配置 / 敏感词）的对外请求与响应结构。
//
// 字段来源（逐条可追，不是想象）：
//   - ConfigDto            ← internal/service/infra/config_service.go 的 ConfigView
//     （运行时唯一组装点 toView；BaseModel 的 deletedAt 是 json:"-" 本来就不下发）
//   - ConfigRequest        ← 同文件的 ConfigInput（handler/config.go 直接绑它）
//   - PublicConfigsDto     ← ConfigService.PublicConfigs（{group:{key:value}} 聚合）
//   - AdvertisementConfigDto ← ConfigService.AdvertisementConfig（11 个扁平字段 + raw）
//   - SensitiveWord*Dto    ← handler/config.go 各敏感词端点的 gin.H 字面量键
//
// 空值规则（docs/21-JSON序列化规范.md）：
//   - 字符串 → 值类型，空时 ""（不用 *string）
//   - 数组   → 非 nil 切片，空时 []（nil 切片会被 json.Marshal 输出 null）
//   - 时间   → 本域的时间**不是可空字段**：id/createdAt/updatedAt 由 DB 必然给出，
//     所以用 time.Time 值类型（不是 *time.Time + omitempty）—— 这是 docs/21 第四节
//     「允许的例外」的适用场景（字段必然有值，不存在「空」的表示问题）。
//
// ⚠️ 打码语义归 ConfigService，DTO 只声明形状：
// 敏感项（s3_access_key / s3_secret_key / search_password 等）的 value 恒为 "***"
// （库中值为空时为 ""），并附 hasValue / masked / startupOnly 三个标记。
// 本文件**不得**新增任何能绕过打码的字段（例如明文 value 的别名），
// 判定逻辑的唯一事实来源仍是 model.IsSensitiveConfigKey。

// ConfigDto 管理端配置项视图（GET /config、/config/group/{g}、/config/key/{k}、/config/{id}）。
//
// 12 个字段与改前 model.Config 的对外 JSON 逐字对齐（见 TestConfigViewJSONFields）：
// 打码只**新增** hasValue / masked / startupOnly，不删、不改名任何既有字段。
type ConfigDto struct {
	ID string `json:"id" example:"8f2a1c34-5b6d-4e7f-9a0b-1c2d3e4f5a6b"`

	Key         string `json:"key" example:"s3_secret_key"`
	Value       string `json:"value" example:"***"`
	Description string `json:"description" example:"S3 Secret Key"`
	Type        string `json:"type" enums:"string,number,boolean,json" example:"string"`
	Group       string `json:"group" example:"storage"`
	Public      bool   `json:"public" example:"false"`

	// HasValue 后端是否已配置（DB 值非空）。敏感项 value 被打码后仍能靠它判断「有没有配」。
	HasValue bool `json:"hasValue" example:"true"`
	// Masked 该项是否属敏感项（读时打码 + 写时 "***" 视为「保持原值」）。
	Masked bool `json:"masked" example:"true"`
	// StartupOnly 启动期配置：改了必须重启才生效，管理端应置灰只读（四个写入口也会硬拒）。
	// 与 Masked 互不排斥 —— s3_access_key 两者同为 true（写路径拒、读路径仍打码）。
	StartupOnly bool `json:"startupOnly" example:"true"`

	CreatedAt time.Time `json:"createdAt" example:"2026-11-23T10:00:00Z"`
	UpdatedAt time.Time `json:"updatedAt" example:"2026-11-23T10:00:00Z"`
}

// ConfigRequest 配置创建 / 更新请求体（POST /config、PATCH /config、PATCH /config/group/{g}）。
//
// Public 保持 *bool：请求体的「三态」是真实需求（不传 = 不改，false = 显式置否），
// 与响应体的「只有有值/无值两态」不同（docs/21 只约束响应）。
//
// 敏感项写语义（由 buildUpdates 实现，DTO 不改）：value 传 "" 或 "***" 都表示保持原值，
// 前端可把读到的对象原样回传而不清空密钥。
type ConfigRequest struct {
	Key         string `json:"key" example:"site_name"`
	Value       string `json:"value" example:"PicArt"`
	Description string `json:"description" example:"站点名称"`
	Type        string `json:"type" enums:"string,number,boolean,json" example:"string"`
	Group       string `json:"group" example:"site"`
	Public      *bool  `json:"public" example:"true"`
}

// ConfigUpdateResultDto 批量更新结果（PATCH /config、PATCH /config/group/{g}）。
//
// updated 是**真实写库条数**；skipped 是「未命中的 key + 敏感项空值/*** 占位符」条数。
// 两者语义不同，前端不要用 requests - updated 反推 skipped。
type ConfigUpdateResultDto struct {
	Updated int `json:"updated" example:"3"`
	Skipped int `json:"skipped" example:"1"`
}

// PublicConfigsDto 公开配置聚合（GET /config/public，NoAuth）。
//
// 形态是 {group: {key: value}} 两层 map，便于前端按分组取用。值按配置的 type 还原
// （number→float64 / boolean→bool / json→解析后的结构 / string→原样）。
//
// 为什么用 map 而不是固定字段结构：分组与键随运营配置变化（site / upload / cdn /
// advertisement / search …），写死字段必然与库脱节。swag 会把它声明为
// object + additionalProperties，语义准确。
//
// 安全兜底（在 service.aggregatePublic，不在 DTO）：敏感项与敏感词表一律不下发，
// 即便被误标 public 也不会经匿名接口泄漏。
type PublicConfigsDto map[string]map[string]interface{}

// AdvertisementConfigDto 广告位配置（GET /config/advertisement，NoAuth）。
//
// 前 11 个字段是前端按固定字段名读取的扁平视图（对齐原 getAdvertisementConfig）；
// raw 是 group=advertisement 的原始键值全集，供前端读取新增配置项时兜底。
//
// 空值规则：扁平字段缺失时是 "" / false（不是 null）—— 组装层已做类型回落，
// 否则 map 取不存在的键会得到 nil，JSON 里就是 null。
type AdvertisementConfigDto struct {
	Enabled            bool   `json:"enabled" example:"false"`
	HomeBanner         string `json:"homeBanner" example:"https://cdn.example.com/banner.png"`
	HomeBannerLink     string `json:"homeBannerLink" example:"https://example.com/promo"`
	ArticleTop         string `json:"articleTop" example:""`
	ArticleTopLink     string `json:"articleTopLink" example:""`
	ArticleBottom      string `json:"articleBottom" example:""`
	ArticleBottomLink  string `json:"articleBottomLink" example:""`
	ArticleSidebar     string `json:"articleSidebar" example:""`
	ArticleSidebarLink string `json:"articleSidebarLink" example:""`
	GlobalNotice       string `json:"globalNotice" example:""`

	// Raw 广告组的原始键值（含上面 11 个字段的源 key）。无配置时为 {}（不是 null）。
	Raw map[string]interface{} `json:"raw"`
}

// SensitiveWordListDto 敏感词列表（GET /config/sensitive-words）。
//
// 响应保留 {success, data, total} 原形态，便于前端零改动对接（这是既有契约，不改）。
// Words 空时 []（不是 null）。
type SensitiveWordListDto struct {
	Success bool     `json:"success" example:"true"`
	Words   []string `json:"data"`
	Total   int      `json:"total" example:"128"`
}

// SensitiveWordActionDto 单个敏感词增删结果（POST / DELETE /config/sensitive-words）。
//
// 成功时 message 走 i18n（ConfigSensitiveAdded / ConfigSensitiveRemoved），此处只声明 data。
type SensitiveWordActionDto struct {
	Success bool `json:"success" example:"true"`
}

// SensitiveWordBatchResultDto 批量增删结果（POST / DELETE /config/sensitive-words/batch）。
//
// Added / Removed 是**实际生效条数**（已存在或不存在的不计数），与请求条数可能不同。
// Message 是中英文案（走 i18n Tf），作为 data 的一部分保留原契约。
type SensitiveWordBatchResultDto struct {
	Success bool   `json:"success" example:"true"`
	Message string `json:"message" example:"Imported 10 sensitive words"`
	// Added 批量新增实际生效条数（批量删除端点为 0，改用 Removed）。
	Added int `json:"added" example:"8"`
	// Removed 批量删除实际生效条数（批量新增端点为 0，改用 Added）。
	Removed int `json:"removed" example:"0"`
}

// SensitiveWordCacheClearDto 敏感词缓存清理结果（POST /config/sensitive-words/clear-cache）。
//
// Total 是清理后重建的前缀树词条数（用于确认词表确实重新加载）。
type SensitiveWordCacheClearDto struct {
	Success bool `json:"success" example:"true"`
	Total   int  `json:"total" example:"128"`
}

// SensitiveWordRequest 单个敏感词增删请求体（POST / DELETE /config/sensitive-words）。
//
// 原实现是 handler 内的匿名 struct，只取 word；此处具名以便 swag 复用。
type SensitiveWordRequest struct {
	Word string `json:"word" binding:"required" example:"违禁词"`
}

// SensitiveWordBatchRequest 批量敏感词增删请求体（POST / DELETE /config/sensitive-words/batch）。
type SensitiveWordBatchRequest struct {
	Words []string `json:"words" binding:"required"`
}

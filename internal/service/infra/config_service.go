// 基础设施域：系统配置（config 表）与敏感词管理。
//
// 对齐原仓库 tmp/picart/src/modules/config/config.service.ts：
//   - 配置按 key 唯一存储，支持按 group 批量读写；
//   - public=true 的配置走匿名接口（/config/public、/config/advertisement）；
//   - 敏感词以 JSON 数组存在 config 表 key=sensitive_words，
//     内存前缀树（sensitive_trie.go）+ Redis 缓存失效（clear-cache 语义）；
//   - 首次启动写入默认配置种子（原 initializeDatabase 的核心子集，见 model/config.go）。
//
// 缓存设计：进程内 map 为一级缓存（命中零网络开销），Redis 为二级共享缓存
// （多实例部署下 refresh 后其它实例能立即失效）；Redis 不可用时退化为纯进程内缓存。
package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/config"
	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// ConfigKeySensitiveWords 敏感词配置键（供 handler 层引用，避免直接依赖 model 包）。
const ConfigKeySensitiveWords = model.ConfigKeySensitiveWords

// configCacheTTL 二级（Redis）缓存 TTL；进程内缓存无 TTL，靠显式失效。
const configCacheTTL = 10 * time.Minute

// configCachePrefix Redis key 前缀（对齐原 config 缓存命名习惯）。
const configCachePrefix = "config:cache:"

// ConfigService 系统配置与敏感词服务。
type ConfigService struct {
	db  *gorm.DB
	rdb *redis.Client

	mu    sync.RWMutex
	cache map[string]string // key -> value 进程内一级缓存
	trie  *Trie             // 敏感词前缀树

	// wordsLoaded 敏感词是否已从库加载（多个业务域各自持有 ConfigService 实例时，
	// 首次 CheckText 会懒加载，避免漏判）。
	wordsLoaded atomic.Bool
	// wordsFingerprint 当前前缀树对应的词表 JSON（与 Redis 缓存值比对判脏）。
	wordsFingerprint atomic.Value // string
	// lastWordsSync 上次同步词表的 Unix 秒（多实例惰性刷新节流）。
	lastWordsSync atomic.Int64
}

// NewConfigService 构造配置服务。
func NewConfigService(db *gorm.DB, rdb *redis.Client) *ConfigService {
	return &ConfigService{
		db:    db,
		rdb:   rdb,
		cache: make(map[string]string, 64),
		trie:  NewTrie(nil),
	}
}

// EnsureSeeds 初始化默认配置（幂等：已存在的 key 不覆盖），并加载敏感词树。
// 对齐原 config.service.onModuleInit → initializeDatabase。
func (s *ConfigService) EnsureSeeds() error {
	if s.db == nil {
		return nil
	}
	seeds := model.DefaultConfigSeeds()
	keys := make([]string, 0, len(seeds))
	for _, sd := range seeds {
		keys = append(keys, sd.Key)
	}
	var existing []string
	if err := s.db.Model(&model.Config{}).Where("key IN ?", keys).Pluck("key", &existing).Error; err != nil {
		return fmt.Errorf("查询既有配置失败: %w", err)
	}
	exists := make(map[string]struct{}, len(existing))
	for _, k := range existing {
		exists[k] = struct{}{}
	}
	var toCreate []model.Config
	for _, sd := range seeds {
		if _, ok := exists[sd.Key]; ok {
			continue
		}
		toCreate = append(toCreate, model.Config{
			Key:         sd.Key,
			Value:       sd.Value,
			Description: sd.Description,
			Type:        sd.Type,
			Group:       sd.Group,
			Public:      sd.Public,
		})
	}
	if len(toCreate) > 0 {
		if err := s.db.Create(&toCreate).Error; err != nil {
			return fmt.Errorf("写入默认配置失败: %w", err)
		}
	}
	return s.ReloadSensitiveWords()
}

// ---------- 管理端视图与敏感项打码 ----------

// maskedValue 敏感配置项在管理端的占位符。
//
// 语义是双向的：
//   - 读（GET /config 系列）：真实值非空时用该占位符代替；
//   - 写（PATCH /config 系列）：收到该占位符或空串表示「保持原值不变」，
//     前端可以原样回传读到的对象，不必为了改别的字段而清空密钥。
const maskedValue = "***"

// isSensitiveKey 判断配置项是否需要在管理端打码。
//
// 单一事实来源：判定规则统一由 model.IsSensitiveConfigKey 提供
// （白名单 model.ConfigSensitiveKeys + 命名兜底 password/secret/access_key/_token/private_key），
// 本函数只是 service 层的一层薄包装，便于包内调用与单测；**不要在此另写一份规则**，
// 否则 model 与 service 两份清单会漂移。
func isSensitiveKey(key string) bool {
	return model.IsSensitiveConfigKey(key)
}

// skipValueUpdate 判断某次写入的 value 是否应被忽略（敏感项的空值/占位符保护）。
//
// 规则：仅对敏感 key 生效——value 为空串（或纯空白）或等于打码占位符 "***" 时跳过。
// 非敏感 key 一律返回 false，写入语义保持原样（含显式写空串）。
func skipValueUpdate(key, value string) bool {
	if !isSensitiveKey(key) {
		return false
	}
	switch strings.TrimSpace(value) {
	case "", maskedValue:
		return true
	}
	return false
}

// ConfigView 管理端配置视图：敏感项 value 打码，附 hasValue 表明后端是否已配置。
//
// 字段集合必须与改前的 GORM 实体响应**保持一致**（AGENTS.md 全局决策：对齐前端契约、前端零改动）：
// model.Config 内嵌 BaseModel，其对外 JSON 为 id/createdAt/updatedAt（deletedAt 是 json:"-"），
// 因此这里必须显式带出 createdAt / updatedAt，不允许借着「改成视图」的机会删字段。
// 打码只新增 hasValue / masked 两个字段，不删除、不改名任何既有字段。
//
// startupOnly=true 的行前端应置灰只读；即便前端漏判，四个写入口也会在 service 层拒绝
// （rejectStartupOnlyKey：Create / Update / UpdateAll / UpdateOne）。
//
// startupOnly 与 masked 是两件独立的事，可以同时为 true（s3_access_key 就是）：
// 写路径先判启动期直接拒绝，读路径仍按敏感打码 —— 历史数据可能已存在于老库，读出必须遮住。
//
// 为什么是 dto 别名（而不是本包自定义结构）：结构与 dto.ConfigDto 逐字段一致
// （12 字段、同名 JSON、时间用 time.Time 值类型）。此前是两份平行定义 ——
// 注解声明 dto.ConfigDto、运行时返回本结构，是「文档一份、运行时另一份」的漂移源。
// 收归别名后 swag schema 与运行时类型就是同一个类型，不会再各改一边。
// **打码语义完全不变**：判定仍在 toView / isSensitiveKey，别名只负责形状。
type ConfigView = dto.ConfigDto

// toView 把配置实体转成管理端视图（纯函数，便于单测）。
//
// 敏感项：DB 值非空 → Value 为 "***"；DB 值为空 → Value 为空串（表示尚未配置）；
// 两种情况 HasValue 都等于「DB 值是否非空」，Masked 恒为 true。
// 非敏感项：Value 原样返回，Masked=false，HasValue 仍如实反映是否非空。
// CreatedAt / UpdatedAt 一律原样透传（前端按 updatedAt 展示「最后修改时间」需要它）。
// StartupOnly 按 model.IsStartupOnlyConfigKey 判定，与 Masked 互不排斥。
func toView(c model.Config) ConfigView {
	masked := isSensitiveKey(c.Key)
	hasValue := c.Value != ""
	value := c.Value
	if masked {
		if hasValue {
			value = maskedValue
		} else {
			value = ""
		}
	}
	return ConfigView{
		ID:          c.ID,
		Key:         c.Key,
		Value:       value,
		Description: c.Description,
		Type:        c.Type,
		Group:       c.Group,
		Public:      c.Public,
		HasValue:    hasValue,
		Masked:      masked,
		StartupOnly: model.IsStartupOnlyConfigKey(c.Key),
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}
}

// toViews 批量转换（nil 入参返回空切片，避免前端拿到 null）。
func toViews(list []model.Config) []ConfigView {
	out := make([]ConfigView, 0, len(list))
	for _, c := range list {
		out = append(out, toView(c))
	}
	return out
}

// ---------- 配置 CRUD ----------

// List 全部配置（对齐 ConfigController.findAll，权限 config:read）。
//
// 返回管理端视图：敏感项（s3_access_key / s3_secret_key / search_password 等）已打码。
func (s *ConfigService) List() ([]ConfigView, error) {
	rows, err := s.listRaw()
	if err != nil {
		return nil, err
	}
	return toViews(rows), nil
}

// listRaw 读全部配置实体（内部使用方法，保留明文 value）。
func (s *ConfigService) listRaw() ([]model.Config, error) {
	if s.db == nil {
		return nil, svcUnavailable()
	}
	var out []model.Config
	if err := s.db.Order(`"group" ASC, key ASC`).Find(&out).Error; err != nil {
		return nil, svcInternal("查询配置失败: %v", err)
	}
	return out, nil
}

// ByGroup 按分组取配置（对齐 findByGroup）。
//
// 与 List 同为管理端读路径，同样返回打码视图——否则按分组读取就能绕过打码。
func (s *ConfigService) ByGroup(group string) ([]ConfigView, error) {
	rows, err := s.byGroupRaw(group)
	if err != nil {
		return nil, err
	}
	return toViews(rows), nil
}

// byGroupRaw 按分组读实体（内部使用方法，保留明文 value）。
func (s *ConfigService) byGroupRaw(group string) ([]model.Config, error) {
	if s.db == nil {
		return nil, svcUnavailable()
	}
	if strings.TrimSpace(group) == "" {
		return nil, errcode.ErrBadRequest.WithKey(i18n.ConfigListEmpty)
	}
	var out []model.Config
	if err := s.db.Where(`"group" = ?`, group).Order(`key ASC`).Find(&out).Error; err != nil {
		return nil, svcInternal("查询分组配置失败: %v", err)
	}
	return out, nil
}

// ByID 按主键取配置（管理端视图，敏感项打码）。
func (s *ConfigService) ByID(id string) (*ConfigView, error) {
	c, err := s.getByIDRaw(id)
	if err != nil {
		return nil, err
	}
	v := toView(*c)
	return &v, nil
}

// getByIDRaw 按主键取实体（内部使用方法，含明文 value；写路径一律走它）。
func (s *ConfigService) getByIDRaw(id string) (*model.Config, error) {
	if s.db == nil {
		return nil, svcUnavailable()
	}
	var c model.Config
	if err := s.db.First(&c, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.ErrNotFound
		}
		return nil, svcInternal("查询配置失败: %v", err)
	}
	return &c, nil
}

// ByKey 按 key 取配置（管理端视图，敏感项打码）。
func (s *ConfigService) ByKey(key string) (*ConfigView, error) {
	c, err := s.getByKeyRaw(key)
	if err != nil {
		return nil, err
	}
	v := toView(*c)
	return &v, nil
}

// getByKeyRaw 按 key 取实体（内部使用方法，含明文 value）。
func (s *ConfigService) getByKeyRaw(key string) (*model.Config, error) {
	if s.db == nil {
		return nil, svcUnavailable()
	}
	var c model.Config
	if err := s.db.Where("key = ?", key).First(&c).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.ErrNotFound
		}
		return nil, svcInternal("查询配置失败: %v", err)
	}
	return &c, nil
}

// ConfigInput 创建/更新配置入参（对齐 CreateConfigDto / UpdateConfigDto）。
type ConfigInput struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Group       string `json:"group"`
	Public      *bool  `json:"public"`
}

// rejectStartupOnlyKey 启动期配置写入的统一拦截（纯函数，便于单测；返回 nil 表示放行）。
//
// 背景：S3 / storage_driver 这类配置绑定启动期创建的 client，改库不生效（必须改环境变量 + 重启）。
// 删掉种子只堵住「启动时自动写入」，POST /config、PATCH /config/:id、PATCH /config 以及
// 业务域直写 UpdateOne 仍是可手工造出「永不生效的假配置」的入口 —— 在这里统一拒绝，
// 让运维拿到明确报错，而不是写成功后再也等不到生效。
//
// 为什么是一个函数而不是各入口各写一份 if：入口有四处，规则必须只有一份，
// 否则 model 侧以后加 key 时会漏掉某个入口。规则本身（哪些 key）只在
// model.ConfigStartupOnlyKeys 里维护，这里不做任何硬编码。
//
// 空 key 直接放行：交给调用方各自的「配置 key 不能为空」去报，不在这里抢错误。
func rejectStartupOnlyKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	if !model.IsStartupOnlyConfigKey(key) {
		return nil
	}
	// 文案给运维可执行的话，而不是只说「不允许」：否则运维会以为后台坏了，
	// 转头去 SQL 直改库，绕开所有校验 —— 那比报错本身更糟。
	return errcode.ErrBadRequest.WithKeyf(i18n.ConfigStartupOnlyFix, key)
}

// Create 新建配置（key 唯一，冲突返回 409）。
//
// 返回管理端视图（与 List/ByGroup/ByKey/ByID/Update 口径一致）：创建者能看见自己刚填的值，
// 敏感项照常打码，前端只需一套解析逻辑。
// 启动期配置（S3 / storage_driver）拒绝创建 —— 见 rejectStartupOnlyKey。
func (s *ConfigService) Create(in ConfigInput) (*ConfigView, error) {
	if s.db == nil {
		return nil, svcUnavailable()
	}
	if strings.TrimSpace(in.Key) == "" {
		return nil, errcode.ErrBadRequest.WithKey(i18n.ConfigKeyRequired)
	}
	if err := rejectStartupOnlyKey(in.Key); err != nil {
		return nil, err
	}
	var cnt int64
	if err := s.db.Model(&model.Config{}).Where("key = ?", in.Key).Count(&cnt).Error; err != nil {
		return nil, svcInternal("查询配置失败: %v", err)
	}
	if cnt > 0 {
		return nil, errcode.ErrConflict.WithKeyf(i18n.ConfigKeyExists, in.Key)
	}
	c := &model.Config{
		Key:         in.Key,
		Value:       in.Value,
		Description: in.Description,
		Type:        orDefaultStr(in.Type, model.ConfigTypeString),
		Group:       orDefaultStr(in.Group, model.ConfigGroupSystem),
	}
	if in.Public != nil {
		c.Public = *in.Public
	}
	if err := s.db.Create(c).Error; err != nil {
		return nil, svcInternal("创建配置失败: %v", err)
	}
	s.invalidate(c.Key)
	return s.ByKey(c.Key)
}

// buildUpdates 由入参与现有实体推导 PATCH 需要写的列集合（纯函数，便于单测）。
//
// 「空值不覆盖」语义集中在两处：
//   - value：空串一律不写列——旧实现写成 `in.Value != "" || in.Type != ""`，
//     导致「只改 type 不改 value」时把 value 覆盖成空串，本函数顺带修掉该缺陷；
//   - 敏感 key：value 为 "***"（读接口的打码占位符）同样不写列，
//     前端把读到的对象原样回传即可，密钥保持原值。
//
// 其余字段语义不变：description / type / group 非空才写，public 非 nil 才写。
func buildUpdates(c model.Config, in ConfigInput) map[string]interface{} {
	updates := map[string]interface{}{}
	if in.Value != "" && !skipValueUpdate(c.Key, in.Value) {
		updates["value"] = in.Value
	}
	if in.Description != "" {
		updates["description"] = in.Description
	}
	if in.Type != "" {
		updates["type"] = in.Type
	}
	if in.Group != "" {
		updates["group"] = in.Group
	}
	if in.Public != nil {
		updates["public"] = *in.Public
	}
	return updates
}

// Update 按主键更新配置，返回管理端视图（敏感项打码）。
//
// 启动期配置（S3 / storage_driver）拒绝修改 —— 见 rejectStartupOnlyKey。
func (s *ConfigService) Update(id string, in ConfigInput) (*ConfigView, error) {
	c, err := s.getByIDRaw(id)
	if err != nil {
		return nil, err
	}
	if err := rejectStartupOnlyKey(c.Key); err != nil {
		return nil, err
	}
	updates := buildUpdates(*c, in)
	if len(updates) > 0 {
		if err := s.db.Model(&model.Config{}).Where("id = ?", c.ID).Updates(updates).Error; err != nil {
			return nil, svcInternal("更新配置失败: %v", err)
		}
		s.invalidate(c.Key)
		// 敏感词被逐条更新时同步重建前缀树（与 UpdateAll 行为一致）
		if c.Key == model.ConfigKeySensitiveWords {
			_ = s.ReloadSensitiveWords()
		}
	}
	return s.ByID(id)
}

// UpdateOne 按 key 更新单个值（供业务域写配置，如审核开关）。
//
// 启动期配置（S3 / storage_driver）拒绝写入 —— 含「不存在则创建」分支，
// 否则业务域误传一个 s3_* key 就会凭空造出永不生效的配置行。
func (s *ConfigService) UpdateOne(key, value string) error {
	if s.db == nil {
		return svcUnavailable()
	}
	if err := rejectStartupOnlyKey(key); err != nil {
		return err
	}
	var c model.Config
	if err := s.db.Where("key = ?", key).First(&c).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 不存在则建（带默认分组 system、非公开）
			if cerr := s.db.Create(&model.Config{Key: key, Value: value, Type: model.ConfigTypeString, Group: model.ConfigGroupSystem}).Error; cerr != nil {
				return svcInternal("创建配置失败: %v", cerr)
			}
			s.invalidate(key)
			return nil
		}
		return svcInternal("查询配置失败: %v", err)
	}
	if err := s.db.Model(&model.Config{}).Where("id = ?", c.ID).Update("value", value).Error; err != nil {
		return svcInternal("更新配置失败: %v", err)
	}
	s.invalidate(key)
	return nil
}

// UpdateAll 批量更新（body 为 [{key,value}]，对齐 updateAll）。
//
// 返回 (实际更新条数, 跳过条数, error)。跳过规则：
//   - key 不存在：静默忽略（对齐原实现），计入跳过数；
//   - 敏感 key 且 value 为空串或 "***"：不写库、不失效缓存，计入跳过数；
//   - 非敏感 key 的 value 保持原语义（含显式写入空串，不做额外拦截）。
//
// 启动期配置（S3 / storage_driver）**整批报错回滚**（不静默计入 skipped）：
// 这个入口能改到库里残留的旧 S3 行，写了也不生效，必须让调用方看见失败。
func (s *ConfigService) UpdateAll(items []ConfigInput) (int, int, error) {
	if s.db == nil {
		return 0, 0, svcUnavailable()
	}
	if len(items) == 0 {
		return 0, 0, errcode.ErrBadRequest.WithKey(i18n.ConfigListEmpty)
	}
	updated, skipped := 0, 0
	// 收集实际改动的 key，事务提交后统一失效缓存（含 Redis，多实例一致）
	changed := make([]string, 0, len(items))
	err := s.db.Transaction(func(tx *gorm.DB) error {
		for _, it := range items {
			if it.Key == "" {
				return errcode.ErrBadRequest.WithKey(i18n.ConfigKeyRequired)
			}
			if err := rejectStartupOnlyKey(it.Key); err != nil {
				return err // 事务回滚：批量里混入启动期配置整批拒绝
			}
			var c model.Config
			err := tx.Where("key = ?", it.Key).First(&c).Error
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					skipped++
					continue // 不存在的 key 跳过（对齐原实现：静默忽略）
				}
				return svcInternal("查询配置失败: %v", err)
			}
			if skipValueUpdate(c.Key, it.Value) {
				skipped++
				continue // 敏感项的空值/占位符：保持库里原值
			}
			if err := tx.Model(&model.Config{}).Where("id = ?", c.ID).Update("value", it.Value).Error; err != nil {
				return svcInternal("更新配置失败: %v", err)
			}
			updated++
			changed = append(changed, c.Key)
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	for _, key := range changed {
		s.invalidate(key)
	}
	// 敏感词被批量改写时同步重建前缀树
	for _, key := range changed {
		if key == model.ConfigKeySensitiveWords {
			_ = s.ReloadSensitiveWords()
		}
	}
	return updated, skipped, nil
}

// UpdateGroup 批量更新某分组（先校验分组存在，再逐项更新）。
//
// 跳过语义完全复用 UpdateAll（敏感项空值/占位符不覆盖）。
func (s *ConfigService) UpdateGroup(group string, items []ConfigInput) (int, int, error) {
	if s.db == nil {
		return 0, 0, svcUnavailable()
	}
	if strings.TrimSpace(group) == "" {
		return 0, 0, errcode.ErrBadRequest.WithKey(i18n.ConfigListEmpty)
	}
	var cnt int64
	if err := s.db.Model(&model.Config{}).Where(`"group" = ?`, group).Count(&cnt).Error; err != nil {
		return 0, 0, svcInternal("查询分组配置失败: %v", err)
	}
	if cnt == 0 {
		return 0, 0, errcode.ErrNotFound.WithKeyf(i18n.ConfigGroupNotFound, group)
	}
	updated, skipped, err := s.UpdateAll(items)
	if err != nil {
		return 0, 0, err
	}
	// 分组内变更：清空整组缓存
	s.invalidateGroup(group)
	return updated, skipped, nil
}

// Remove 删除配置（按主键）。
func (s *ConfigService) Remove(id string) error {
	c, err := s.getByIDRaw(id)
	if err != nil {
		return err
	}
	if err := s.db.Delete(&model.Config{}, "id = ?", c.ID).Error; err != nil {
		return svcInternal("删除配置失败: %v", err)
	}
	s.invalidate(c.Key)
	return nil
}

// ---------- 公开配置 ----------

// PublicConfigs 全部 public=true 配置，按分组聚合（对齐 getPublicConfigs）。
//
// 返回形态：{group: {key: value}}，便于前端按分组取用；
// 值按 type 做类型还原（number/boolean/json），与原始实现一致。
//
// 返回具名类型 dto.PublicConfigsDto（内部就是同一个 map 类型），使注解与运行时同源。
func (s *ConfigService) PublicConfigs() (dto.PublicConfigsDto, error) {
	if s.db == nil {
		return nil, svcUnavailable()
	}
	var rows []model.Config
	if err := s.db.Where("public = ?", true).Order(`"group" ASC, key ASC`).Find(&rows).Error; err != nil {
		return nil, svcInternal("查询公开配置失败: %v", err)
	}
	return dto.PublicConfigsDto(aggregatePublic(rows)), nil
}

// aggregatePublic 把 public 配置聚合成 {group: {key: value}}（纯函数，便于单测）。
//
// 兜底剔除两类：
//  1. 敏感项（isSensitiveKey 命中）一律不下发——即便被误标 public（例如有人把
//     s3_secret_key 的 public 改成 true），也不会经匿名接口泄露；
//  2. 敏感词表（sensitive_words）单独剔除：它不在密钥白名单里（词表不是密钥），
//     但它是内容审核资产，绝不能出现在匿名接口（原实现同样在此处 delete，语义保持）。
func aggregatePublic(rows []model.Config) map[string]map[string]interface{} {
	out := make(map[string]map[string]interface{}, 8)
	for _, c := range rows {
		if isSensitiveKey(c.Key) || c.Key == model.ConfigKeySensitiveWords {
			continue
		}
		g := orDefaultStr(c.Group, model.ConfigGroupSystem)
		if out[g] == nil {
			out[g] = map[string]interface{}{}
		}
		out[g][c.Key] = castValue(c.Value, c.Type)
	}
	return out
}

// AdvertisementConfig 广告配置（对齐 getAdvertisementConfig，public + group=advertisement）。
//
// 广告组不含密钥类配置，故此处不做打码/剔除；若将来把敏感项误归入 advertisement 组，
// 需要在此追加 isSensitiveKey 兜底（与 PublicConfigs 的 aggregatePublic 保持一致）。
//
// 扁平字段用 configValue* 助手做类型回落：map 取不存在的键得到 nil，
// 直接塞进 interface{} 会让 JSON 出现 null（docs/21 违规）——
// 改用具名 dto 后字段是 bool/string 值类型，缺失即 false/""，null 面消失。
func (s *ConfigService) AdvertisementConfig() (*dto.AdvertisementConfigDto, error) {
	if s.db == nil {
		return nil, svcUnavailable()
	}
	var rows []model.Config
	if err := s.db.Where(`"group" = ?`, model.ConfigGroupAdvertisement).Order(`key ASC`).Find(&rows).Error; err != nil {
		return nil, svcInternal("查询广告配置失败: %v", err)
	}
	out := make(map[string]interface{}, len(rows))
	for _, c := range rows {
		out[c.Key] = castValue(c.Value, c.Type)
	}
	return &dto.AdvertisementConfigDto{
		Enabled:            castBool(out["advertisement_enabled"]),
		HomeBanner:         castString(out["advertisement_home_banner"]),
		HomeBannerLink:     castString(out["advertisement_home_banner_link"]),
		ArticleTop:         castString(out["advertisement_article_top"]),
		ArticleTopLink:     castString(out["advertisement_article_top_link"]),
		ArticleBottom:      castString(out["advertisement_article_bottom"]),
		ArticleBottomLink:  castString(out["advertisement_article_bottom_link"]),
		ArticleSidebar:     castString(out["advertisement_article_sidebar"]),
		ArticleSidebarLink: castString(out["advertisement_article_sidebar_link"]),
		GlobalNotice:       castString(out["advertisement_global_notice"]),
		Raw:                out,
	}, nil
}

// castString 任意配置值 → 字符串（nil → ""，非字符串用 fmt 兜底而非丢弃）。
//
// 为什么非字符串也转而不是返回 ""：广告位 URL 类配置若被误标成 number 类型，
// 直接丢掉会让前端「明明配了却显示空」，比显示原值更难排查。
func castString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprintf("%v", t)
	}
}

// castBool 任意配置值 → 布尔（nil → false）。
func castBool(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "true", "1", "yes", "on":
			return true
		}
		return false
	default:
		return false
	}
}

// ---------- 配置值读取（带缓存） ----------

// GetString 读取配置字符串值（一级进程内缓存 → Redis → DB）。
func (s *ConfigService) GetString(key, def string) string {
	if v, ok := s.cached(key); ok {
		return v
	}
	if s.db == nil {
		return def
	}
	var c model.Config
	if err := s.db.Where("key = ?", key).First(&c).Error; err != nil {
		return def
	}
	s.store(key, c.Value)
	return c.Value
}

// GetBool 读取布尔配置。
func (s *ConfigService) GetBool(key string, def bool) bool {
	raw := s.GetString(key, "")
	if raw == "" {
		return def
	}
	switch strings.ToLower(raw) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return def
	}
}

// GetInt 读取整数配置。
func (s *ConfigService) GetInt(key string, def int) int {
	raw := s.GetString(key, "")
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return def
	}
	return n
}

// cached 读缓存。
//
// 一致性红线（多实例部署）：**Redis 可用时以 Redis 为唯一事实源**——不读进程内
// map，否则 A 实例改了配置、B 实例内存仍是旧值，/clear-cache 也无法跨实例生效。
// 只有在 Redis 不可用（单实例降级）时才使用进程内 map。
func (s *ConfigService) cached(key string) (string, bool) {
	if s.rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		val, err := s.rdb.Get(ctx, configCachePrefix+key).Result()
		if err != nil {
			return "", false // miss 或 Redis 异常：一律回源 DB（宁可多查一次，不返回脏值）
		}
		return val, true
	}
	s.mu.RLock()
	v, ok := s.cache[key]
	s.mu.RUnlock()
	return v, ok
}

// store 写缓存（有 Redis 只写 Redis；无 Redis 落进程内 map）。
func (s *ConfigService) store(key, value string) {
	if s.rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		_ = s.rdb.Set(ctx, configCachePrefix+key, value, configCacheTTL).Err()
		return
	}
	s.mu.Lock()
	s.cache[key] = value
	s.mu.Unlock()
}

// invalidate 使单 key 缓存失效（Redis 为主，进程内 map 一并清理防止残留）。
func (s *ConfigService) invalidate(key string) {
	s.mu.Lock()
	delete(s.cache, key)
	s.mu.Unlock()
	if s.rdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.rdb.Del(ctx, configCachePrefix+key).Err()
}

// invalidateGroup 使整组缓存失效（分组批量更新后用）。
func (s *ConfigService) invalidateGroup(group string) {
	if s.db == nil {
		s.mu.Lock()
		s.cache = map[string]string{}
		s.mu.Unlock()
		return
	}
	var keys []string
	_ = s.db.Model(&model.Config{}).Where(`"group" = ?`, group).Pluck("key", &keys).Error
	for _, k := range keys {
		s.invalidate(k)
	}
}

// RefreshCache 清除某 key 的缓存（对外暴露，对齐 refreshConfigCache）。
func (s *ConfigService) RefreshCache(key string) {
	s.invalidate(key)
	if key == model.ConfigKeySensitiveWords {
		_ = s.ReloadSensitiveWords()
	}
}

// ---------- 敏感词 ----------

// SensitiveWords 敏感词列表（对齐 getSensitiveWords）。
func (s *ConfigService) SensitiveWords() ([]string, error) {
	if s.db == nil {
		return nil, svcUnavailable()
	}
	raw := s.GetString(model.ConfigKeySensitiveWords, "")
	if raw == "" {
		return []string{}, nil
	}
	var words []string
	if err := json.Unmarshal([]byte(raw), &words); err != nil {
		// 兼容历史脏数据（逗号分隔文本）
		parts := strings.Split(raw, ",")
		words = words[:0]
		for _, p := range parts {
			if w := strings.TrimSpace(p); w != "" {
				words = append(words, w)
			}
		}
	}
	return words, nil
}

// wordsSyncInterval 多实例下敏感词树的同步间隔（CheckText 触发的惰性校验）。
const wordsSyncInterval = 30 * time.Second

// ReloadSensitiveWords 从配置重新加载敏感词树（启动与变更后调用）。
func (s *ConfigService) ReloadSensitiveWords() error {
	words, err := s.SensitiveWords()
	if err != nil {
		return err
	}
	s.trie.Rebuild(words)
	s.wordsLoaded.Store(true)
	if payload, err := json.Marshal(words); err == nil {
		s.wordsFingerprint.Store(string(payload))
	}
	s.lastWordsSync.Store(time.Now().Unix())
	return nil
}

// syncWordsIfStale 惰性同步：每 wordsSyncInterval 校验一次 Redis 中的词表，
// 与本地前缀树指纹不一致则重建——解决「A 实例改了敏感词，B 实例仍在用内存旧树」。
//
// Redis 不可用时直接返回（单实例语义，靠显式 clear-cache 刷新）。
func (s *ConfigService) syncWordsIfStale() {
	if s.rdb == nil {
		return
	}
	now := time.Now().Unix()
	last := s.lastWordsSync.Load()
	if now-last < int64(wordsSyncInterval/time.Second) {
		return
	}
	// 抢占式更新，避免高并发下多协程同时回源
	if !s.lastWordsSync.CompareAndSwap(last, now) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	raw, err := s.rdb.Get(ctx, configCachePrefix+model.ConfigKeySensitiveWords).Result()
	if err != nil || raw == "" {
		// Redis 无缓存：直接回源 DB（同时会回填 Redis）
		_ = s.ReloadSensitiveWords()
		return
	}
	if raw == s.wordsFingerprint.Load() {
		return
	}
	var words []string
	if err := json.Unmarshal([]byte(raw), &words); err != nil {
		_ = s.ReloadSensitiveWords()
		return
	}
	s.trie.Rebuild(words)
	s.wordsFingerprint.Store(raw)
}

// AddSensitiveWord 添加单个敏感词（已存在则幂等成功）。
func (s *ConfigService) AddSensitiveWord(word string) error {
	w := strings.TrimSpace(word)
	if w == "" {
		return errcode.ErrBadRequest.WithKey(i18n.ConfigSensitiveEmpty)
	}
	return s.mutateWords(func(words []string) ([]string, error) {
		for _, it := range words {
			if strings.EqualFold(it, w) {
				return words, nil
			}
		}
		return append(words, w), nil
	})
}

// BatchAddSensitiveWords 批量添加（去重，忽略空项）。
func (s *ConfigService) BatchAddSensitiveWords(list []string) (int, error) {
	if len(list) == 0 {
		return 0, errcode.ErrBadRequest.WithKey(i18n.ConfigSensitiveEmpty)
	}
	added := 0
	err := s.mutateWords(func(words []string) ([]string, error) {
		seen := make(map[string]struct{}, len(words))
		for _, w := range words {
			seen[strings.ToLower(w)] = struct{}{}
		}
		for _, raw := range list {
			w := strings.TrimSpace(raw)
			if w == "" {
				continue
			}
			if _, ok := seen[strings.ToLower(w)]; ok {
				continue
			}
			seen[strings.ToLower(w)] = struct{}{}
			words = append(words, w)
			added++
		}
		return words, nil
	})
	return added, err
}

// RemoveSensitiveWord 删除单个敏感词。
func (s *ConfigService) RemoveSensitiveWord(word string) error {
	w := strings.TrimSpace(word)
	if w == "" {
		return errcode.ErrBadRequest.WithKey(i18n.ConfigSensitiveEmpty)
	}
	found := false
	err := s.mutateWords(func(words []string) ([]string, error) {
		out := make([]string, 0, len(words))
		for _, it := range words {
			if strings.EqualFold(it, w) {
				found = true
				continue
			}
			out = append(out, it)
		}
		return out, nil
	})
	if err != nil {
		return err
	}
	if !found {
		return errcode.ErrNotFound.WithKeyf(i18n.ConfigSensitiveNotFound, w)
	}
	return nil
}

// BatchRemoveSensitiveWords 批量删除，返回实际删除数量。
func (s *ConfigService) BatchRemoveSensitiveWords(list []string) (int, error) {
	if len(list) == 0 {
		return 0, errcode.ErrBadRequest.WithKey(i18n.ConfigSensitiveEmpty)
	}
	drop := make(map[string]struct{}, len(list))
	for _, raw := range list {
		if w := strings.TrimSpace(raw); w != "" {
			drop[strings.ToLower(w)] = struct{}{}
		}
	}
	removed := 0
	err := s.mutateWords(func(words []string) ([]string, error) {
		out := make([]string, 0, len(words))
		for _, it := range words {
			if _, ok := drop[strings.ToLower(it)]; ok {
				removed++
				continue
			}
			out = append(out, it)
		}
		return out, nil
	})
	return removed, err
}

// mutateWords 读-改-写敏感词 JSON（事务内 upsert + 重建树 + 缓存失效）。
func (s *ConfigService) mutateWords(fn func([]string) ([]string, error)) error {
	if s.db == nil {
		return svcUnavailable()
	}
	current, err := s.SensitiveWords()
	if err != nil {
		return err
	}
	next, err := fn(current)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(next)
	if err != nil {
		return svcInternal("序列化敏感词失败: %v", err)
	}
	if err := s.UpdateOne(model.ConfigKeySensitiveWords, string(payload)); err != nil {
		return err
	}
	s.trie.Rebuild(next)
	s.RefreshCache(model.ConfigKeySensitiveWords)
	return nil
}

// CheckText 文本敏感词检查（供 article/comment 等域调用）：
// 返回命中词与是否违规；未违规时命中词为空。
func (s *ConfigService) CheckText(text string) (string, bool) {
	// 懒加载：新实例（其它业务域自建）首次调用即从库加载词表
	if !s.wordsLoaded.Load() && s.db != nil {
		_ = s.ReloadSensitiveWords()
	}
	// 多实例惰性同步（30s 一次，Redis 为准）
	s.syncWordsIfStale()
	return s.trie.Match(text)
}

// TrieSize 当前敏感词条数（运维观测用）。
func (s *ConfigService) TrieSize() int { return s.trie.Size() }

// ---------- 内部工具 ----------

// castValue 按 type 还原配置值类型（number / boolean / json，其余按 string）。
func castValue(raw, typ string) interface{} {
	switch typ {
	case model.ConfigTypeNumber:
		if n, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil {
			return n
		}
		return raw
	case model.ConfigTypeBoolean:
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		}
		return raw
	case model.ConfigTypeJSON:
		var v interface{}
		if err := json.Unmarshal([]byte(raw), &v); err == nil {
			return v
		}
		return raw
	default:
		return raw
	}
}

// orDefaultStr 空字符串回退默认值。
func orDefaultStr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// 编译期守护：确保 config 包被使用（InfraConfig 由上层注入，此处仅保留引用位）。
var _ = config.InfraConfig{}

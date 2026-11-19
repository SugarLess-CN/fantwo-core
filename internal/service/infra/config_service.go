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
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
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

// ---------- 配置 CRUD ----------

// List 全部配置（对齐 ConfigController.findAll，权限 setting:read）。
func (s *ConfigService) List() ([]model.Config, error) {
	if s.db == nil {
		return nil, svcUnavailable("数据库不可用")
	}
	var out []model.Config
	if err := s.db.Order("group ASC, key ASC").Find(&out).Error; err != nil {
		return nil, svcInternal("查询配置失败: %v", err)
	}
	return out, nil
}

// ByGroup 按分组取配置（对齐 findByGroup）。
func (s *ConfigService) ByGroup(group string) ([]model.Config, error) {
	if s.db == nil {
		return nil, svcUnavailable("数据库不可用")
	}
	if strings.TrimSpace(group) == "" {
		return nil, errcode.ErrBadRequest.With("分组不能为空")
	}
	var out []model.Config
	if err := s.db.Where(`"group" = ?`, group).Order("key ASC").Find(&out).Error; err != nil {
		return nil, svcInternal("查询分组配置失败: %v", err)
	}
	return out, nil
}

// ByID 按主键取配置。
func (s *ConfigService) ByID(id string) (*model.Config, error) {
	if s.db == nil {
		return nil, svcUnavailable("数据库不可用")
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

// ByKey 按 key 取配置。
func (s *ConfigService) ByKey(key string) (*model.Config, error) {
	if s.db == nil {
		return nil, svcUnavailable("数据库不可用")
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

// Create 新建配置（key 唯一，冲突返回 409）。
func (s *ConfigService) Create(in ConfigInput) (*model.Config, error) {
	if s.db == nil {
		return nil, svcUnavailable("数据库不可用")
	}
	if strings.TrimSpace(in.Key) == "" {
		return nil, errcode.ErrBadRequest.With("配置 key 不能为空")
	}
	var cnt int64
	if err := s.db.Model(&model.Config{}).Where("key = ?", in.Key).Count(&cnt).Error; err != nil {
		return nil, svcInternal("查询配置失败: %v", err)
	}
	if cnt > 0 {
		return nil, errcode.ErrConflict.Withf("配置项 %s 已存在", in.Key)
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
	return c, nil
}

// Update 按主键更新配置。
func (s *ConfigService) Update(id string, in ConfigInput) (*model.Config, error) {
	c, err := s.ByID(id)
	if err != nil {
		return nil, err
	}
	updates := map[string]interface{}{}
	if in.Value != "" || in.Type != "" {
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
	if len(updates) > 0 {
		if err := s.db.Model(&model.Config{}).Where("id = ?", c.ID).Updates(updates).Error; err != nil {
			return nil, svcInternal("更新配置失败: %v", err)
		}
		s.invalidate(c.Key)
	}
	return s.ByID(id)
}

// UpdateOne 按 key 更新单个值（供业务域写配置，如审核开关）。
func (s *ConfigService) UpdateOne(key, value string) error {
	if s.db == nil {
		return svcUnavailable("数据库不可用")
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

// UpdateAll 批量更新（body 为 [{key,value}] 或 [{id,value}]，对齐 updateAll）。
func (s *ConfigService) UpdateAll(items []ConfigInput) error {
	if s.db == nil {
		return svcUnavailable("数据库不可用")
	}
	if len(items) == 0 {
		return errcode.ErrBadRequest.With("配置列表不能为空")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		for _, it := range items {
			var c model.Config
			var err error
			if it.Key != "" {
				err = tx.Where("key = ?", it.Key).First(&c).Error
			} else {
				return errcode.ErrBadRequest.With("配置项缺少 key")
			}
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue // 不存在的 key 跳过（对齐原实现：静默忽略）
				}
				return svcInternal("查询配置失败: %v", err)
			}
			if err := tx.Model(&model.Config{}).Where("id = ?", c.ID).Update("value", it.Value).Error; err != nil {
				return svcInternal("更新配置失败: %v", err)
			}
		}
		return nil
	})
}

// UpdateGroup 批量更新某分组（先校验分组存在，再逐项更新）。
func (s *ConfigService) UpdateGroup(group string, items []ConfigInput) error {
	if s.db == nil {
		return svcUnavailable("数据库不可用")
	}
	if strings.TrimSpace(group) == "" {
		return errcode.ErrBadRequest.With("分组不能为空")
	}
	var cnt int64
	if err := s.db.Model(&model.Config{}).Where(`"group" = ?`, group).Count(&cnt).Error; err != nil {
		return svcInternal("查询分组配置失败: %v", err)
	}
	if cnt == 0 {
		return errcode.ErrNotFound.Withf("分组 %s 不存在", group)
	}
	if err := s.UpdateAll(items); err != nil {
		return err
	}
	// 分组内变更：清空整组缓存
	s.invalidateGroup(group)
	return nil
}

// Remove 删除配置（按主键）。
func (s *ConfigService) Remove(id string) error {
	c, err := s.ByID(id)
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
func (s *ConfigService) PublicConfigs() (map[string]map[string]interface{}, error) {
	if s.db == nil {
		return nil, svcUnavailable("数据库不可用")
	}
	var rows []model.Config
	if err := s.db.Where("public = ?", true).Order(`"group" ASC, key ASC`).Find(&rows).Error; err != nil {
		return nil, svcInternal("查询公开配置失败: %v", err)
	}
	out := make(map[string]map[string]interface{}, 8)
	for _, c := range rows {
		g := orDefaultStr(c.Group, model.ConfigGroupSystem)
		if out[g] == nil {
			out[g] = map[string]interface{}{}
		}
		out[g][c.Key] = castValue(c.Value, c.Type)
	}
	// 敏感词绝不下发到匿名接口（即便误标 public 也强制剔除）
	for _, g := range out {
		delete(g, model.ConfigKeySensitiveWords)
	}
	return out, nil
}

// AdvertisementConfig 广告配置（对齐 getAdvertisementConfig，public + group=advertisement）。
func (s *ConfigService) AdvertisementConfig() (map[string]interface{}, error) {
	if s.db == nil {
		return nil, svcUnavailable("数据库不可用")
	}
	var rows []model.Config
	if err := s.db.Where(`"group" = ?`, model.ConfigGroupAdvertisement).Order("key ASC").Find(&rows).Error; err != nil {
		return nil, svcInternal("查询广告配置失败: %v", err)
	}
	out := make(map[string]interface{}, len(rows))
	for _, c := range rows {
		out[c.Key] = castValue(c.Value, c.Type)
	}
	// 显式给出前端常用的扁平字段，兼容前端按固定字段名读取
	return map[string]interface{}{
		"enabled":            out["advertisement_enabled"],
		"homeBanner":         out["advertisement_home_banner"],
		"homeBannerLink":     out["advertisement_home_banner_link"],
		"articleTop":         out["advertisement_article_top"],
		"articleTopLink":     out["advertisement_article_top_link"],
		"articleBottom":      out["advertisement_article_bottom"],
		"articleBottomLink":  out["advertisement_article_bottom_link"],
		"articleSidebar":     out["advertisement_article_sidebar"],
		"articleSidebarLink": out["advertisement_article_sidebar_link"],
		"globalNotice":       out["advertisement_global_notice"],
		"raw":                out,
	}, nil
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

// cached 读一级/二级缓存。
func (s *ConfigService) cached(key string) (string, bool) {
	s.mu.RLock()
	v, ok := s.cache[key]
	s.mu.RUnlock()
	if ok {
		return v, true
	}
	if s.rdb == nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	val, err := s.rdb.Get(ctx, configCachePrefix+key).Result()
	if err != nil {
		return "", false
	}
	s.mu.Lock()
	s.cache[key] = val
	s.mu.Unlock()
	return val, true
}

// store 写两级缓存。
func (s *ConfigService) store(key, value string) {
	s.mu.Lock()
	s.cache[key] = value
	s.mu.Unlock()
	if s.rdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = s.rdb.Set(ctx, configCachePrefix+key, value, configCacheTTL).Err()
}

// invalidate 使单 key 缓存失效（进程内 + Redis）。
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
		return nil, svcUnavailable("数据库不可用")
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

// ReloadSensitiveWords 从配置重新加载敏感词树（启动与变更后调用）。
func (s *ConfigService) ReloadSensitiveWords() error {
	words, err := s.SensitiveWords()
	if err != nil {
		return err
	}
	s.trie.Rebuild(words)
	s.wordsLoaded.Store(true)
	return nil
}

// AddSensitiveWord 添加单个敏感词（已存在则幂等成功）。
func (s *ConfigService) AddSensitiveWord(word string) error {
	w := strings.TrimSpace(word)
	if w == "" {
		return errcode.ErrBadRequest.With("敏感词不能为空")
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
		return 0, errcode.ErrBadRequest.With("敏感词列表不能为空")
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
		return errcode.ErrBadRequest.With("敏感词不能为空")
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
		return errcode.ErrNotFound.Withf("敏感词 %s 不存在", w)
	}
	return nil
}

// BatchRemoveSensitiveWords 批量删除，返回实际删除数量。
func (s *ConfigService) BatchRemoveSensitiveWords(list []string) (int, error) {
	if len(list) == 0 {
		return 0, errcode.ErrBadRequest.With("敏感词列表不能为空")
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
		return svcUnavailable("数据库不可用")
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

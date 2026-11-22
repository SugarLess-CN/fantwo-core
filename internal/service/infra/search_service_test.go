// 搜索服务单测：地址解析、配置签名、DB 优先读取、client 惰性重建。
//
// 全部为纯内存测试（不连 Elasticsearch、不连 DB/Redis）：
//   - 地址解析与签名是纯函数；
//   - 配置读取用 ConfigService 的进程内缓存桩（rdb=nil 时 GetString 走 map）模拟「DB 里有值」；
//   - elasticsearch.NewClient 只构造客户端不发起连接，因此重建路径也可离线验证。
package infra

import (
	"reflect"
	"testing"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/config"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// newStubConfigService 构造不依赖 DB/Redis 的配置服务桩。
//
// ConfigService.cached 在 rdb 为 nil 时读进程内 map，因此预填该 map 等价于
// 「DB 里已有这条配置」——用于验证 DB 优先逻辑，无需真库。
func newStubConfigService(kv map[string]string) *ConfigService {
	return &ConfigService{cache: kv}
}

// resetParamsCache 清空参数快照（searchParamsTTL 窗口内会命中缓存，测试需立即读到新值）。
func resetParamsCache(s *Searcher) {
	s.paramsMu.Lock()
	s.params, s.paramsAt = esParams{}, time.Time{}
	s.paramsMu.Unlock()
}

func TestParseAddresses(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{
			name: "JSON 数组（DB 推荐写法）",
			raw:  `["http://127.0.0.1:9200","http://10.0.0.2:9200"]`,
			want: []string{"http://127.0.0.1:9200", "http://10.0.0.2:9200"},
		},
		{
			name: "逗号分隔（对齐 SEARCH_ADDRESSES 习惯）",
			raw:  "http://a:9200, http://b:9200 ,",
			want: []string{"http://a:9200", "http://b:9200"},
		},
		{
			name: "单值",
			raw:  "http://only:9200",
			want: []string{"http://only:9200"},
		},
		{
			name: "空串",
			raw:  "",
			want: nil,
		},
		{
			name: "纯空白",
			raw:  "  \t ",
			want: nil,
		},
		{
			name: "空 JSON 数组（种子默认值，须回退 env）",
			raw:  "[]",
			want: nil,
		},
		{
			name: "JSON 数组去重保序",
			raw:  `["http://c:9200","http://c:9200","http://d:9200"]`,
			want: []string{"http://c:9200", "http://d:9200"},
		},
		{
			name: "JSON 数组含空项",
			raw:  `["http://g:9200",""]`,
			want: []string{"http://g:9200"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseAddresses(c.raw); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("parseAddresses(%q)：得到 %#v，期望 %#v", c.raw, got, c.want)
			}
		})
	}
}

func TestSignParams(t *testing.T) {
	base := esParams{
		Enabled:   true,
		Addresses: []string{"http://a:9200"},
		Username:  "user",
		Password:  "pass",
		Index:     "idx",
	}
	if signParams(base) != signParams(base) {
		t.Fatal("同一组参数两次签名应相同")
	}

	changes := []struct {
		name   string
		mutate func(*esParams)
	}{
		{"地址变化", func(p *esParams) { p.Addresses = []string{"http://b:9200"} }},
		{"用户名变化", func(p *esParams) { p.Username = "user2" }},
		{"密码变化", func(p *esParams) { p.Password = "pass2" }},
		{"索引变化", func(p *esParams) { p.Index = "idx2" }},
	}
	for _, c := range changes {
		got := base
		c.mutate(&got)
		if signParams(got) == signParams(base) {
			t.Errorf("%s：签名应不同，实际相同（%s）", c.name, signParams(got))
		}
	}

	// enabled 只是开关，不属于 client 身份，不进签名（否则开关一抖就重建连接）
	onlyEnabled := base
	onlyEnabled.Enabled = false
	if signParams(onlyEnabled) != signParams(base) {
		t.Error("enabled 变化不应改变签名")
	}
}

func TestConfigSignature_ReflectsChanges(t *testing.T) {
	baseCfg := config.SearchEngineConfig{
		Enabled:       true,
		Addresses:     []string{"http://a:9200"},
		Username:      "user",
		Password:      "pass",
		ArticlesIndex: "idx1",
	}
	newSearcher := func(mut func(*Searcher)) *Searcher {
		s := NewSearcher(baseCfg)
		if mut != nil {
			mut(s)
		}
		return s // 新建实例 paramsAt 为零值，首次 configSignature 会直读 cfg
	}

	baseSig := newSearcher(nil).configSignature()
	if baseSig == "" {
		t.Fatal("签名不应为空")
	}
	if again := newSearcher(nil).configSignature(); again != baseSig {
		t.Fatalf("相同配置签名应相同：%s vs %s", again, baseSig)
	}

	mutations := []struct {
		name string
		mut  func(*Searcher)
	}{
		{"地址变化", func(s *Searcher) { s.cfg.Addresses = []string{"http://b:9200"} }},
		{"用户名变化", func(s *Searcher) { s.cfg.Username = "user2" }},
		{"密码变化", func(s *Searcher) { s.cfg.Password = "pass2" }},
		{"索引变化", func(s *Searcher) { s.cfg.ArticlesIndex = "idx2" }},
	}
	for _, m := range mutations {
		if got := newSearcher(m.mut).configSignature(); got == baseSig {
			t.Errorf("%s：签名应不同，实际相同（%s）", m.name, got)
		}
	}

	if got := newSearcher(func(s *Searcher) { s.cfg.Enabled = false }).configSignature(); got != baseSig {
		t.Errorf("仅 enabled 变化不应改签名：%s vs %s", got, baseSig)
	}
}

func TestReadParams_DBOverridesEnv(t *testing.T) {
	envCfg := config.SearchEngineConfig{
		Enabled:       true,
		Addresses:     []string{"http://env:9200"},
		Username:      "envuser",
		Password:      "envpass",
		ArticlesIndex: "env_idx",
	}

	// 1) 未注入配置服务：全部走启动期 env 值
	s := NewSearcher(envCfg)
	p := s.currentParams()
	if !reflect.DeepEqual(p.Addresses, envCfg.Addresses) || p.Index != "env_idx" || p.Username != "envuser" {
		t.Fatalf("未注入 cfgSvc 时应走 env：%+v", p)
	}

	// 2) 注入 DB 值：四项 client 参数 + 开关全部被 DB 覆盖
	s.cfgSvc = newStubConfigService(map[string]string{
		model.ConfigKeySearchEnabled:       "false",
		model.ConfigKeySearchAddresses:     `["http://db1:9200","http://db2:9200"]`,
		model.ConfigKeySearchUsername:      "dbuser",
		model.ConfigKeySearchPassword:      "dbpass",
		model.ConfigKeySearchArticlesIndex: "db_idx",
	})
	resetParamsCache(s)
	p = s.currentParams()
	if p.Enabled {
		t.Error("search_enabled=false 应生效")
	}
	if want := []string{"http://db1:9200", "http://db2:9200"}; !reflect.DeepEqual(p.Addresses, want) {
		t.Errorf("地址应取 DB 值：得到 %#v，期望 %#v", p.Addresses, want)
	}
	if p.Username != "dbuser" || p.Password != "dbpass" {
		t.Errorf("凭据应取 DB 值：%s/%s", p.Username, p.Password)
	}
	if got := s.Index(); got != "db_idx" {
		t.Errorf("Index() 应取 DB 值：%s", got)
	}
	if s.Enabled() {
		t.Error("Enabled() 应跟随 DB 开关为 false")
	}
}

func TestReadParams_SeedEmptyAddressesFallsBackToEnv(t *testing.T) {
	envCfg := config.SearchEngineConfig{
		Enabled:       true,
		Addresses:     []string{"http://env:9200"},
		ArticlesIndex: "env_idx",
	}
	s := NewSearcher(envCfg)
	// 新建库的种子值：search_addresses="[]"、search_articles_index 有默认值
	s.cfgSvc = newStubConfigService(map[string]string{
		model.ConfigKeySearchAddresses:     "[]",
		model.ConfigKeySearchArticlesIndex: "",
	})
	resetParamsCache(s)

	p := s.currentParams()
	if want := []string{"http://env:9200"}; !reflect.DeepEqual(p.Addresses, want) {
		t.Fatalf("种子值 [] 不应清空 env 地址：得到 %#v", p.Addresses)
	}
	if got := s.Index(); got != "env_idx" {
		t.Fatalf("空索引配置应回退 env 值：%s", got)
	}
}

func TestReadParams_CommaSeparatedAddresses(t *testing.T) {
	s := NewSearcher(config.SearchEngineConfig{Enabled: true, Addresses: []string{"http://env:9200"}})
	s.cfgSvc = newStubConfigService(map[string]string{
		model.ConfigKeySearchAddresses: "http://db1:9200, http://db2:9200",
	})
	resetParamsCache(s)

	want := []string{"http://db1:9200", "http://db2:9200"}
	if got := s.currentParams().Addresses; !reflect.DeepEqual(got, want) {
		t.Fatalf("逗号分隔地址解析错误：得到 %#v，期望 %#v", got, want)
	}
}

func TestEnsureClient_DisabledSkipsClient(t *testing.T) {
	// 开关关闭：不建 client，也不报错
	s := NewSearcher(config.SearchEngineConfig{Enabled: false, Addresses: []string{"http://127.0.0.1:9200"}})
	if err := s.ensureClient(); err != nil {
		t.Fatalf("关闭状态 ensureClient 应返回 nil：%v", err)
	}
	s.mu.RLock()
	c := s.client
	s.mu.RUnlock()
	if c != nil {
		t.Fatal("关闭状态不应建 client")
	}

	// 无地址：同样不建（Enabled 语义需要至少一个地址）
	s2 := NewSearcher(config.SearchEngineConfig{Enabled: true})
	if err := s2.ensureClient(); err != nil {
		t.Fatalf("无地址时 ensureClient 应返回 nil：%v", err)
	}
	s2.mu.RLock()
	c2 := s2.client
	s2.mu.RUnlock()
	if c2 != nil {
		t.Fatal("无地址时不应建 client")
	}
}

func TestEnsureClient_BuildsAndRebuildsOnSignatureChange(t *testing.T) {
	s := NewSearcher(config.SearchEngineConfig{
		Enabled:       true,
		Addresses:     []string{"http://127.0.0.1:9200"},
		ArticlesIndex: "idx_a",
	})
	if err := s.ensureClient(); err != nil {
		t.Fatalf("首次 ensureClient 失败：%v", err)
	}
	s.mu.RLock()
	first, firstSig := s.client, s.signature
	s.mu.RUnlock()
	if first == nil || firstSig == "" {
		t.Fatal("首次调用应建立 client 并记录签名")
	}

	// 签名未变：复用同一 client（快路径）
	if err := s.ensureClient(); err != nil {
		t.Fatalf("二次 ensureClient 失败：%v", err)
	}
	s.mu.RLock()
	second := s.client
	s.mu.RUnlock()
	if second != first {
		t.Fatal("签名未变时应复用同一 client")
	}

	// 索引变化 → 签名变化 → 重建
	s.cfg.ArticlesIndex = "idx_b"
	resetParamsCache(s)
	if err := s.ensureClient(); err != nil {
		t.Fatalf("配置变化后 ensureClient 失败：%v", err)
	}
	s.mu.RLock()
	third, thirdSig := s.client, s.signature
	s.mu.RUnlock()
	if third == first {
		t.Fatal("签名变化后应重建 client")
	}
	if thirdSig == firstSig {
		t.Fatal("签名应随配置更新")
	}
}

func TestWithConfigService_NilIsSafe(t *testing.T) {
	s := NewSearcher(config.SearchEngineConfig{
		Enabled:   true,
		Addresses: []string{"http://x:9200"},
	}).WithConfigService(nil)
	if !s.Enabled() {
		t.Fatal("nil cfgSvc 时应回退 env 的 Enabled")
	}
	if got := s.Index(); got != articlesIndexDefault {
		t.Fatalf("空索引应回退默认值：%s", got)
	}
}

// TestClientOrInit_ClosedEvenWithStaleClient 运行期关掉接口后，内存里的旧 client
// 必须对调用方不可见（ensureClient 会保留旧 client 供重新打开时复用，不能顺带放行）。
func TestClientOrInit_ClosedEvenWithStaleClient(t *testing.T) {
	s := NewSearcher(config.SearchEngineConfig{
		Enabled:       true,
		Addresses:     []string{"http://127.0.0.1:9200"},
		ArticlesIndex: "idx",
	})
	if _, err := s.clientOrInit(); err != nil {
		t.Fatalf("启用状态应拿到 client：%v", err)
	}

	// 运行期关闭：旧 client 仍在内存，但对外必须不可用
	s.cfg.Enabled = false
	resetParamsCache(s)
	if _, err := s.clientOrInit(); err == nil {
		t.Fatal("关闭后 clientOrInit 应返回「服务未配置」，不能复用旧 client")
	}

	// 重新打开且签名未变：复用旧 client，不重复重建
	s.cfg.Enabled = true
	resetParamsCache(s)
	s.mu.RLock()
	before := s.client
	s.mu.RUnlock()
	if _, err := s.clientOrInit(); err != nil {
		t.Fatalf("重新打开后应恢复可用：%v", err)
	}
	s.mu.RLock()
	after := s.client
	s.mu.RUnlock()
	if before != after {
		t.Fatal("重新打开且签名未变时应复用旧 client")
	}
}

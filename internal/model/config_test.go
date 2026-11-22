package model

import "testing"

// TestIsSensitiveConfigKey 表驱动覆盖三档：白名单命中、命名兜底命中、非敏感放行。
func TestIsSensitiveConfigKey(t *testing.T) {
	cases := []struct {
		key  string
		want bool
		why  string
	}{
		// ① 白名单命中（ConfigSensitiveKeys）
		{ConfigKeyS3AccessKey, true, "白名单：s3_access_key"},
		{ConfigKeyS3SecretKey, true, "白名单：s3_secret_key"},
		{ConfigKeySearchPassword, true, "白名单：search_password"},

		// ② 命名兜底命中（password / secret / access_key / _token / private_key）
		{"foo_password", true, "含 password"},
		{"my_secret", true, "含 secret"},
		{"x_access_key", true, "含 access_key"},
		{"a_token", true, "含 _token"},
		{"refresh_token", true, "含 _token：refresh_token 这类必须打码"},
		{"b_private_key", true, "含 private_key"},
		{"S3_SECRET_KEY_V2", true, "大小写不敏感 + 不在白名单也靠兜底命中"},
		{"mail_password_backup", true, "password 出现在中段也要命中"},

		// ③ 非敏感放行
		{"site_name", false, "无任何关键词"},
		{"search_username", false, "用户名不是密钥"},
		{"sensitive_words", false, "sensitive 不在关键词表，且敏感词表本身要明文可见"},
		{"upload_max_image_size", false, "数值限制项"},
		{"upload_thumbnail_specs", false, "公开规格项"},
		{"tokenizer", false, "token 前无下划线，不得误命中"},
		{"s3_endpoint", false, "endpoint 不是密钥"},
		{"cdn_base_url", false, "公开项"},
		{"", false, "空串不敏感"},
	}

	for _, c := range cases {
		if got := IsSensitiveConfigKey(c.key); got != c.want {
			t.Errorf("IsSensitiveConfigKey(%q) = %v, want %v（%s）", c.key, got, c.want, c.why)
		}
	}
}

// TestConfigSensitiveKeysWhitelist 校验白名单自身的口径：
// 每条白名单 key 必然被判为敏感，且白名单恰好就是那三个明文密钥项（防止有人只加常量忘加白名单）。
func TestConfigSensitiveKeysWhitelist(t *testing.T) {
	want := []string{
		"search_password",
		"s3_access_key",
		"s3_secret_key",
	}
	if len(ConfigSensitiveKeys) != len(want) {
		t.Fatalf("ConfigSensitiveKeys 条数 = %d, want %d: %v", len(ConfigSensitiveKeys), len(want), ConfigSensitiveKeys)
	}
	has := make(map[string]struct{}, len(ConfigSensitiveKeys))
	for _, k := range ConfigSensitiveKeys {
		has[k] = struct{}{}
		if !IsSensitiveConfigKey(k) {
			t.Errorf("白名单 key %q 未被 IsSensitiveConfigKey 判为敏感", k)
		}
	}
	for _, k := range want {
		if _, ok := has[k]; !ok {
			t.Errorf("白名单缺少 %q，实际 %v", k, ConfigSensitiveKeys)
		}
	}
}

// TestConfigSensitiveKeysCoverSeeds 单向不变量：种子里凡是被判为敏感的 key，都必须显式列入白名单。
// 兜底关键词只用于「漏加白名单也不打码泄露」的保险，真正的口径以白名单为准 ——
// 因此新增密钥类种子却忘了补白名单时，这里会失败。
//
// 注意方向：**种子被判敏感 ⊆ 白名单**，而不是两者相等。白名单允许包含不在种子里的 key
// （见 TestSensitiveKeysNotInSeedsAreAllowed：S3 七项是启动期配置，不写种子但保留打码口径）。
func TestConfigSensitiveKeysCoverSeeds(t *testing.T) {
	whitelist := make(map[string]struct{}, len(ConfigSensitiveKeys))
	for _, k := range ConfigSensitiveKeys {
		whitelist[k] = struct{}{}
	}
	for _, seed := range DefaultConfigSeeds() {
		if !IsSensitiveConfigKey(seed.Key) {
			continue
		}
		if _, ok := whitelist[seed.Key]; !ok {
			t.Errorf("种子 %q 被判为敏感但未列入 ConfigSensitiveKeys（兜底命中不代表已打码，请补白名单）", seed.Key)
		}
	}
}

// TestSensitiveKeysNotInSeedsAreAllowed 反向口径：白名单里的 key **允许**不在种子中。
//
// 场景（T-34b3 裁定）：S3 七项（endpoint / bucket / access_key / secret_key / region /
// use_ssl / force_path_style）绑定启动期创建的 minio client，改了必须重启，因此不写种子 ——
// 库里留一条永远不生效的假配置比不迁更危险。但它们的 key 常量与打码白名单必须保留：
// 库里可能已有历史数据，将来做「配置签名变化 → 惰性重建 client」时也要直接启用。
// 本测试把这个「允许缺失」的口径钉住，避免后来者误认为「白名单项必须出现在种子中」而补回假种子。
//
// 若将来 S3 真的纳入运行期可调并加回种子，请同步更新本测试的快照断言与 T-34b3 的决策记录。
func TestSensitiveKeysNotInSeedsAreAllowed(t *testing.T) {
	seedKeys := make(map[string]struct{}, 42)
	for _, seed := range DefaultConfigSeeds() {
		seedKeys[seed.Key] = struct{}{}
	}

	inSeed := make([]string, 0, len(ConfigSensitiveKeys))
	notInSeed := make([]string, 0, len(ConfigSensitiveKeys))
	for _, k := range ConfigSensitiveKeys {
		if _, ok := seedKeys[k]; ok {
			inSeed = append(inSeed, k)
		} else {
			notInSeed = append(notInSeed, k)
			// 不在种子里的白名单项仍必须是敏感项（白名单本身就是权威口径）。
			if !IsSensitiveConfigKey(k) {
				t.Errorf("白名单 key %q 未被判为敏感", k)
			}
		}
	}
	t.Logf("白名单 %d 项：在种子里 %v，不在种子里（允许，预留项）%v", len(ConfigSensitiveKeys), inSeed, notInSeed)

	// 快照：当前 S3 两项为预留项（启动期配置），必须不在种子里。
	for _, k := range []string{ConfigKeyS3AccessKey, ConfigKeyS3SecretKey} {
		if _, ok := seedKeys[k]; ok {
			t.Errorf("%q 不应出现在种子里（S3 为启动期配置，见 T-34b3 裁定）；若确实要纳入运行期可调，请先改测试口径", k)
		}
	}
}

// TestDefaultConfigSeedsCount 种子总数快照：35（原有）+ 14（T-34a）- 7（T-34b3 删 S3）= 42。
// 任何增删都应是有意为之 —— 数字对不上时先看上面的取舍说明，别直接改这个常量。
func TestDefaultConfigSeedsCount(t *testing.T) {
	if got := len(DefaultConfigSeeds()); got != 42 {
		t.Errorf("DefaultConfigSeeds() 条数 = %d, want 42（增删种子请同步更新取舍说明与本断言）", got)
	}
}

// TestIsStartupOnlyConfigKey 表驱动覆盖启动期配置判定（精确匹配，无子串兜底）。
func TestIsStartupOnlyConfigKey(t *testing.T) {
	cases := []struct {
		key  string
		want bool
		why  string
	}{
		// ① 启动期配置：8 项全部 true
		{"storage_driver", true, "minio client 类型选择（无 key 常量，字面量）"},
		{ConfigKeyS3Endpoint, true, "S3 Endpoint"},
		{ConfigKeyS3Bucket, true, "S3 Bucket"},
		{ConfigKeyS3Region, true, "S3 Region"},
		{ConfigKeyS3AccessKey, true, "S3 Access Key（同时是敏感项）"},
		{ConfigKeyS3SecretKey, true, "S3 Secret Key（同时是敏感项）"},
		{ConfigKeyS3UseSSL, true, "S3 use_ssl"},
		{ConfigKeyS3ForcePathStyle, true, "S3 force_path_style"},

		// ② 运行期可调配置：一项都不许命中（误判会让运维该改的被拒）
		{"storage_local_dir", false, "运行期可调：本地目录"},
		{"cdn_base_url", false, "运行期可调：CDN 域名"},
		{"upload_max_image_size", false, "运行期可调：上传上限"},
		{"search_addresses", false, "运行期可调：ES 地址"},
		{"upload_compression_enabled", false, "运行期可调：压缩开关"},

		// ③ 边界
		{"", false, "空串"},
		{"s3", false, "无子串兜底：s3 本身不是 key"},
		{"s3_endpoint_old", false, "无子串兜底：后缀变体不命中（与敏感判定的宽严取向相反）"},
	}

	for _, c := range cases {
		if got := IsStartupOnlyConfigKey(c.key); got != c.want {
			t.Errorf("IsStartupOnlyConfigKey(%q) = %v, want %v（%s）", c.key, got, c.want, c.why)
		}
	}
}

// TestStartupOnlyKeysNotSeeded 不变量：启动期配置**永不**写进种子。
// 这是 T-34b3 裁定的核心（库里不留永不生效的假配置），也是删掉 7 条 S3 种子的理由。
func TestStartupOnlyKeysNotSeeded(t *testing.T) {
	for _, seed := range DefaultConfigSeeds() {
		if IsStartupOnlyConfigKey(seed.Key) {
			t.Errorf("种子 %q 属于启动期配置，不应写种子（改了必须重启才可能生效，库里留假配置比不迁更危险）", seed.Key)
		}
	}
}

// TestStartupOnlyKeysIntersectSensitiveKeys 两个清单的交集必须恰好是 S3 的两把密钥。
//
// 口径：s3_access_key / s3_secret_key 既是启动期配置（不许写库），又要在管理端打码返回
// （历史数据 / 将来启用时展示）。除这两项之外，任何交集都说明有人往任一清单塞错了东西：
// 例如把 search_password 加进启动期清单（会让运行期可改的密钥被拒写），
// 或把 storage_driver 加进敏感清单（它根本不会出现在库里）。
func TestStartupOnlyKeysIntersectSensitiveKeys(t *testing.T) {
	want := map[string]struct{}{
		ConfigKeyS3AccessKey: {},
		ConfigKeyS3SecretKey: {},
	}

	startup := make(map[string]struct{}, len(ConfigStartupOnlyKeys))
	for _, k := range ConfigStartupOnlyKeys {
		startup[k] = struct{}{}
	}

	got := make([]string, 0, 2)
	for _, k := range ConfigSensitiveKeys {
		if _, ok := startup[k]; ok {
			got = append(got, k)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("启动期清单 ∩ 敏感清单 = %v, want 恰好 %v", got, []string{ConfigKeyS3AccessKey, ConfigKeyS3SecretKey})
	}
	for _, k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("交集出现意外项 %q：启动期清单与敏感清单只允许 S3 的两把密钥重叠", k)
		}
	}
	// 反向核对 want 里的每项都真的出现在交集（防两边同时被删而测试仍然通过）。
	for k := range want {
		found := false
		for _, g := range got {
			if g == k {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q 应同时属于启动期清单与敏感清单", k)
		}
		if !IsStartupOnlyConfigKey(k) || !IsSensitiveConfigKey(k) {
			t.Errorf("%q 的判定结果与清单不一致（startup=%v sensitive=%v）",
				k, IsStartupOnlyConfigKey(k), IsSensitiveConfigKey(k))
		}
	}
}

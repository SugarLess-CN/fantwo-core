// 审核域核心逻辑单测（不依赖 PG / Redis / 云厂商）。
//
// 覆盖：
//  1. sceneEnabled 场景开关矩阵（provider=none 与各场景独立开关）；
//  2. sensitiveResult / checkSensitive：命中敏感词按 autoBlock 决定 block 或 review；
//  3. AuditConfig 的 json tag 必须 camelCase（防退回 PascalCase）；
//  4. queueFor / emitStatusChanged 状态回写分支；
//  5. resolveMsgID 空 msg_id 兜底（见下方事故注释）。
package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/queue"
)

// TestSceneEnabledMatrix 场景开关矩阵。
//
// 防什么 bug：
//   - provider=none 时仍去做云调用（无凭据 → 每次请求都超时/报错）；
//   - 场景串味（comment 开关控制到了 article）→ 运营在后台关了评论审核，
//     结果文章审核一起失灵；
//   - 未知场景默认放行 → 新增场景忘记登记时静默不审核。
func TestSceneEnabledMatrix(t *testing.T) {
	cases := []struct {
		name  string
		cfg   AuditConfig
		scene string
		want  bool
	}{
		{"provider=none 全关-评论", AuditConfig{Provider: AuditProviderNone, CommentEnabled: true}, model.AuditSceneComment, false},
		{"provider=none 全关-文章", AuditConfig{Provider: AuditProviderNone, ArticleEnabled: true}, model.AuditSceneArticle, false},

		{"阿里云+评论开", AuditConfig{Provider: AuditProviderAliyun, CommentEnabled: true}, model.AuditSceneComment, true},
		{"阿里云+评论关", AuditConfig{Provider: AuditProviderAliyun, CommentEnabled: false}, model.AuditSceneComment, false},
		{"腾讯云+头像开", AuditConfig{Provider: AuditProviderTencent, AvatarEnabled: true}, model.AuditSceneAvatar, true},
		{"腾讯云+头像关", AuditConfig{Provider: AuditProviderTencent, AvatarEnabled: false}, model.AuditSceneAvatar, false},
		{"腾讯云+图片开", AuditConfig{Provider: AuditProviderTencent, ImageEnabled: true}, model.AuditSceneImage, true},
		{"腾讯云+文章开", AuditConfig{Provider: AuditProviderTencent, ArticleEnabled: true}, model.AuditSceneArticle, true},

		// 串味检查：只开评论，其它三个场景必须仍然关闭
		{"只开评论时文章仍关",
			AuditConfig{Provider: AuditProviderAliyun, CommentEnabled: true}, model.AuditSceneArticle, false},
		{"只开评论时头像仍关",
			AuditConfig{Provider: AuditProviderAliyun, CommentEnabled: true}, model.AuditSceneAvatar, false},
		{"只开评论时图片仍关",
			AuditConfig{Provider: AuditProviderAliyun, CommentEnabled: true}, model.AuditSceneImage, false},

		{"未知场景默认关",
			AuditConfig{Provider: AuditProviderAliyun, CommentEnabled: true, ArticleEnabled: true}, "video", false},
		{"空场景默认关",
			AuditConfig{Provider: AuditProviderAliyun, CommentEnabled: true}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := c.cfg
			if got := cfg.sceneEnabled(c.scene); got != c.want {
				t.Fatalf("sceneEnabled(%q) with %+v = %v, want %v", c.scene, c.cfg, got, c.want)
			}
		})
	}
}

// TestSensitiveResult autoBlock 决定 block / review。
//
// 防什么 bug：autoBlock 判断反了 → 「配了拦截却只进复审」（违规内容直接上线），
// 或「配了复审却直接拦截」（误封正常内容）。
func TestSensitiveResult(t *testing.T) {
	cases := []struct {
		name          string
		word          string
		autoBlock     bool
		wantAction    string
		wantPassed    bool
		wantLabel     string
		wantReasonHas string
	}{
		{"autoBlock=true → 硬拦截", "违禁词", true, model.AuditActionBlock, false, "sensitive_word", "违禁词"},
		{"autoBlock=false → 转人工复审且放行", "违禁词", false, model.AuditActionReview, true, "sensitive_word", "违禁词"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sensitiveResult(c.word, c.autoBlock)
			if got.Action != c.wantAction {
				t.Errorf("action = %q, want %q", got.Action, c.wantAction)
			}
			if got.Passed != c.wantPassed {
				t.Errorf("passed = %v, want %v", got.Passed, c.wantPassed)
			}
			if got.Label != c.wantLabel {
				t.Errorf("label = %q, want %q", got.Label, c.wantLabel)
			}
			if !strings.Contains(got.Reason, c.wantReasonHas) {
				t.Errorf("reason = %q, 应包含命中词 %q", got.Reason, c.wantReasonHas)
			}
		})
	}
}

// TestCheckSensitive 敏感词命中链路（注入替身 checker，DB=nil 走降级分支）。
//
// 防什么 bug：
//   - 未注入 checker / 空文本时误判命中 → 所有内容被拦；
//   - DB 不可用（降级启动）时 panic 或静默放行 → 敏感词失效；
//   - 命中后不走 autoBlock 默认值（此时必须按拦截处理，宁可错杀）。
func TestCheckSensitive(t *testing.T) {
	// DB=nil：LoadAuditConfig 不可用，必须走「autoBlock 默认 true」降级分支且不 panic
	svc := NewAuditService(nil, nil)

	// 未注入 checker
	if _, hit := svc.checkSensitive("任意内容"); hit {
		t.Fatal("未注入 checker 时不应命中")
	}

	// 注入 checker：命中词为「违禁」
	svc.SetSensitiveChecker(func(text string) (string, bool) {
		if strings.Contains(text, "违禁") {
			return "违禁", true
		}
		return "", false
	})

	// 空内容：即使 checker 会命中也不拦（避免空串误判）
	if _, hit := svc.checkSensitive(""); hit {
		t.Fatal("空内容不应命中敏感词")
	}

	// 未命中
	if _, hit := svc.checkSensitive("正常内容"); hit {
		t.Fatal("正常内容不应命中敏感词")
	}

	// 命中：DB=nil → autoBlock 默认 true → block
	got, hit := svc.checkSensitive("这是违禁内容")
	if !hit {
		t.Fatal("含违禁词应命中")
	}
	if got.Action != model.AuditActionBlock || got.Passed {
		t.Fatalf("DB 不可用时应按 autoBlock=true 拦截，实际 action=%q passed=%v", got.Action, got.Passed)
	}
}

// TestAuditConfigJSONTagsCamelCase JSON 字段必须 camelCase，密钥字段必须脱敏。
//
// 防什么 bug（2026-11-19 联调缺陷）：AuditConfig 曾用 Go 字段名直出
// （Provider/CommentEnabled…），前端按 camelCase 读取恒为 undefined，
// 表现为「后台审核开关全部显示为关闭」，运营误判为配置未生效。
func TestAuditConfigJSONTagsSnakeCase(t *testing.T) {
	typ := reflect.TypeOf(AuditConfig{})
	allowedEmpty := map[string]bool{"TencentSecretKey": true, "AliyunAccessKeySecret": true}

	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" {
			t.Errorf("字段 %s 缺少 json tag（会以 Go 字段名直出）", f.Name)
			continue
		}
		name := strings.Split(tag, ",")[0]

		if allowedEmpty[f.Name] {
			if name != "-" {
				t.Errorf("密钥字段 %s 必须 json:\"-\"，实际 %q", f.Name, name)
			}
			continue
		}
		if name == "-" {
			continue
		}
		// 2026-11-27 规范改为 snake_case：小写字母、单词间下划线、不含大写字母
		if name != strings.ToLower(name) {
			t.Errorf("字段 %s 的 json tag %q 含大写字母（应为 snake_case）", f.Name, name)
		}
	}

	// 端到端再看一眼序列化结果：不能出现 PascalCase 键
	raw, err := json.Marshal(AuditConfig{Provider: "aliyun", CommentEnabled: true, AutoBlock: true})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	s := string(raw)
	for _, bad := range []string{`"Provider"`, `"CommentEnabled"`, `"AutoBlock"`, `"TencentSecretKey"`, `"AliyunAccessKeySecret"`} {
		if strings.Contains(s, bad) {
			t.Errorf("序列化结果不应包含 %s: %s", bad, s)
		}
	}
	for _, want := range []string{`"provider"`, `"comment_enabled"`, `"auto_block"`} {
		if !strings.Contains(s, want) {
			t.Errorf("序列化结果应包含 %s: %s", want, s)
		}
	}
}

// TestQueueFor 审核记录 → 队列名映射。
//
// 防什么 bug：图片任务被投到 text-audit 队列（并发 5 vs 10 不同），
// 且消费端按队列名做去重记录，错投会导致 image-audit 队列长期空转。
func TestQueueFor(t *testing.T) {
	svc := NewAuditService(nil, nil)
	cases := []struct {
		name string
		rec  model.AuditRecord
		want string
	}{
		{"文本记录 → text-audit", model.AuditRecord{Content: "文本"}, queue.QueueTextAudit},
		{"图片记录 → image-audit", model.AuditRecord{ImageURL: "https://x/y.png"}, queue.QueueImageAudit},
		{"同时有内容与图片时按图片算", model.AuditRecord{Content: "文本", ImageURL: "https://x/y.png"}, queue.QueueImageAudit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := c.rec
			if got := svc.queueFor(&rec); got != c.want {
				t.Fatalf("queueFor(%+v) = %q, want %q", c.rec, got, c.want)
			}
		})
	}
}

// TestResolveMsgID 空 msg_id 兜底。
//
// 2026-11-19 事故：msg_id 用 string 零值导致多条记录都为空串，
// 撞 uniqueIndex（SQLSTATE 23505），只有第一条审核记录能建。
// 修复后：msg_id 为空一律回退审核记录 ID，进入去重表的值永不为空串。
func TestResolveMsgID(t *testing.T) {
	cases := []struct {
		name     string
		msgID    string
		recordID string
		want     string
	}{
		{"正常 msg_id 直接使用", "task-abc", "rec-1", "task-abc"},
		{"空 msg_id 回退 recordID", "", "rec-1", "rec-1"},
		{"两者都空仍不 panic（返回空，由调用方 DB 层兜底）", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveMsgID(c.msgID, c.recordID); got != c.want {
				t.Fatalf("resolveMsgID(%q,%q) = %q, want %q", c.msgID, c.recordID, got, c.want)
			}
		})
	}

	// 事故回归断言：只要有 recordID，结果就绝不会是空串
	if got := resolveMsgID("", "rec-x"); got == "" {
		t.Fatal("有 recordID 时 msg_id 不得为空串（否则撞 uniqueIndex 23505）")
	}
}

// TestEmitStatusChanged 审核结果 → 跨域事件分支。
//
// 防什么 bug：
//   - 文章被拦截却发出 PUBLISHED（违规内容上线）；
//   - 复审（REVIEW）被当成通过；评论拦截状态与文章混用；
//   - 头像未拦截也发事件（用户域收到无意义通知）。
func TestEmitStatusChanged(t *testing.T) {
	prev := event.Default()
	bus := event.NewBus()
	event.SetDefault(bus)
	t.Cleanup(func() { event.SetDefault(prev) })

	type got struct {
		topic   string
		payload interface{}
	}
	var events []got
	for _, topic := range []string{
		event.EventArticleStatusChanged,
		event.EventCommentStatusChanged,
		event.EventUserAvatarAuditBlocked,
	} {
		tp := topic
		bus.Subscribe(tp, func(p interface{}) {
			events = append(events, got{topic: tp, payload: p})
		})
	}

	svc := NewAuditService(nil, nil)
	cases := []struct {
		name      string
		rec       model.AuditRecord
		wantTopic string
		wantTo    string // 空表示「不应发事件」
	}{
		{"文章-拦截 → REJECTED", model.AuditRecord{BizType: "article", BizID: "a1", Status: "BLOCKED"},
			event.EventArticleStatusChanged, "REJECTED"},
		{"文章-复审 → REVIEW", model.AuditRecord{BizType: "article", BizID: "a2", Status: "REVIEW"},
			event.EventArticleStatusChanged, "REVIEW"},
		{"文章-通过 → PUBLISHED", model.AuditRecord{BizType: "article", BizID: "a3", Status: "PASSED"},
			event.EventArticleStatusChanged, "PUBLISHED"},
		{"评论-拦截 → BLOCKED", model.AuditRecord{BizType: "comment", BizID: "c1", Status: "BLOCKED"},
			event.EventCommentStatusChanged, "BLOCKED"},
		{"评论-通过 → PUBLISHED", model.AuditRecord{BizType: "comment", BizID: "c2", Status: "PASSED"},
			event.EventCommentStatusChanged, "PUBLISHED"},
		{"头像-拦截 → 发通知事件", model.AuditRecord{BizType: "user_avatar", BizID: "u1", Status: "BLOCKED"},
			event.EventUserAvatarAuditBlocked, ""},
		{"头像-通过 → 不发事件", model.AuditRecord{BizType: "user_avatar", BizID: "u2", Status: "PASSED"}, "", ""},
		{"未知 bizType → 不发事件", model.AuditRecord{BizType: "video", BizID: "v1", Status: "BLOCKED"}, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			events = nil
			rec := c.rec
			svc.emitStatusChanged(&rec)

			if c.wantTopic == "" {
				if len(events) != 0 {
					t.Fatalf("不应发事件，实际 %+v", events)
				}
				return
			}
			if len(events) != 1 {
				t.Fatalf("应发 1 个事件，实际 %d: %+v", len(events), events)
			}
			if events[0].topic != c.wantTopic {
				t.Fatalf("事件名 = %q, want %q", events[0].topic, c.wantTopic)
			}
			if c.wantTo != "" {
				p, ok := events[0].payload.(event.ArticleStatusChangedPayload)
				if ok {
					if p.To != c.wantTo {
						t.Fatalf("文章目标状态 = %q, want %q", p.To, c.wantTo)
					}
					if p.ArticleID != c.rec.BizID {
						t.Fatalf("ArticleID = %q, want %q", p.ArticleID, c.rec.BizID)
					}
					return
				}
				cp, ok := events[0].payload.(event.CommentStatusChangedPayload)
				if !ok {
					t.Fatalf("未知载荷类型 %T", events[0].payload)
				}
				if cp.To != c.wantTo {
					t.Fatalf("评论目标状态 = %q, want %q", cp.To, c.wantTo)
				}
			}
		})
	}
}

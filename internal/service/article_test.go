package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// ---------- StringList：images 单值 / 数组 / 空值三态 ----------

// TestStringListUnmarshal 防的 bug：前端传 "http://a.jpg"（单串）时接口 400。
// 原 DTO 定义 oneOf[string, array]，Go 侧必须两种都能吃。
func TestStringListUnmarshal(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"数组形态", `["a.jpg","b.jpg"]`, []string{"a.jpg", "b.jpg"}},
		{"单串形态", `"a.jpg"`, []string{"a.jpg"}},
		{"空串", `""`, nil},
		{"null", `null`, nil},
		{"空数组", `[]`, []string{}},
		{"数组含空串", `["a.jpg",""]`, []string{"a.jpg", ""}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got StringList
			if err := json.Unmarshal([]byte(c.raw), &got); err != nil {
				t.Fatalf("Unmarshal(%s) 报错：%v", c.raw, err)
			}
			if !reflect.DeepEqual([]string(got), c.want) && !(len(got) == 0 && len(c.want) == 0) {
				t.Errorf("Unmarshal(%s) = %v, want %v", c.raw, []string(got), c.want)
			}
		})
	}
}

// TestStringListToJSONText 防的 bug：空列表写成 "[]" 占位，导致库内出现伪数据；
// 空列表必须落 NULL。
func TestStringListToJSONText(t *testing.T) {
	if got := (StringList{}).ToJSONText(); got != nil {
		t.Errorf("空列表应返回 nil（落库 NULL），实际 %q", *got)
	}
	if got := (StringList(nil)).ToJSONText(); got != nil {
		t.Errorf("nil 应返回 nil，实际 %q", *got)
	}
	got := StringList{"a.jpg", "b.jpg"}.ToJSONText()
	if got == nil || *got != `["a.jpg","b.jpg"]` {
		t.Errorf("非空列表序列化错误：%v", got)
	}
}

// TestParseImages 防的 bug：历史数据里 images 是裸 URL 而非 JSON 数组时，
// 详情接口把整段字符串当数组返回，前端渲染崩溃。
func TestParseImages(t *testing.T) {
	jsonArr := `["a.jpg","b.jpg"]`
	bareURL := `https://example.com/x.jpg`
	empty := ""

	cases := []struct {
		name string
		raw  *string
		want interface{}
	}{
		{"nil → 空切片", nil, []string{}},
		{"空串 → 空切片", &empty, []string{}},
		{"JSON 数组 → 切片", &jsonArr, []string{"a.jpg", "b.jpg"}},
		{"裸 URL → 原样字符串", &bareURL, bareURL},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseImages(c.raw)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("ParseImages = %#v, want %#v", got, c.want)
			}
		})
	}
}

// TestParseImagesList 裸 URL 也要变成单元素切片（审核提交图片要用）。
func TestParseImagesList(t *testing.T) {
	bareURL := `https://example.com/x.jpg`
	if got := ParseImagesList(nil); len(got) != 0 {
		t.Errorf("nil 应返回空切片，实际 %v", got)
	}
	got := ParseImagesList(&bareURL)
	if len(got) != 1 || got[0] != bareURL {
		t.Errorf("裸 URL 应转为单元素切片，实际 %v", got)
	}
}

// ---------- 发布状态机 ----------

// TestResolvePublishStatus 防的 bug：接入审核后仍直接 PUBLISHED（内容未审先上线），
// 或未接审核时把文章永久卡在 PENDING（无法发帖）。
func TestResolvePublishStatus(t *testing.T) {
	cases := []struct {
		name          string
		requested     string
		hasAuditor    bool
		wantStatus    string
		wantNeedAudit bool
	}{
		{"发布+有审核 → PENDING 并送审", model.ArticleStatusPublished, true, model.ArticleStatusPending, true},
		{"发布+无审核 → 直接 PUBLISHED", model.ArticleStatusPublished, false, model.ArticleStatusPublished, false},
		{"草稿不受审核影响", model.ArticleStatusDraft, true, model.ArticleStatusDraft, false},
		{"显式待审保持 PENDING", model.ArticleStatusPending, true, model.ArticleStatusPending, false},
		{"下架不受审核影响", model.ArticleStatusArchived, true, model.ArticleStatusArchived, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, needAudit := resolvePublishStatus(c.requested, c.hasAuditor)
			if status != c.wantStatus || needAudit != c.wantNeedAudit {
				t.Errorf("resolvePublishStatus(%q,%v) = (%q,%v), want (%q,%v)",
					c.requested, c.hasAuditor, status, needAudit, c.wantStatus, c.wantNeedAudit)
			}
		})
	}
}

// TestIsValidArticleStatusTransition 防的 bug：
//  1. 已删除文章被「复活」（违反删除语义）；
//  2. 未知状态字符串入库（脏值让列表查询失效）。
func TestIsValidArticleStatusTransition(t *testing.T) {
	cases := []struct {
		name string
		from string
		to   string
		want bool
	}{
		// 合法主链路
		{"草稿→待审", model.ArticleStatusDraft, model.ArticleStatusPending, true},
		{"草稿→发布（无审核直发）", model.ArticleStatusDraft, model.ArticleStatusPublished, true},
		{"待审→发布（审核通过）", model.ArticleStatusPending, model.ArticleStatusPublished, true},
		{"待审→驳回", model.ArticleStatusPending, model.ArticleStatusRejected, true},
		{"驳回→草稿（改稿重投）", model.ArticleStatusRejected, model.ArticleStatusDraft, true},
		{"发布→下架（作者主动下架）", model.ArticleStatusPublished, model.ArticleStatusArchived, true},
		{"发布→草稿（撤回）", model.ArticleStatusPublished, model.ArticleStatusDraft, true},
		{"下架→发布（重新上架）", model.ArticleStatusArchived, model.ArticleStatusPublished, true},
		// 同状态幂等
		{"同状态", model.ArticleStatusPublished, model.ArticleStatusPublished, true},
		// 终态与脏值
		{"已删除→发布 禁止复活", model.ArticleStatusDeleted, model.ArticleStatusPublished, false},
		{"已删除→草稿 禁止", model.ArticleStatusDeleted, model.ArticleStatusDraft, false},
		{"未知目标状态拒绝", model.ArticleStatusDraft, "WHATEVER", false},
		{"空目标状态拒绝", model.ArticleStatusDraft, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isValidArticleStatusTransition(c.from, c.to); got != c.want {
				t.Errorf("isValidArticleStatusTransition(%q→%q) = %v, want %v",
					c.from, c.to, got, c.want)
			}
		})
	}
}

// ---------- 匿名可见性 ----------

// TestFilterAnonymousVisible 防的 bug：listRequireLogin 的文章出现在未登录用户的列表里
// （内容泄露）。
func TestFilterAnonymousVisible(t *testing.T) {
	rows := []model.Article{
		{BaseModel: model.BaseModel{ID: "public"}, ListRequireLogin: false},
		{BaseModel: model.BaseModel{ID: "login-only"}, ListRequireLogin: true},
		{BaseModel: model.BaseModel{ID: "public2"}, ListRequireLogin: false},
	}
	got := filterAnonymousVisible(rows)
	if len(got) != 2 {
		t.Fatalf("应过滤掉 1 条仅登录可见，实际剩 %d 条", len(got))
	}
	for _, r := range got {
		if r.ListRequireLogin {
			t.Errorf("仅登录可见的文章 %s 未被过滤", r.ID)
		}
	}
}

// TestArticleListFromMap 防的 bug：按 ID 批量取文章后顺序错乱
// （点赞列表/收藏列表应按互动时间排序，而不是 DB 返回顺序）。
func TestArticleListFromMap(t *testing.T) {
	m := map[string]*model.Article{
		"a": {BaseModel: model.BaseModel{ID: "a"}},
		"b": {BaseModel: model.BaseModel{ID: "b"}},
		"c": {BaseModel: model.BaseModel{ID: "c"}},
	}
	got := articleListFromMap(m, []string{"c", "a", "missing", "b"})
	if len(got) != 3 {
		t.Fatalf("应返回 3 条（missing 跳过），实际 %d", len(got))
	}
	want := []string{"c", "a", "b"}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("第 %d 条应为 %s，实际 %s（顺序错乱）", i, id, got[i].ID)
		}
	}
}

// ---------- 模型唯一索引（幂等落库的根保障） ----------

// TestInteractionUniqueIndexes 防的 bug：点赞/收藏/不喜欢/评论点赞/标签关注
// 缺少 (user, 目标) 唯一索引 → 同一用户重复点赞插入两行，计数翻倍。
//
// 这是「幂等」在数据层的兜底：即使应用层并发漏判，DB 也会拒绝重复。
func TestInteractionUniqueIndexes(t *testing.T) {
	cases := []struct {
		name      string
		typ       reflect.Type
		fields    []string
		indexName string
	}{
		{"文章点赞", reflect.TypeOf(model.ArticleLike{}), []string{"ArticleID", "UserID"}, "uk_article_likes_user_article"},
		{"文章收藏", reflect.TypeOf(model.ArticleFavorite{}), []string{"UserID", "ArticleID"}, "uk_article_favorites_user_article"},
		{"不喜欢", reflect.TypeOf(model.ArticleDislike{}), []string{"UserID", "ArticleID"}, "uk_article_dislikes_user_article"},
		{"评论点赞", reflect.TypeOf(model.CommentLike{}), []string{"CommentID", "UserID"}, "uk_comment_likes_user_comment"},
		{"标签关注", reflect.TypeOf(model.TagFollow{}), []string{"UserID", "TagID"}, "uk_tag_follows_user_tag"},
		{"文章标签关联", reflect.TypeOf(model.ArticleTag{}), []string{"ArticleID", "TagID"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, fieldName := range c.fields {
				f, ok := c.typ.FieldByName(fieldName)
				if !ok {
					t.Fatalf("%s 缺少字段 %s", c.typ.Name(), fieldName)
				}
				tag := f.Tag.Get("gorm")
				if c.indexName == "" {
					if !strings.Contains(tag, "primaryKey") {
						t.Errorf("%s.%s 应为复合主键，实际 tag=%q", c.typ.Name(), fieldName, tag)
					}
					continue
				}
				if !strings.Contains(tag, "uniqueIndex:"+c.indexName) {
					t.Errorf("%s.%s 缺少唯一索引 %s（重复记录会入库，计数翻倍），实际 tag=%q",
						c.typ.Name(), fieldName, c.indexName, tag)
				}
			}
		})
	}
}

// TestBrowseHistoryUniqueIndex 防的 bug：同一用户同一文章产生多条浏览记录，
// 浏览历史列表出现重复项、viewCount 统计失真。
func TestBrowseHistoryUniqueIndex(t *testing.T) {
	typ := reflect.TypeOf(model.BrowseHistory{})
	pairs := [][2]string{{"UserID", "uk_browse_histories_user_article"}, {"ArticleID", "uk_browse_histories_user_article"}}
	for _, p := range pairs {
		f, ok := typ.FieldByName(p[0])
		if !ok {
			t.Fatalf("BrowseHistory 缺少字段 %s", p[0])
		}
		if tag := f.Tag.Get("gorm"); !strings.Contains(tag, "uniqueIndex:"+p[1]) {
			t.Errorf("BrowseHistory.%s 缺少唯一索引 %s，实际 tag=%q", p[0], p[1], tag)
		}
	}
}

// ---------- 权限判定（安全关键，纯逻辑不碰 DB） ----------

// TestActorPermissions 防的 bug：权限点匹配写成前缀/包含匹配，
// 导致 article:update 命中 article:update-all 之类的越权放行。
func TestActorPermissions(t *testing.T) {
	a := Actor{UserID: "u-1", Permissions: []string{"article:update", "comment:create"}}
	if !a.Has("article:update") {
		t.Error("已有权限应命中")
	}
	if a.Has("article:delete") {
		t.Error("未授予的权限不得命中")
	}
	if a.Has("article") {
		t.Error("前缀不算命中（必须全等）")
	}
	if a.Has("article:update:extra") {
		t.Error("超集不算命中")
	}
	if !a.HasAny("nope", "comment:create") {
		t.Error("HasAny 任一命中即 true")
	}
	if a.HasAny("nope", "nope2") {
		t.Error("HasAny 全不命中应 false")
	}
	if a.Anonymous() {
		t.Error("有 UserID 不应是匿名")
	}
	if !(Actor{}).Anonymous() {
		t.Error("空 UserID 应视为匿名")
	}
}

// TestActorIsManager 防的 bug：普通用户被误判为管理员，
// 从而能设精/改他人文章/看未审稿。
func TestActorIsManager(t *testing.T) {
	cases := []struct {
		name  string
		perms []string
		want  bool
	}{
		{"普通用户", []string{"article:create", "comment:create"}, false},
		{"设精权限 → 管理员", []string{PermArticleFeatured}, true},
		{"评论管理 → 管理员", []string{PermCommentManage}, true},
		{"标签管理 → 管理员", []string{PermTagUpdate}, true},
		{"分类管理 → 管理员", []string{PermCategoryUpdate}, true},
		{"admin 通配", []string{"admin"}, true},
		{"super_admin", []string{"super_admin"}, true},
		{"无权限", nil, false},
		// 关键负向：普通业务权限不得被当成管理权限
		{"article:update 不是管理员", []string{PermArticleUpdate}, false},
		{"article:delete 不是管理员", []string{PermArticleDelete}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := (Actor{Permissions: c.perms}).IsManager(); got != c.want {
				t.Errorf("IsManager(%v) = %v, want %v", c.perms, got, c.want)
			}
		})
	}
}

// TestCanModify 防的 bug：非作者修改/删除他人文章（IDOR 越权）。
func TestCanModify(t *testing.T) {
	authorID := "author-1"
	other := "user-2"
	a := &model.Article{BaseModel: model.BaseModel{ID: "art-1"}, AuthorID: &authorID}

	svc := &ArticleService{} // canModify 不碰 repo，可直接构造

	if !svc.canModify(a, Actor{UserID: "author-1"}) {
		t.Error("作者本人应可修改")
	}
	if svc.canModify(a, Actor{UserID: other}) {
		t.Error("非作者不得修改")
	}
	if svc.canModify(a, Actor{}) {
		t.Error("匿名不得修改")
	}
	if !svc.canModify(a, Actor{UserID: other, Permissions: []string{PermArticleFeatured}}) {
		t.Error("管理员应可修改他人文章")
	}

	// 无作者（系统文章）：任何人都不是作者，仅管理员可改
	orphan := &model.Article{BaseModel: model.BaseModel{ID: "art-2"}}
	if svc.canModify(orphan, Actor{UserID: "anyone"}) {
		t.Error("无作者文章不得被普通用户修改")
	}
	if !svc.canModify(orphan, Actor{UserID: "admin", Permissions: []string{"admin"}}) {
		t.Error("管理员应可修改无作者文章")
	}
}

// TestVisibleStatuses 防的 bug：普通用户传 ?status=PENDING 就能看到全站待审稿。
//
// 规则：只有管理员能用 status 参数筛选；其他人一律只看 PUBLISHED。
func TestVisibleStatuses(t *testing.T) {
	svc := &ArticleService{}

	admin := Actor{UserID: "admin", Permissions: []string{PermArticleFeatured}}
	normal := Actor{UserID: "u-1"}
	anon := Actor{}

	if got := svc.visibleStatuses(admin, []string{model.ArticleStatusPending}); len(got) != 1 || got[0] != model.ArticleStatusPending {
		t.Errorf("管理员应可用 status 筛选，实际 %v", got)
	}
	for _, actor := range []Actor{normal, anon} {
		got := svc.visibleStatuses(actor, []string{model.ArticleStatusPending, model.ArticleStatusRejected})
		if len(got) != 1 || got[0] != model.ArticleStatusPublished {
			t.Errorf("非管理员必须只看到 PUBLISHED，实际 %v", got)
		}
	}
}

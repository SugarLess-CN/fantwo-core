// 内容审核服务：双 provider（阿里云 green20220302 / 腾讯云 CMS）+ asynq 异步管线
// + 人工复审，对齐原 content-audit 模块。
//
// 架构：
//   - 提交方（article/comment/upload 等域）调 SubmitText/SubmitImage 入队，
//     不感知 provider；
//   - asynq worker（见 internal/queue/audit.go）消费后调 AuditText/AuditImage；
//   - 结果只写 audit_records，跨域状态回写一律通过 event 总线
//     （article.status.changed / comment.status.changed），由 content 域监听；
//   - provider 与场景开关从 config 表读取，事件触发重载。
package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/queue"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

// AuditProviderName provider 标识。
const (
	AuditProviderNone    = "none"
	AuditProviderAliyun  = "aliyun"
	AuditProviderTencent = "tencent"
)

// AuditConfig 审核配置（config 表聚合）。
type AuditConfig struct {
	Provider       string `json:"provider"`
	CommentEnabled bool   `json:"commentEnabled"`
	AvatarEnabled  bool   `json:"avatarEnabled"`
	ImageEnabled   bool   `json:"imageEnabled"`
	ArticleEnabled bool   `json:"articleEnabled"`
	AutoBlock      bool   `json:"autoBlock"`
	ReviewMode     string `json:"reviewMode"`  // auto / manual
	Sensitivity    string `json:"sensitivity"` // low / medium / high（对齐原仓库默认 medium）
	// Tencent 腾讯云 CMS
	TencentSecretID  string `json:"tencentSecretId"`
	TencentSecretKey string `json:"-"`
	TencentRegion    string `json:"tencentRegion"`
	TencentTextBiz   string `json:"tencentTextBiz"`
	TencentImageBiz  string `json:"tencentImageBiz"`
	// Aliyun 阿里云 green
	AliyunAccessKeyID     string `json:"aliyunAccessKeyId"`
	AliyunAccessKeySecret string `json:"-"`
	AliyunRegion          string `json:"aliyunRegion"`
}

// AuditResult 统一审核结果。
type AuditResult struct {
	Passed     bool    `json:"passed"`
	Action     string  `json:"action"` // pass / review / block
	Label      string  `json:"label"`
	Reason     string  `json:"reason"`
	Score      float64 `json:"score"`
	DetailJSON string  `json:"detailJson"`
}

// AuditService 审核业务。
type AuditService struct {
	DB    *gorm.DB
	asynq *asynq.Client
	// sensitive 本地敏感词检查（返回命中的词与是否命中）。
	// 与云 provider 相互独立：即使 provider=none，本地敏感词也必须生效。
	sensitive func(text string) (string, bool)
}

// NewAuditService 构造。
func NewAuditService(db *gorm.DB, c *asynq.Client) *AuditService {
	return &AuditService{DB: db, asynq: c}
}

// SetSensitiveChecker 注入本地敏感词检查（由装配层传 infra.ConfigService.CheckText）。
func (s *AuditService) SetSensitiveChecker(fn func(text string) (string, bool)) *AuditService {
	s.sensitive = fn
	return s
}

// sensitiveResult 命中敏感词时的结果构造（纯函数，便于表驱动单测）。
//
// 语义（对齐原 content-audit：autoBlock 决定是硬拦截还是转人工）：
//   - autoBlock = true  → action=block，passed=false（直接拦下）
//   - autoBlock = false → action=review，passed=true（放行但进人工复审队列）
//
// 防什么 bug：autoBlock 判断反了会导致「配置为拦截却只进复审」（违规内容直接上线），
// 或「配置为复审却直接拦截」（误封正常内容）。
func sensitiveResult(word string, autoBlock bool) AuditResult {
	action := model.AuditActionReview
	if autoBlock {
		action = model.AuditActionBlock
	}
	return AuditResult{
		// review 是放行 + 转人工，只有 block 才拦截；
		// （单测曾抓到写成 action == pass 的 bug：review 被误判为拦截，正常内容被拦下）
		Passed: action != model.AuditActionBlock,
		Action: action,
		Label:  "sensitive_word",
		Reason: "命中本地敏感词：" + word,
	}
}

// checkSensitive 本地敏感词检查：命中即按 autoBlock 决定 block / review。
//
// DB 不可用（降级启动 / 单测替身）时按 autoBlock=true 处理：宁可拦下也不放行，
// 避免「配置读取失败 → 敏感词失效」的静默放过。
func (s *AuditService) checkSensitive(content string) (AuditResult, bool) {
	if s.sensitive == nil || content == "" {
		return AuditResult{}, false
	}
	word, hit := s.sensitive(content)
	if !hit {
		return AuditResult{}, false
	}
	autoBlock := true
	if s.DB != nil {
		autoBlock = s.LoadAuditConfig().AutoBlock
	}
	return sensitiveResult(word, autoBlock), true
}

// LoadAuditConfig 从 config 表读审核配置（key 对齐原仓库 content_audit_*）。
func (s *AuditService) LoadAuditConfig() AuditConfig {
	get := func(key, def string) string {
		var c model.ConfigKV
		if err := s.DB.First(&c, "\"key\" = ?", key).Error; err != nil || c.Value == "" {
			return def
		}
		return c.Value
	}
	return parseAuditConfig(get)
}

// parseAuditConfig 把 key→value 的读取结果组装成 AuditConfig（纯函数，便于单测）。
//
// getter 由 LoadAuditConfig 注入：真实现查 config 表（key 查不到或值为空串时回落 def），
// 单测注入 map 桩，因此本函数不依赖 DB，可做纯函数级表驱动测试。
func parseAuditConfig(get func(key, def string) string) AuditConfig {
	boolv := func(key string) bool { return get(key, "false") == "true" }
	return AuditConfig{
		Provider:              get("content_audit_provider", AuditProviderNone),
		CommentEnabled:        boolv("content_audit_comment_enabled"),
		AvatarEnabled:         boolv("content_audit_avatar_enabled"),
		ImageEnabled:          boolv("content_audit_image_enabled"),
		ArticleEnabled:        boolv("content_audit_article_enabled"),
		AutoBlock:             get("content_audit_auto_block", "true") == "true",
		ReviewMode:            get("content_audit_review_mode", "auto"),
		Sensitivity:           get(model.ConfigKeyAuditSensitivity, "medium"),
		TencentSecretID:       get("tencent_secret_id", ""),
		TencentSecretKey:      get("tencent_secret_key", ""),
		TencentRegion:         get("tencent_region", "ap-beijing"),
		TencentTextBiz:        get("tencent_text_biz_type", ""),
		TencentImageBiz:       get("tencent_image_biz_type", ""),
		AliyunAccessKeyID:     get("aliyun_access_key_id", ""),
		AliyunAccessKeySecret: get("aliyun_access_key_secret", ""),
		AliyunRegion:          get("aliyun_region", "cn-beijing"),
	}
}

// sceneEnabled 场景开关。
func (c *AuditConfig) sceneEnabled(scene string) bool {
	if c.Provider == AuditProviderNone {
		return false
	}
	switch scene {
	case model.AuditSceneComment:
		return c.CommentEnabled
	case model.AuditSceneAvatar:
		return c.AvatarEnabled
	case model.AuditSceneImage:
		return c.ImageEnabled
	case model.AuditSceneArticle:
		return c.ArticleEnabled
	}
	return false
}

// ---------- provider 实现 ----------

// aliyunTextService 文本场景 → 阿里云 service（对齐原 aliyun-audit.service.ts）。
func aliyunTextService(scene string) string {
	switch scene {
	case "nickname":
		return "nickname_detection"
	case "chat":
		return "chat_detection"
	case model.AuditSceneArticle:
		return "pgc_detection"
	default:
		return "comment_detection"
	}
}

// aliyunImageService 图片场景 → 阿里云 service。
func aliyunImageService(scene string) string {
	if scene == model.AuditSceneAvatar {
		return "profilePhotoCheck"
	}
	return "baselineCheck"
}

// AuditText 同步文本审核（provider 路由 + 场景开关；未启用直接放行）。
func (s *AuditService) AuditText(scene, content string, userID string) AuditResult {
	// 本地敏感词优先（与云 provider 无关，provider=none 时也生效）
	if r, hit := s.checkSensitive(content); hit {
		return r
	}
	cfg := s.LoadAuditConfig()
	if !cfg.sceneEnabled(scene) {
		return AuditResult{Passed: true, Action: model.AuditActionPass}
	}
	switch cfg.Provider {
	case AuditProviderAliyun:
		return s.aliyunText(&cfg, scene, content, userID)
	case AuditProviderTencent:
		return s.tencentText(&cfg, scene, content, userID)
	}
	return AuditResult{Passed: true, Action: model.AuditActionPass}
}

// AuditImage 同步图片审核。
func (s *AuditService) AuditImage(scene, imageURL string, userID string) AuditResult {
	cfg := s.LoadAuditConfig()
	checkScene := scene
	if scene != model.AuditSceneAvatar && scene != model.AuditSceneArticle {
		checkScene = model.AuditSceneImage
	}
	if !cfg.sceneEnabled(checkScene) {
		return AuditResult{Passed: true, Action: model.AuditActionPass}
	}
	switch cfg.Provider {
	case AuditProviderAliyun:
		return s.aliyunImage(&cfg, scene, imageURL, userID)
	case AuditProviderTencent:
		return s.tencentImage(&cfg, scene, imageURL, userID)
	}
	return AuditResult{Passed: true, Action: model.AuditActionPass}
}

// aliyunText 阿里云 green20220302 TextModeration（官方 SDK）。
func (s *AuditService) aliyunText(cfg *AuditConfig, scene, content, userID string) AuditResult {
	if cfg.AliyunAccessKeyID == "" || cfg.AliyunAccessKeySecret == "" {
		return AuditResult{Passed: true, Action: model.AuditActionPass}
	}
	res := AuditResult{Passed: true, Action: model.AuditActionPass}
	raw, err := queue.AliyunTextModeration(cfg.AliyunAccessKeyID, cfg.AliyunAccessKeySecret, cfg.AliyunRegion,
		aliyunTextService(scene), content)
	if err != nil {
		return res // provider 异常放行（对齐原仓库 catch 语义）
	}
	res.DetailJSON = raw
	var body struct {
		Data struct {
			Labels string `json:"labels"`
			Reason string `json:"reason"`
		} `json:"data"`
	}
	_ = json.Unmarshal([]byte(raw), &body)
	if body.Data.Reason != "" {
		var reason struct {
			RiskLevel string `json:"riskLevel"`
		}
		if json.Unmarshal([]byte(body.Data.Reason), &reason) == nil && reason.RiskLevel == "high" {
			res.Passed = false
			res.Action = model.AuditActionBlock
			res.Reason = body.Data.Labels
		}
	}
	if body.Data.Labels != "" && body.Data.Labels != "nonLabel" {
		res.Label = body.Data.Labels
		if res.Action == model.AuditActionPass {
			res.Action = model.AuditActionReview
		}
	}
	return res
}

// aliyunImage 阿里云 ImageModeration。
func (s *AuditService) aliyunImage(cfg *AuditConfig, scene, imageURL, userID string) AuditResult {
	if cfg.AliyunAccessKeyID == "" || cfg.AliyunAccessKeySecret == "" {
		return AuditResult{Passed: true, Action: model.AuditActionPass}
	}
	res := AuditResult{Passed: true, Action: model.AuditActionPass}
	raw, err := queue.AliyunImageModeration(cfg.AliyunAccessKeyID, cfg.AliyunAccessKeySecret, cfg.AliyunRegion,
		aliyunImageService(scene), imageURL)
	if err != nil {
		return res
	}
	res.DetailJSON = raw
	var body struct {
		Data struct {
			RiskLevel string  `json:"riskLevel"`
			Score     float64 `json:"score"`
			Result    []struct {
				Label string `json:"label"`
			} `json:"result"`
		} `json:"data"`
	}
	_ = json.Unmarshal([]byte(raw), &body)
	if body.Data.RiskLevel == "high" {
		res.Passed = false
		res.Action = model.AuditActionBlock
		res.Score = body.Data.Score
		if len(body.Data.Result) > 0 {
			res.Label = body.Data.Result[0].Label
		}
	}
	return res
}

// tencentText 腾讯云 TMS TextModeration（官方 SDK）。
func (s *AuditService) tencentText(cfg *AuditConfig, scene, content, userID string) AuditResult {
	if cfg.TencentSecretID == "" || cfg.TencentSecretKey == "" {
		return AuditResult{Passed: true, Action: model.AuditActionPass}
	}
	res := AuditResult{Passed: true, Action: model.AuditActionPass}
	raw, err := queue.TencentTextModeration(cfg.TencentSecretID, cfg.TencentSecretKey, cfg.TencentRegion,
		cfg.TencentTextBiz, content, userID)
	if err != nil {
		return res
	}
	res.DetailJSON = raw
	var body struct {
		Suggestion string  `json:"Suggestion"`
		Label      string  `json:"Label"`
		Score      float64 `json:"Score"`
	}
	_ = json.Unmarshal([]byte(raw), &body)
	res.Label = body.Label
	res.Score = body.Score
	switch lower(body.Suggestion) {
	case "block":
		res.Passed = false
		res.Action = model.AuditActionBlock
	case "review":
		res.Action = model.AuditActionReview
	}
	return res
}

// tencentImage 腾讯云 IMS ImageModeration。
func (s *AuditService) tencentImage(cfg *AuditConfig, scene, imageURL, userID string) AuditResult {
	if cfg.TencentSecretID == "" || cfg.TencentSecretKey == "" {
		return AuditResult{Passed: true, Action: model.AuditActionPass}
	}
	res := AuditResult{Passed: true, Action: model.AuditActionPass}
	raw, err := queue.TencentImageModeration(cfg.TencentSecretID, cfg.TencentSecretKey, cfg.TencentRegion,
		cfg.TencentImageBiz, imageURL, userID)
	if err != nil {
		return res
	}
	res.DetailJSON = raw
	var body struct {
		Suggestion string  `json:"Suggestion"`
		Label      string  `json:"Label"`
		Score      float64 `json:"Score"`
	}
	_ = json.Unmarshal([]byte(raw), &body)
	res.Label = body.Label
	res.Score = body.Score
	switch lower(body.Suggestion) {
	case "block":
		res.Passed = false
		res.Action = model.AuditActionBlock
	case "review":
		res.Action = model.AuditActionReview
	}
	return res
}

func lower(s string) string {
	out := []byte(s)
	for i := range out {
		if out[i] >= 'A' && out[i] <= 'Z' {
			out[i] += 'a' - 'A'
		}
	}
	return string(out)
}

// ---------- 异步提交（asynq） ----------

// SubmitTextTask 提交文本审核任务（image-audit/text-audit 队列；重试 5 次指数退避由 asynq 配置）。
func (s *AuditService) SubmitTextTask(scene, bizType, bizID, content, operatorID string) (*model.AuditRecord, error) {
	record := &model.AuditRecord{
		Scene: scene, BizType: bizType, BizID: bizID,
		OperatorID: operatorID, Content: content,
		Provider: s.LoadAuditConfig().Provider, Status: "PENDING",
	}
	if err := s.DB.Create(record).Error; err != nil {
		return nil, err
	}
	if s.asynq == nil {
		return record, nil // 队列降级：记录留存，不投递
	}
	task, err := queue.NewTextAuditTask(record.ID)
	if err != nil {
		return nil, err
	}
	info, err := s.asynq.Enqueue(task, asynq.Queue(queue.QueueTextAudit), asynq.MaxRetry(5), asynq.TaskID(record.ID))
	if err != nil {
		// TaskID 冲突（重复提交）视为已入队
		if !errors.Is(err, asynq.ErrTaskIDConflict) {
			return nil, err
		}
	}
	_ = info
	return record, nil
}

// SubmitImageTask 提交图片审核任务。
func (s *AuditService) SubmitImageTask(scene, bizType, bizID, imageURL, operatorID string) (*model.AuditRecord, error) {
	record := &model.AuditRecord{
		Scene: scene, BizType: bizType, BizID: bizID,
		OperatorID: operatorID, ImageURL: imageURL,
		Provider: s.LoadAuditConfig().Provider, Status: "PENDING",
	}
	if err := s.DB.Create(record).Error; err != nil {
		return nil, err
	}
	if s.asynq == nil {
		return record, nil
	}
	task, err := queue.NewImageAuditTask(record.ID)
	if err != nil {
		return nil, err
	}
	if _, err := s.asynq.Enqueue(task, asynq.Queue(queue.QueueImageAudit), asynq.MaxRetry(5), asynq.TaskID(record.ID)); err != nil {
		if !errors.Is(err, asynq.ErrTaskIDConflict) {
			return nil, err
		}
	}
	return record, nil
}

// ---------- worker 回调（queue/audit.go 处理器调用） ----------

// resolveMsgID 计算队列幂等键：msg_id 为空时回退审核记录 ID。
//
// 2026-11-19 事故：msg_id 用 string 零值导致多条记录都为空串，
// 撞 uniqueIndex（SQLSTATE 23505），只有第一条审核记录能建。
// 这里做第二道兜底（第一道在 queue.handleAuditTask 的 TaskID 回退），
// 保证进入去重表的 msg_id 永不为空串。
func resolveMsgID(msgID, recordID string) string {
	if msgID != "" {
		return msgID
	}
	return recordID
}

// ProcessAuditRecord 消费端入口：msg_id 去重 → 调 provider → 写结果 → 跨域事件。
func (s *AuditService) ProcessAuditRecord(recordID, msgID string) error {
	// 幂等 1：msg_id 去重表（空 msg_id 一律回退 recordID，见 resolveMsgID）
	msgID = resolveMsgID(msgID, recordID)
	var cnt int64
	if err := s.DB.Model(&model.QueueMessage{}).Where("msg_id = ?", msgID).Count(&cnt).Error; err != nil {
		return err
	}
	if cnt > 0 {
		return nil
	}

	var record model.AuditRecord
	if err := s.DB.First(&record, "id = ?", recordID).Error; err != nil {
		return fmt.Errorf("审核记录不存在: %w", err)
	}
	// 幂等 2：已出结果直接跳过
	if record.Status != "PENDING" {
		return nil
	}

	var result AuditResult
	if record.ImageURL != "" {
		result = s.AuditImage(record.Scene, record.ImageURL, record.OperatorID)
	} else {
		result = s.AuditText(record.Scene, record.Content, record.OperatorID)
	}

	now := time.Now()
	record.Provider = s.LoadAuditConfig().Provider
	record.Action = result.Action
	record.Label = result.Label
	record.Reason = result.Reason
	record.Score = result.Score
	record.AuditedAt = &now
	if result.DetailJSON != "" {
		_ = json.Unmarshal([]byte(result.DetailJSON), &record.ResultDetail)
	}
	switch {
	case result.Action == model.AuditActionBlock:
		record.Status = "BLOCKED"
	case result.Action == model.AuditActionReview:
		record.Status = "REVIEW"
	default:
		record.Status = "PASSED"
	}
	if err := s.DB.Save(&record).Error; err != nil {
		return err
	}
	if err := s.DB.Create(&model.QueueMessage{MsgID: msgID, Queue: s.queueFor(&record)}).Error; err != nil {
		return err
	}

	// 跨域状态回写：只发事件，不写 content 域表
	s.emitStatusChanged(&record)
	return nil
}

// queueFor 记录对应队列名。
func (s *AuditService) queueFor(r *model.AuditRecord) string {
	if r.ImageURL != "" {
		return queue.QueueImageAudit
	}
	return queue.QueueTextAudit
}

// emitStatusChanged 按场景发布状态变更事件（content 域监听回写）。
func (s *AuditService) emitStatusChanged(r *model.AuditRecord) {
	blocked := r.Status == "BLOCKED"
	switch r.BizType {
	case "article":
		to := "PUBLISHED"
		if blocked {
			to = "REJECTED"
		} else if r.Status == "REVIEW" {
			to = "REVIEW"
		}
		event.Default().Publish(event.EventArticleStatusChanged, event.ArticleStatusChangedPayload{
			ArticleID: r.BizID, From: "PENDING", To: to, Reason: r.Reason, Source: model.AuditSourceAuto,
		})
	case "comment":
		to := "PUBLISHED"
		if blocked {
			to = "BLOCKED"
		}
		event.Default().Publish(event.EventCommentStatusChanged, event.CommentStatusChangedPayload{
			CommentID: r.BizID, From: "PENDING", To: to, Reason: r.Reason, Source: model.AuditSourceAuto,
		})
	case "user_avatar":
		if blocked {
			event.Default().Publish(event.EventUserAvatarAuditBlocked, map[string]string{
				"user_id": r.BizID, "reason": r.Reason,
			})
		}
	}
}

// ---------- 人工复审（管理端） ----------

// ListPendingArticles 待审核文章列表（articles 表局部视图）。
// PendingArticleItem 待审核文章条目（对外 camelCase）。
//
// 此前直接用 []map[string]interface{} 查询，Find 会原样返回数据库列名
// （author_id / created_at），违反全局 camelCase 口径，故改为显式结构映射。
type PendingArticleItem struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	AuthorID   string    `json:"authorId"`
	CategoryID *string   `json:"categoryId"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
}

// ListPendingArticles 待审核文章分页。
func (s *AuditService) ListPendingArticles(page, limit int) ([]PendingArticleItem, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var total int64
	if err := s.DB.Table(model.Article{}.TableName()).Where("status = ?", "PENDING").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	out := make([]PendingArticleItem, 0, limit)
	err := s.DB.Table(model.Article{}.TableName()).
		Select("id, title, author_id, category_id, status, created_at").
		Where("status = ?", "PENDING").
		Order("created_at DESC").
		Offset((page - 1) * limit).Limit(limit).
		Scan(&out).Error
	return out, total, err
}

// ReviewArticleInput 人工复审入参。
type ReviewArticleInput struct {
	Result string `json:"result" binding:"required,oneof=APPROVED REJECTED"`
	Reason string `json:"reason"`
}

// ReviewArticle 人工复审：只发 article.status.changed 事件（content 域负责落库与计数）。
func (s *AuditService) ReviewArticle(articleID string, in ReviewArticleInput, operatorID string) (map[string]interface{}, error) {
	// 用实体查询而非 First(&map)：GORM 对 map 目标的 First 语义不可靠，
	// 曾导致「文章存在却返回 404 文章不存在」。
	var art model.Article
	if err := s.DB.Select("id", "status").First(&art, "id = ?", articleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.ErrNotFound.With("文章不存在")
		}
		return nil, err
	}
	status := art.Status
	if status != "PENDING" {
		return nil, errcode.ErrBadRequest.With("该文章不处于待审核状态")
	}
	to := "REJECTED"
	if in.Result == "APPROVED" {
		to = "PUBLISHED"
	}
	event.Default().Publish(event.EventArticleStatusChanged, event.ArticleStatusChangedPayload{
		ArticleID: articleID, From: status, To: to, Reason: in.Reason,
		Source: model.AuditSourceManual, OperatorID: operatorID,
	})
	return map[string]interface{}{
		"articleId": articleID, "result": in.Result, "reason": in.Reason,
		"reviewedBy": operatorID, "reviewedAt": time.Now(),
	}, nil
}

// GetAuditConfig 获取审核配置（GET /content-audit/config）。
func (s *AuditService) GetAuditConfig() AuditConfig { return s.LoadAuditConfig() }

// TestAuditText 测试文本审核（POST /content-audit/text）。
func (s *AuditService) TestAuditText(content string) AuditResult {
	return s.AuditText("", content, "")
}

// TestAuditImage 测试图片审核（POST /content-audit/image）。
func (s *AuditService) TestAuditImage(url string) AuditResult {
	return s.AuditImage("", url, "")
}

// RegisterQueueHandlers 把审核任务处理器挂到 asynq mux（main 启动 worker 时调用）。
// 处理器内部按 msg_id 去重（queue_messages 表），重试安全。
func (s *AuditService) RegisterQueueHandlers(mux *asynq.ServeMux) {
	queue.RegisterAuditHandlers(mux, queue.AuditWorkerDeps{ProcessRecord: s.ProcessAuditRecord})
}

// SubmitArticleAudit 文章提交审核（content 域调用：仅入队，结果经 article.status.changed 事件回写）。
func (s *AuditService) SubmitArticleAudit(articleID, content string, imageURLs []string) error {
	if content != "" {
		if _, err := s.SubmitTextTask(model.AuditSceneArticle, "article", articleID, content, ""); err != nil {
			return err
		}
	}
	for _, u := range imageURLs {
		if u == "" {
			continue
		}
		if _, err := s.SubmitImageTask(model.AuditSceneArticle, "article", articleID, u, ""); err != nil {
			return err
		}
	}
	return nil
}

// SubmitCommentAudit 评论提交审核（content 域调用）。
func (s *AuditService) SubmitCommentAudit(commentID, content string) error {
	_, err := s.SubmitTextTask(model.AuditSceneComment, "comment", commentID, content, "")
	return err
}

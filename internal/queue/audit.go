// 审核队列：asynq 任务构造 + provider 调用 + worker 处理器注册。
//
// 队列/并发/重试对齐原 Bull：image-audit 并发 5、text-audit 并发 10、
// 重试 5 次指数退避（见 queue.go 服务端配置与 handler 提交侧 MaxRetry(5)）。
package queue

import (
	"context"
	"encoding/base64"
	"encoding/json"

	"github.com/hibiken/asynq"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	ims "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ims/v20201229"
	tms "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tms/v20201229"

	openapiutil "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	green "github.com/alibabacloud-go/green-20220302/v2/client"
	"github.com/redis/go-redis/v9"
)

// FromRedisClient 由已有 go-redis 客户端派生 asynq 客户端（复用同一 Redis 连接配置）。
// rdb 为 nil（Redis 不可用）时返回 nil，调用方按队列降级处理。
func FromRedisClient(rdb *redis.Client) *asynq.Client {
	if rdb == nil {
		return nil
	}
	o := rdb.Options()
	return asynq.NewClient(asynq.RedisClientOpt{Addr: o.Addr, Password: o.Password, DB: o.DB})
}

// 审核任务负载。
type auditPayload struct {
	RecordID string `json:"record_id"`
}

// NewTextAuditTask 构造文本审核任务（TaskID = audit record id，天然防重复入队）。
func NewTextAuditTask(recordID string) (*asynq.Task, error) {
	payload, err := json.Marshal(auditPayload{RecordID: recordID})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeTextAudit, payload), nil
}

// NewImageAuditTask 构造图片审核任务。
func NewImageAuditTask(recordID string) (*asynq.Task, error) {
	payload, err := json.Marshal(auditPayload{RecordID: recordID})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeImageAudit, payload), nil
}

// AuditWorkerDeps worker 依赖（由 main 装配注入，避免 queue 反向依赖 service 包级循环）。
type AuditWorkerDeps struct {
	// ProcessRecord 审核 service 的处理入口（recordID, msgID）。
	ProcessRecord func(recordID, msgID string) error
}

// RegisterAuditHandlers 注册审核任务处理器（asynq mux；main 启动 worker 时调用）。
func RegisterAuditHandlers(mux *asynq.ServeMux, deps AuditWorkerDeps) {
	mux.HandleFunc(TypeTextAudit, func(ctx context.Context, t *asynq.Task) error {
		return handleAuditTask(t, deps)
	})
	mux.HandleFunc(TypeImageAudit, func(ctx context.Context, t *asynq.Task) error {
		return handleAuditTask(t, deps)
	})
}

// handleAuditTask 统一处理：解析负载 → msg_id 幂等 → 调 service。
func handleAuditTask(t *asynq.Task, deps AuditWorkerDeps) error {
	var p auditPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return err // 负载损坏：不再重试（返回 error 走死信）
	}
	if deps.ProcessRecord == nil {
		return nil
	}
	// asynq msg id 即幂等键；低版本无 MsgID 时回退 record_id
	msgID := t.ResultWriter().TaskID()
	if msgID == "" {
		msgID = p.RecordID
	}
	return deps.ProcessRecord(p.RecordID, msgID)
}

// ---------- provider 直调封装（官方 SDK，service 层经此隔离） ----------

// AliyunTextModeration 阿里云 green20220302 TextModeration，返回原始 JSON。
func AliyunTextModeration(accessKeyID, accessKeySecret, region, service, content string) (string, error) {
	cli, err := aliyunClient(accessKeyID, accessKeySecret, region)
	if err != nil {
		return "", err
	}
	req := &green.TextModerationRequest{
		Service:           strPtr(service),
		ServiceParameters: strPtr(mustJSON(map[string]string{"content": content})),
	}
	resp, err := cli.TextModeration(req)
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(resp.Body)
	return string(raw), nil
}

// AliyunImageModeration 阿里云 ImageModeration，返回原始 JSON。
func AliyunImageModeration(accessKeyID, accessKeySecret, region, service, imageURL string) (string, error) {
	cli, err := aliyunClient(accessKeyID, accessKeySecret, region)
	if err != nil {
		return "", err
	}
	req := &green.ImageModerationRequest{
		Service:           strPtr(service),
		ServiceParameters: strPtr(mustJSON(map[string]string{"imageUrl": imageURL})),
	}
	resp, err := cli.ImageModeration(req)
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(resp.Body)
	return string(raw), nil
}

// TencentTextModeration 腾讯云 TMS v20201229 TextModeration，返回原始 JSON。
func TencentTextModeration(secretID, secretKey, region, bizType, content, userID string) (string, error) {
	cli, err := tms.NewClient(common.NewCredential(secretID, secretKey), region, nil)
	if err != nil {
		return "", err
	}
	req := tms.NewTextModerationRequest()
	req.Content = strPtr(base64Encode(content))
	if bizType != "" {
		req.BizType = strPtr(bizType)
	}
	if userID != "" {
		req.DataId = strPtr("text:" + userID)
	}
	resp, err := cli.TextModeration(req)
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(resp)
	return string(raw), nil
}

// TencentImageModeration 腾讯云 IMS v20201229 ImageModeration，返回原始 JSON。
func TencentImageModeration(secretID, secretKey, region, bizType, imageURL, userID string) (string, error) {
	cli, err := ims.NewClient(common.NewCredential(secretID, secretKey), region, nil)
	if err != nil {
		return "", err
	}
	req := ims.NewImageModerationRequest()
	req.Type = strPtr("IMAGE")
	req.FileUrl = strPtr(imageURL)
	if bizType != "" {
		req.BizType = strPtr(bizType)
	}
	if userID != "" {
		req.DataId = strPtr("image:" + userID)
	}
	resp, err := cli.ImageModeration(req)
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(resp)
	return string(raw), nil
}

// ---------- 内部辅助 ----------

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func strPtr(s string) *string { return &s }

func base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// aliyunClient 懒构建阿里云 green 客户端（endpoint 对齐原仓库 green-cip.<region>；
// 连接/读超时防止审核 worker 被外呼卡死）。
func aliyunClient(akID, akSecret, region string) (*green.Client, error) {
	cfg := &openapiutil.Config{
		AccessKeyId:     strPtr(akID),
		AccessKeySecret: strPtr(akSecret),
		Endpoint:        strPtr("green-cip." + region + ".aliyuncs.com"),
		ConnectTimeout:  intPtr(5000),
		ReadTimeout:     intPtr(15000),
	}
	return green.NewClient(cfg)
}

func intPtr(v int) *int { return &v }

// 审核队列单测（不依赖 Redis / asynq server）。
//
// 覆盖：
//  1. 任务构造（type、payload 可解析）；
//  2. worker 注册后 mux 能分发两种任务类型；
//  3. handleAuditTask 的 msg_id 选取：优先 asynq TaskID，为空回退 recordID
//     —— 见下方 2026-11-19 事故注释；
//  4. 坏负载 / 未注入 ProcessRecord 的降级行为；
//  5. FromRedisClient 在 Redis 不可用时返回 nil（队列降级而非 panic）。
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hibiken/asynq"
)

// TestNewAuditTask 任务构造：type 与 payload。
//
// 防什么 bug：type 写错会让 worker 的 mux 匹配不到处理器（任务进死信），
// payload 字段名（record_id）与消费端结构体不一致会导致 recordID 恒为空。
func TestNewAuditTask(t *testing.T) {
	cases := []struct {
		name     string
		build    func(string) (*asynq.Task, error)
		wantType string
	}{
		{"文本审核任务", NewTextAuditTask, TypeTextAudit},
		{"图片审核任务", NewImageAuditTask, TypeImageAudit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			task, err := c.build("rec-1")
			if err != nil {
				t.Fatalf("构造任务失败: %v", err)
			}
			if task.Type() != c.wantType {
				t.Fatalf("type = %q, want %q", task.Type(), c.wantType)
			}
			var p auditPayload
			if err := json.Unmarshal(task.Payload(), &p); err != nil {
				t.Fatalf("payload 解析失败: %v", err)
			}
			if p.RecordID != "rec-1" {
				t.Fatalf("record_id = %q, want rec-1", p.RecordID)
			}
		})
	}
}

// TestRegisterAuditHandlers 两种任务类型都必须有处理器。
//
// 防什么 bug：只注册了 text-audit 处理器，图片任务被投递后一直
// 报 "handler not found" 而重试到死信，图片审核静默不执行。
func TestRegisterAuditHandlers(t *testing.T) {
	var called []string
	deps := AuditWorkerDeps{ProcessRecord: func(recordID, msgID string) error {
		called = append(called, recordID)
		return nil
	}}
	mux := asynq.NewServeMux()
	RegisterAuditHandlers(mux, deps)

	for _, tc := range []struct {
		name  string
		build func(string) (*asynq.Task, error)
	}{
		{"文本", NewTextAuditTask},
		{"图片", NewImageAuditTask},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called = nil
			task, err := tc.build("rec-" + tc.name)
			if err != nil {
				t.Fatalf("构造任务失败: %v", err)
			}
			if err := mux.ProcessTask(context.Background(), task); err != nil {
				t.Fatalf("处理任务失败: %v", err)
			}
			if len(called) != 1 || called[0] != "rec-"+tc.name {
				t.Fatalf("ProcessRecord 未被正确调用: %v", called)
			}
		})
	}
}

// TestHandleAuditTask_MsgIDFallback msg_id 选取规则。
//
// 2026-11-19 事故：msg_id 用 string 零值导致多条记录都为空串，
// 撞 queue_messages 的 uniqueIndex（SQLSTATE 23505），只有第一条审核记录能建，
// 后续审核全部静默失败。修复：取不到 asynq TaskID 时回退审核记录 ID。
//
// SDK 语义（asynq v0.26）：asynq.NewTask 造出的 task，其 ResultWriter 恒为 nil，
// 只有 server 传给 Handler.ProcessTask 的 task 才有有效 ResultWriter；
// 所以单测覆盖的正是「取不到 TaskID → 回退 recordID」这条最有价值的防线。
// asynq.TaskID option 只影响入队去重键，不影响这里取到的 msg id。
func TestHandleAuditTask_MsgIDFallback(t *testing.T) {
	cases := []struct {
		name     string
		taskID   string // asynq.TaskID option（入队去重键）
		recordID string
		wantMsg  string
	}{
		{"无 TaskID → 回退 recordID", "", "rec-1", "rec-1"},
		{"有 TaskID option 但未在 server 执行 → 仍回退 recordID", "task-1", "rec-2", "rec-2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			payload, _ := json.Marshal(auditPayload{RecordID: c.recordID})
			var opts []asynq.Option
			if c.taskID != "" {
				opts = append(opts, asynq.TaskID(c.taskID))
			}
			task := asynq.NewTask(TypeTextAudit, payload, opts...)

			var gotRecordID, gotMsgID string
			deps := AuditWorkerDeps{ProcessRecord: func(recordID, msgID string) error {
				gotRecordID, gotMsgID = recordID, msgID
				return nil
			}}
			if err := handleAuditTask(task, deps); err != nil {
				t.Fatalf("handleAuditTask 返回错误: %v", err)
			}
			if gotRecordID != c.recordID {
				t.Fatalf("recordID = %q, want %q", gotRecordID, c.recordID)
			}
			if gotMsgID != c.wantMsg {
				t.Fatalf("msgID = %q, want %q", gotMsgID, c.wantMsg)
			}
			if gotMsgID == "" {
				t.Fatal("msg_id 为空串会撞 uniqueIndex 23505（2026-11-19 事故），必须回退 recordID")
			}
		})
	}
}

// TestResultWriterNilSafe ResultWriter 为 nil 时不得 panic。
//
// 防什么 bug：旧实现直接 t.ResultWriter().TaskID()，而 SDK 明确
// 「NewTask 创建的 task ResultWriter 为 nil」，任何非 server 执行路径
// （直接 ProcessTask、本地回放、单测）都会空指针 panic，worker 直接崩。
func TestResultWriterNilSafe(t *testing.T) {
	payload, _ := json.Marshal(auditPayload{RecordID: "rec-nil"})
	task := asynq.NewTask(TypeTextAudit, payload)
	if task.ResultWriter() != nil {
		t.Skip("asynq 版本行为变化：ResultWriter 已非 nil，本用例前提不成立")
	}
	var got string
	deps := AuditWorkerDeps{ProcessRecord: func(recordID, msgID string) error {
		got = msgID
		return nil
	}}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ResultWriter 为 nil 时 panic 了：%v", r)
		}
	}()
	if err := handleAuditTask(task, deps); err != nil {
		t.Fatalf("应安全返回 nil，实际 %v", err)
	}
	if got != "rec-nil" {
		t.Fatalf("msgID = %q, want rec-nil", got)
	}
}

// TestHandleAuditTask_Degraded 坏负载与未注入依赖的降级行为。
//
// 防什么 bug：
//   - 坏负载继续重试 → 同一个坏任务把队列重试额度耗尽（应直接返回 error 进死信）；
//   - deps 未装配时 panic → worker 启动即崩。
func TestHandleAuditTask_Degraded(t *testing.T) {
	t.Run("坏负载返回错误", func(t *testing.T) {
		task := asynq.NewTask(TypeTextAudit, []byte("not-json"))
		deps := AuditWorkerDeps{ProcessRecord: func(recordID, msgID string) error {
			t.Fatal("坏负载不应调用 ProcessRecord")
			return nil
		}}
		if err := handleAuditTask(task, deps); err == nil {
			t.Fatal("坏负载应返回错误（走死信，不无限重试）")
		}
	})

	t.Run("未注入 ProcessRecord 时安全返回 nil", func(t *testing.T) {
		payload, _ := json.Marshal(auditPayload{RecordID: "rec-1"})
		task := asynq.NewTask(TypeTextAudit, payload)
		if err := handleAuditTask(task, AuditWorkerDeps{}); err != nil {
			t.Fatalf("未注入依赖时应安全返回 nil，实际 %v", err)
		}
	})

	t.Run("ProcessRecord 的错误原样上抛（触发 asynq 重试）", func(t *testing.T) {
		payload, _ := json.Marshal(auditPayload{RecordID: "rec-1"})
		task := asynq.NewTask(TypeTextAudit, payload)
		want := errors.New("provider timeout")
		deps := AuditWorkerDeps{ProcessRecord: func(recordID, msgID string) error { return want }}
		if err := handleAuditTask(task, deps); !errors.Is(err, want) {
			t.Fatalf("错误未透传: %v", err)
		}
	})
}

// TestFromRedisClient_Nil Redis 不可用时的队列降级。
//
// 防什么 bug：Redis 未配置时直接 new client 会拿到假地址，
// 入队全部失败；此处必须返回 nil，由上层跳过投递（审核记录仍落库留痕）。
func TestFromRedisClient_Nil(t *testing.T) {
	if c := FromRedisClient(nil); c != nil {
		t.Fatalf("rdb 为 nil 时应返回 nil 客户端，实际 %v", c)
	}
}

// Package model message 域实体：站内消息 / 私信会话 / 私信 / 已读回执 / 拉黑。
//
// 事实来源：tmp/picart/src/modules/message/entities/*.entity.ts（权威源码）。
// 表名与列名保持原 TypeORM 的 snake_case（message / message_read /
// private_conversation / private_message）；主键按本仓库统一约定改为 uuid 字符串
// （docs/04 §5.4），用户关联列因此为 string。
//
// 与原实体的两处有意差异：
//  1. 拉黑表。原仓库把 UserBlock 放在 user 模块
//     （src/modules/user/entities/user-block.entity.ts，表名 user_block）。
//     本仓库该实体已由 user 域（model/user.go 的 UserBlock）落地，消息域**直接复用**，
//     不再另建 message_block —— 同一语义只保留一个实体，避免 AutoMigrate 撞车。
//  2. JSON 列。原 json 列用自建 MessageJSON（实现 driver.Valuer / sql.Scanner），
//     因为 gorm.io/datatypes 不在 go.mod 且本域不得自行 go get。
package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// MessageJSON 通用 JSON 列载荷（对应原 TypeORM 的 type:"json" 列）。
//
// 存：MarshalJSON 后写 jsonb；取：Scan 时反序列化。nil 与空对象都安全。
type MessageJSON map[string]interface{}

// Value 实现 driver.Valuer：nil 写 NULL，否则写 jsonb 文本。
func (m MessageJSON) Value() (driver.Value, error) {
	if len(m) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// Scan 实现 sql.Scanner：兼容 NULL / []byte / string。
func (m *MessageJSON) Scan(src interface{}) error {
	if src == nil {
		*m = nil
		return nil
	}
	var raw []byte
	switch v := src.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return errors.New("message json: 不支持的扫描类型")
	}
	if len(raw) == 0 {
		*m = nil
		return nil
	}
	out := MessageJSON{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	*m = out
	return nil
}

// Message 站内消息（对齐 message.entity.ts）。
//
// is_broadcast=true 表示全员通知：此时 receiver_id 为空，每个用户的已读状态
// 落在 MessageRead（每人一行）；is_broadcast=false 表示点对点/部分发送，
// 已读状态直接落在本表 is_read。
type Message struct {
	BaseModel
	SenderID    string      `gorm:"type:char(36);index" json:"sender_id"`
	ReceiverID  *string     `gorm:"type:char(36);index" json:"receiver_id"`
	Content     string      `gorm:"type:text;not null" json:"content"`
	Type        string      `gorm:"type:varchar(20);not null;default:private;index" json:"type"` // private|system|notification
	IsRead      bool        `gorm:"not null;default:false" json:"is_read"`
	IsBroadcast bool        `gorm:"not null;default:false;index" json:"is_broadcast"`
	Title       *string     `gorm:"type:varchar(255)" json:"title"`
	Metadata    MessageJSON `gorm:"type:jsonb" json:"metadata"`
}

// TableName 表名对齐原 TypeORM @Entity("message")。
func (Message) TableName() string { return "message" }

// MessageRead 全员通知的逐用户已读回执（对齐 message-read.entity.ts）。
// (user_id, message_id) 唯一：重复标记已读按 ON CONFLICT DO NOTHING 处理
// （原源端 insert().orIgnore() 语义）。
type MessageRead struct {
	BaseModel
	UserID    string `gorm:"type:char(36);not null;uniqueIndex:uk_message_read_user_msg" json:"user_id"`
	MessageID string `gorm:"type:char(36);not null;uniqueIndex:uk_message_read_user_msg" json:"message_id"`
}

// TableName 表名对齐原 @Entity("message_read")。
func (MessageRead) TableName() string { return "message_read" }

// PrivateConversation 一对一会话（对齐 private-conversation.entity.ts）。
//
// user_one_id / user_two_id 按 uid 字典序（等价原源端的数值升序）存储，
// 保证同一对用户只有一行（原 findOrCreateConversation 的 sort 语义）。
type PrivateConversation struct {
	BaseModel
	UserOneID     string     `gorm:"type:char(36);not null;uniqueIndex:uk_conversation_pair" json:"user_one_id"`
	UserTwoID     string     `gorm:"type:char(36);not null;uniqueIndex:uk_conversation_pair" json:"user_two_id"`
	LastMessageID *string    `gorm:"type:char(36)" json:"last_message_id"`
	LastMessageAt *time.Time `gorm:"index" json:"last_message_at"`
}

// TableName 表名对齐原 @Entity("private_conversation")。
func (PrivateConversation) TableName() string { return "private_conversation" }

// PrivateMessage 私信（对齐 private-message.entity.ts）。
//
// 撤回想不改删除行，而是置 recalled_at / recalled_by_id / recall_reason 并清空
// content（payload 里打 recalled:true），与源端 recallMessage 行为一致
// ——前端靠 is_recalled 渲染「已撤回」占位。
type PrivateMessage struct {
	BaseModel
	ConversationID string      `gorm:"type:char(36);not null;index:idx_pm_conv_created,priority:1" json:"conversation_id"`
	SenderID       string      `gorm:"type:char(36);not null" json:"sender_id"`
	ReceiverID     string      `gorm:"type:char(36);not null;index:idx_pm_receiver_read,priority:1" json:"receiver_id"`
	MessageKind    string      `gorm:"type:varchar(20);not null;default:text" json:"message_kind"` // text|image|file|card
	Content        string      `gorm:"type:text" json:"content"`
	Payload        MessageJSON `gorm:"type:jsonb" json:"payload"`
	ReadAt         *time.Time  `gorm:"index:idx_pm_receiver_read,priority:2" json:"read_at"`
	RecalledAt     *time.Time  `json:"recalled_at"`
	RecalledByID   *string     `gorm:"type:char(36)" json:"recalled_by_id"`
	RecallReason   *string     `gorm:"type:varchar(255)" json:"recall_reason"`
}

// TableName 表名对齐原 @Entity("private_message")。
func (PrivateMessage) TableName() string { return "private_message" }

// 拉黑关系直接复用 user 域的 model.UserBlock（表 user_block，见 model/user.go），
// 消息域不重复定义实体。

// MessageModels 供 migrations 统一注册的 message 域实体清单。
//
// 用法（migrations/migrate.go）：
//
//	db.AutoMigrate(append([]interface{}{...}, model.MessageModels()...)...)
func MessageModels() []interface{} {
	return []interface{}{
		&Message{},
		&MessageRead{},
		&PrivateConversation{},
		&PrivateMessage{},
	}
}

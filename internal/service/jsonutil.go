// Package service 通用小工具（JSON 序列化等）。
package service

import "encoding/json"

// asynqJSON 任意值 → JSON 字节（asynq payload / jsonb 列共用）。
func asynqJSON(v interface{}) ([]byte, error) { return json.Marshal(v) }

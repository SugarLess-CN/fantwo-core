package repository

import (
	"errors"

	"gorm.io/gorm"
)

// IsMessageNotFound 判断消息相关查询是否为「记录不存在」。
// 由 message 域（仓储层）与 user 域共享，避免各自重复实现。
func IsMessageNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

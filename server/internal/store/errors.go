package store

import "errors"

var ErrNotFound = errors.New("not found")

// ErrConflict 唯一/引用冲突：agent 重名、删除仍被绑定引用的 agent 等。
var ErrConflict = errors.New("conflict")

// ErrInvalid 非法入参：任务同步 upsert 缺外部 ID 等。
var ErrInvalid = errors.New("invalid")

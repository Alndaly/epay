package gateway

import (
	"errors"
	"fmt"
)

// Error 是可以直接展示给商户或买家的业务错误（参数错误、签名错误等）。
// 其余错误（数据库、上游网络等）属于内部错误，对外只显示通用提示，详细信息写日志。
type Error struct {
	Msg string
}

func (e *Error) Error() string { return e.Msg }

func errorf(format string, args ...any) error {
	return &Error{Msg: fmt.Sprintf(format, args...)}
}

// PublicMessage 返回可对外展示的错误信息。
func PublicMessage(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Msg
	}
	return "系统繁忙，请稍后重试"
}

// IsPublic 判断是否为业务错误。
func IsPublic(err error) bool {
	var e *Error
	return errors.As(err, &e)
}

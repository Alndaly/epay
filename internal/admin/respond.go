package admin

import (
	"encoding/json"
	"errors"
	"net/http"

	"epay/internal/gateway"
	"epay/internal/httputil"
	"epay/internal/store"
)

// 统一响应格式：成功 {"data": ...}；失败 {"error": "原因"}，并使用相应的 HTTP 状态码。

func ok(w http.ResponseWriter, data any) {
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"data": data})
}

func fail(w http.ResponseWriter, status int, msg string) {
	httputil.WriteJSON(w, status, map[string]string{"error": msg})
}

// failErr 把错误映射为合适的状态码；内部错误只返回通用提示并记录日志。
func (a *Admin) failErr(w http.ResponseWriter, err error) {
	var bad *badRequest
	switch {
	case errors.As(err, &bad):
		fail(w, http.StatusBadRequest, bad.msg)
	case gateway.IsPublic(err):
		fail(w, http.StatusBadRequest, gateway.PublicMessage(err))
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "记录不存在")
	case errors.Is(err, store.ErrDuplicate):
		fail(w, http.StatusConflict, "记录已存在")
	default:
		a.log.Error("管理后台请求失败", "err", err)
		fail(w, http.StatusInternalServerError, "服务器内部错误")
	}
}

// badRequest 参数校验错误，原样返回给前端。
type badRequest struct{ msg string }

func (e *badRequest) Error() string { return e.msg }

func invalid(msg string) error { return &badRequest{msg: msg} }

// decode 解析 JSON 请求体（上限 1MB），失败时直接写出 400。
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "请求体格式错误")
		return false
	}
	return true
}

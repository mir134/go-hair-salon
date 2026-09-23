package service

import "net/http"

// 业务错误码（04-API.md:25-33）：40001 起为业务错误码；
// 4 位分段中的前三位与 HTTP 状态语义对齐（400xx=参数、401xx=认证、403xx=权限、
// 404xx=不存在、409xx=冲突、422xx=业务校验、500xx=服务器错误）。
const (
	CodeOK               = 0
	CodeInvalidParams    = 40000
	CodeCustomerNotFound = 40001
	CodeUnauthorized     = 40100
	CodeForbidden        = 40300
	CodeNotFound         = 40400
	CodeConflict         = 40900
	CodeValidationFailed = 42200
	CodeInternal         = 50000
)

// BizError 是可预期的业务错误：携带 HTTP 状态码、业务错误码与面向用户的提示。
//
// service 层返回 *BizError，controller 层据此渲染统一失败信封；
// 非 *BizError 的错误一律按 500 处理且不向客户端暴露内部信息（02-AGENTS.md:74）。
type BizError struct {
	Status  int
	Code    int
	Message string
}

// Error 实现 error 接口（message 面向用户，禁止携带内部细节）。
func (e *BizError) Error() string { return e.Message }

// NewBizError 构造业务错误。
func NewBizError(status, code int, message string) *BizError {
	return &BizError{Status: status, Code: code, Message: message}
}

// BadRequest 参数错误（400 / 40000）。
func BadRequest(message string) *BizError {
	return NewBizError(http.StatusBadRequest, CodeInvalidParams, message)
}

// Unauthorized 未登录或凭证无效（401 / 40100）。
func Unauthorized(message string) *BizError {
	return NewBizError(http.StatusUnauthorized, CodeUnauthorized, message)
}

// Forbidden 已登录但无权限（403 / 40300）。
func Forbidden(message string) *BizError {
	return NewBizError(http.StatusForbidden, CodeForbidden, message)
}

// NotFound 资源不存在（404 / 指定业务码）。
func NotFound(code int, message string) *BizError {
	return NewBizError(http.StatusNotFound, code, message)
}

// Conflict 业务冲突，如重复 request_id、重复结账（409 / 40900）。
func Conflict(message string) *BizError {
	return NewBizError(http.StatusConflict, CodeConflict, message)
}

// Validation 业务校验失败，如余额不足（422 / 42200）。
func Validation(message string) *BizError {
	return NewBizError(http.StatusUnprocessableEntity, CodeValidationFailed, message)
}

// Internal 服务器内部错误（500 / 50000），message 必须为用户友好文案。
func Internal(message string) *BizError {
	return NewBizError(http.StatusInternalServerError, CodeInternal, message)
}

// ErrCustomerNotFound 是“客户不存在”的标准业务错误（04-API.md:25-33 示例，40001）。
var ErrCustomerNotFound = NotFound(CodeCustomerNotFound, "客户不存在")

package controller

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/service"
)

// Response 是统一响应信封（04-API.md:13-33）：
//
//	成功 {"code":0,"message":"success","data":{...}}
//	失败 {"code":40001,"message":"客户不存在","data":null}
type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

// PageData 是分页 data 形态（04-API.md:35-44）。
type PageData struct {
	Items    any   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// Success 返回 200 成功信封。
func Success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Response{Code: service.CodeOK, Message: "success", Data: data})
}

// SuccessPage 把 service 分页结果映射为 DTO 列表并输出统一分页信封（04-API.md:35-44）。
//
// Items 保证序列化为数组（空结果为 []，不是 null）。
func SuccessPage[T, V any](c *gin.Context, result *service.PageResult[T], convert func(*T) V) {
	items := make([]V, 0, len(result.Items))
	for i := range result.Items {
		items = append(items, convert(&result.Items[i]))
	}
	Success(c, PageData{Items: items, Total: result.Total, Page: result.Page, PageSize: result.PageSize})
}

// Created 返回 201 成功信封（创建成功）。
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Response{Code: service.CodeOK, Message: "success", Data: data})
}

// Fail 渲染失败信封：
//   - *service.BizError 按自带 Status/Code/Message 返回；
//   - 其他错误一律 500 + 通用文案，内部错误只进服务端日志，不暴露给客户端
//     （02-AGENTS.md:74 禁止把内部 stack trace 返回前端）。
func Fail(c *gin.Context, err error) {
	var biz *service.BizError
	if errors.As(err, &biz) {
		c.JSON(biz.Status, Response{Code: biz.Code, Message: biz.Message, Data: nil})
		return
	}
	slog.Default().Error("未处理的服务端错误", "method", c.Request.Method, "path", c.Request.URL.Path, "err", err)
	c.JSON(http.StatusInternalServerError, Response{
		Code:    service.CodeInternal,
		Message: "服务器内部错误",
		Data:    nil,
	})
}

// FailWith 直接以指定 HTTP 状态与业务码返回失败信封（panic recovery 等无 error 对象的场景）。
func FailWith(c *gin.Context, status, code int, message string) {
	c.JSON(status, Response{Code: code, Message: message, Data: nil})
}

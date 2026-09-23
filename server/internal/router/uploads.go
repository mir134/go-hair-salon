package router

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/controller"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// uploadsRoutePath 是上传文件的静态托管路径（GET/HEAD 公开只读；写入见 router.New 的 POST /uploads）。
//
// 公开读取的原因：<img src> 无法携带 Authorization 头；头像本身非敏感数据。
// 写入口仍由 JWT + RBAC 保护。
const uploadsRoutePath = "/uploads/*filepath"

// newUploadsHandler 构造上传目录的只读静态处理器：
//   - 只放行 service 生成的存储名（16 位小写十六进制 + 规范扩展名），其余一律 404 信封——
//     目录请求、目录列表、.. / 反斜杠 / 绝对路径、编码穿越、可执行/脚本类型全部在此被拒
//     （防路径穿越，02-AGENTS.md:83）；
//   - 只读取 UPLOAD_DIR 下的普通文件，绝不暴露其他目录；
//   - 未命中返回 JSON 404 信封，不回退 SPA HTML（避免把 index.html 当作图片返回）。
func newUploadsHandler(uploadDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := strings.TrimPrefix(c.Param("filepath"), "/")
		if !service.IsStoredUploadName(name) {
			controller.FailWith(c, http.StatusNotFound, service.CodeNotFound, "资源不存在")
			return
		}
		full := filepath.Join(uploadDir, name)
		info, err := os.Stat(full)
		if err != nil || !info.Mode().IsRegular() {
			controller.FailWith(c, http.StatusNotFound, service.CodeNotFound, "资源不存在")
			return
		}
		// 图片按扩展名给出 Content-Type，并禁止浏览器按内容嗅探改写类型。
		c.Header("X-Content-Type-Options", "nosniff")
		c.File(full)
	}
}

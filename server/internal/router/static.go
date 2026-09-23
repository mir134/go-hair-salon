package router

import (
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/controller"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// 静态托管约定（01-PROJECT.md:47-59、08-DEPLOYMENT.md:9-21）：
//   - 静态根固定为 web/dist 子树，绝不把 config.yaml / data / logs 暴露给 HTTP；
//   - 非 /api 的未命中路由回退 web/dist/index.html，支撑前端 history 路由；
//   - /api 未命中路由必须返回统一 JSON 404 信封，不得回退 HTML（04-API.md:13-33）。
const (
	// apiPrefix 是业务 API 前缀；未命中的 /api 路由不进入 SPA 回退。
	apiPrefix = "/api"
	// distIndexName 是 SPA 入口文件名。
	distIndexName = "index.html"
)

// staticResult 是静态路径解析的三态结果。
type staticResult int

const (
	// staticMiss 表示未命中（文件不存在或不是普通文件），调用方回退 index.html。
	staticMiss staticResult = iota
	// staticHit 表示命中 dist 内的普通文件。
	staticHit
	// staticRejected 表示路径含穿越/注入意图（.. 段、反斜杠、NUL），直接拒绝。
	staticRejected
)

// spaHandler 是 NoRoute 兜底：/api 返回 JSON 404，其余请求优先静态文件，未命中回退 index.html。
type spaHandler struct {
	distDir   string // web/dist 绝对路径；空串表示未找到构建产物（仅 API 模式）
	indexPath string // distDir/index.html
}

// newSPAHandler 解析静态目录并返回 NoRoute 处理器。
func newSPAHandler(logger *slog.Logger) gin.HandlerFunc {
	distDir := resolveDistDir(logger)
	handler := spaHandler{distDir: distDir}
	if distDir != "" {
		handler.indexPath = filepath.Join(distDir, distIndexName)
	}
	return handler.handle
}

// handle 处理所有未命中路由。
func (h spaHandler) handle(c *gin.Context) {
	if isAPIPath(c.Request.URL.Path) {
		controller.FailWith(c, http.StatusNotFound, service.CodeNotFound, "接口不存在")
		return
	}
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		// 静态资源与 SPA 回退只服务 GET/HEAD，其余方法按未找到处理。
		controller.FailWith(c, http.StatusNotFound, service.CodeNotFound, "资源不存在")
		return
	}
	if h.distDir == "" {
		controller.FailWith(c, http.StatusNotFound, service.CodeNotFound, "页面不存在")
		return
	}
	switch file, result := h.resolveStatic(c.Request.URL.Path); result {
	case staticHit:
		c.File(file)
	case staticRejected:
		// 穿越/注入路径不进入回退，也不依赖 net/http 的兜底拦截：显式 404。
		controller.FailWith(c, http.StatusNotFound, service.CodeNotFound, "资源不存在")
	default:
		c.File(h.indexPath)
	}
}

// isAPIPath 判断是否业务 API 路径（/api 或 /api/...）。
func isAPIPath(urlPath string) bool {
	return urlPath == apiPrefix || strings.HasPrefix(urlPath, apiPrefix+"/")
}

// resolveStatic 把 URL 路径映射为 distDir 内的普通文件，返回三态：
//   - staticHit：命中的文件路径；
//   - staticMiss：未命中（不存在/目录），调用方回退 index.html；
//   - staticRejected：路径含 .. 段、反斜杠或 NUL（目录穿越 / Windows 分隔符注入）。
func (h spaHandler) resolveStatic(urlPath string) (string, staticResult) {
	rel := strings.TrimPrefix(urlPath, "/")
	if rel == "" {
		return h.indexPath, staticHit
	}
	if strings.ContainsAny(rel, "\\\x00") || path.IsAbs(rel) || hasDotDotSegment(rel) {
		return "", staticRejected
	}
	full := filepath.Join(h.distDir, filepath.FromSlash(path.Clean(rel)))
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() {
		return "", staticMiss
	}
	return full, staticHit
}

// hasDotDotSegment 判断路径是否含 ".." 段（不含 ".." 段时 path.Clean 不会逃出 dist）。
func hasDotDotSegment(rel string) bool {
	for _, segment := range strings.Split(rel, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

// resolveDistDir 按候选顺序返回第一个含 index.html 的 web/dist 绝对路径；均不存在时返回空串。
func resolveDistDir(logger *slog.Logger) string {
	for _, candidate := range distDirCandidates() {
		info, err := os.Stat(filepath.Join(candidate, distIndexName))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		abs, err := filepath.Abs(candidate)
		if err != nil {
			abs = candidate
		}
		logger.Info("静态资源托管已启用", "dir", abs)
		return abs
	}
	logger.Warn("未找到 web/dist，静态托管与 SPA 回退未启用（仅提供 API）")
	return ""
}

// distDirCandidates 返回 web/dist 候选路径（按优先级）：
//  1. 可执行文件同级目录（部署目录 hair-salon/ 内 exe + web/dist）；
//  2. 当前工作目录（以部署目录为工作目录启动）；
//  3. 工作目录的上级（源码树内 `go run ./cmd/server` 时 CWD=server/）。
func distDirCandidates() []string {
	candidates := make([]string, 0, 3)
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "web", "dist"))
	}
	candidates = append(candidates,
		filepath.Join("web", "dist"),
		filepath.Join("..", "web", "dist"),
	)
	return candidates
}

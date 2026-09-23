package controller_test

// TestUploads 是头像上传接口的验收测试（D8 决议：POST /uploads；02-AGENTS.md:82-83 类型/大小校验+防路径穿越；
// 08-DEPLOYMENT.md:74 uploads 纳入备份；03-DATABASE.md avatar 字段）：
//   - happy path：200 + 服务端哈希命名（sha256 前 16 位 + 规范扩展名）+ 文件真实落盘 + 返回路径经静态路由可取回；
//   - RBAC both：admin 与 staff 均可上传；未登录 401 且零写入；
//   - 大小：>2MB → 400（04-API.md:265-275 状态集合内；不引入文档外状态码）；
//   - 扩展名白名单 {jpg,jpeg,png,webp,gif}：exe/svg/html/无扩展名一律拒绝，jpeg → 规范为 jpg；
//   - MIME（magic bytes）嗅探：文本冒充 png、jpeg 冒充 png、png 冒充 jpg、空文件一律拒绝，绝不信任客户端声明；
//   - 客户端文件名净化：.. / 反斜杠 / 绝对路径 / 编码穿越均不得逃出 UPLOAD_DIR，也绝不覆盖目录外文件；
//   - 同名确定性：同一内容重复上传得到同一存储名（不产生第二个文件）；
//   - 静态托管：目录请求、不存在、非哈希名、穿越探针一律 404 信封且不泄漏目录外文件，也不回退 SPA HTML；
//   - 审计：每次成功上传写 operation_logs(action=upload)。

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// 以下字节均为真实文件头（服务端只嗅探签名，不解码像素）：
//
//	testPNG  最小 1x1 透明 PNG（67 字节，含 IHDR/IDAT/IEND）；
//	testJPEG 最小 JPEG（SOI + APP0/JFIF + EOI）；
//	testGIF  最小 GIF89a（含逻辑屏幕描述符与结束符）；
//	testWebP 最小 RIFF/WEBP（VP8 块头）。
var (
	testPNG = []byte{
		0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89,
		0x00, 0x00, 0x00, 0x0A, 'I', 'D', 'A', 'T',
		0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01,
		0x0D, 0x0A, 0x2D, 0xB4,
		0x00, 0x00, 0x00, 0x00, 'I', 'E', 'N', 'D', 0xAE, 0x42, 0x60, 0x82,
	}
	testJPEG = []byte{
		0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00,
		0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00,
		0xFF, 0xD9,
	}
	testGIF = []byte{
		'G', 'I', 'F', '8', '9', 'a',
		0x01, 0x00, 0x01, 0x00, 0x80, 0x00, 0x00,
		0x00, 0x00, 0x00, 0xFF, 0xFF, 0xFF,
		0x21, 0xF9, 0x04, 0x01, 0x00, 0x00, 0x00, 0x00,
		0x2C, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00,
		0x02, 0x02, 0x44, 0x01, 0x00, 0x3B,
	}
	testWebP = []byte{
		'R', 'I', 'F', 'F', 0x1A, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P',
		'V', 'P', '8', ' ', 0x0E, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
)

// storedUploadNamePattern 是服务端存储名的期望形态：16 位小写十六进制 + 规范扩展名。
var storedUploadNamePattern = regexp.MustCompile(`^[0-9a-f]{16}\.(jpg|png|webp|gif)$`)

// uploadData 是上传成功 data 的测试镜像（server/internal/controller/upload.go UploadView）。
type uploadData struct {
	URL  string `json:"url"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// pngVariant 在真实 PNG 尾部追加 1 字节标记：内容不同 → sha256 前缀不同（用于计数与确定性断言）。
func pngVariant(mark byte) []byte {
	return append(append([]byte{}, testPNG...), mark)
}

// multipartUpload 组装 multipart/form-data 请求体（字段名与客户端文件名可控）。
func multipartUpload(t *testing.T, field, filename string, content []byte) (string, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("CreateFormFile(%s): %v", field, err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("写入 multipart 内容: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("关闭 multipart writer: %v", err)
	}
	return writer.FormDataContentType(), &buf
}

// upload 发起 POST /api/v1/uploads（multipart，字段 file；token 为空表示匿名）。
func (e *customerEnv) upload(t *testing.T, token, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	contentType, body := multipartUpload(t, "file", filename, content)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/uploads", body)
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

// mustUpload 断言上传成功（200 + code=0）并返回 data。
func (e *customerEnv) mustUpload(t *testing.T, token, filename string, content []byte) uploadData {
	t.Helper()
	w := e.upload(t, token, filename, content)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /uploads(%s) status = %d, want 200 (body=%s)", filename, w.Code, w.Body.String())
	}
	var data uploadData
	decodeData(t, decodeEnvelope(t, w), &data)
	return data
}

// decodeUpload 解码上传成功 data。
func decodeUpload(t *testing.T, w *httptest.ResponseRecorder) uploadData {
	t.Helper()
	var data uploadData
	decodeData(t, decodeEnvelope(t, w), &data)
	return data
}

// regularFiles 返回 dir 下（递归）全部普通文件的路径；目录不存在时返回空。
func regularFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(p string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		t.Fatalf("遍历 %s 失败: %v", dir, err)
	}
	sort.Strings(files)
	return files
}

// assertStoredUpload 断言存储名对应的文件真实落盘且内容一致（misleading_success_output 探针：
// 不能只凭 200 判定成功）。
func assertStoredUpload(t *testing.T, dir, name string, want []byte) {
	t.Helper()
	full := filepath.Join(dir, name)
	got, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("上传文件未落盘 %s: %v", full, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("落盘内容与上传内容不一致：len(got)=%d len(want)=%d", len(got), len(want))
	}
}

// countUploadLogs 统计 action=upload 的审计日志条数。
func countUploadLogs(t *testing.T, env *customerEnv) int64 {
	t.Helper()
	var n int64
	if err := env.db.Model(&model.OperationLog{}).Where("action = ?", "upload").Count(&n).Error; err != nil {
		t.Fatalf("统计 upload 审计日志失败: %v", err)
	}
	return n
}

func TestUploads(t *testing.T) {
	env := newCustomerEnv(t)

	t.Run("happy_path_returns_hash_name_and_is_fetchable", func(t *testing.T) {
		// Given: 已登录 admin，上传目录初始为空。
		filesBefore := len(regularFiles(t, env.uploadDir))
		logsBefore := countUploadLogs(t, env)

		// When: POST /api/v1/uploads（真实 1x1 PNG）。
		w := env.upload(t, env.adminToken, "我的头像.png", testPNG)

		// Then: 200 + 统一信封 + 服务端哈希命名 + 相对 URL + 原始大小。
		if w.Code != http.StatusOK {
			t.Fatalf("POST /uploads status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		data := decodeUpload(t, w)
		if !storedUploadNamePattern.MatchString(data.Name) {
			t.Errorf("存储名 = %q, want 16 位十六进制 + 规范扩展名", data.Name)
		}
		if data.URL != service.UploadURLPrefix+data.Name {
			t.Errorf("url = %q, want %q", data.URL, service.UploadURLPrefix+data.Name)
		}
		if data.Size != int64(len(testPNG)) {
			t.Errorf("size = %d, want %d", data.Size, len(testPNG))
		}

		// Then: 文件真实落盘（内容一致），且只新增 1 个文件。
		assertStoredUpload(t, env.uploadDir, data.Name, testPNG)
		if got := len(regularFiles(t, env.uploadDir)); got != filesBefore+1 {
			t.Errorf("上传目录文件数 = %d, want %d", got, filesBefore+1)
		}

		// Then: 返回路径经 /uploads 静态路由可取回，内容一致且为图片类型。
		fetch := env.do(http.MethodGet, data.URL, "", nil)
		if fetch.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200 (body=%s)", data.URL, fetch.Code, fetch.Body.String())
		}
		if !bytes.Equal(fetch.Body.Bytes(), testPNG) {
			t.Errorf("GET %s 内容与上传内容不一致", data.URL)
		}
		if contentType := fetch.Header().Get("Content-Type"); contentType != "image/png" {
			t.Errorf("GET %s Content-Type = %q, want image/png", data.URL, contentType)
		}

		// Then: HEAD 同样可用（静态只读语义，仅响应头）。
		if head := env.do(http.MethodHead, data.URL, "", nil); head.Code != http.StatusOK {
			t.Errorf("HEAD %s status = %d, want 200", data.URL, head.Code)
		}

		// Then: 审计日志 action=upload（业务写操作之外独立写入）。
		if got := countUploadLogs(t, env); got != logsBefore+1 {
			t.Errorf("upload 审计日志 = %d 条, want %d", got, logsBefore+1)
		}
	})

	t.Run("staff_can_upload_both_role", func(t *testing.T) {
		// Given/When: staff 登录（路由策略 both：头像上传是资料编辑，非账务/管理操作）。
		w := env.upload(t, env.staffToken, "staff.png", pngVariant(1))

		// Then: 200 且落盘。
		if w.Code != http.StatusOK {
			t.Fatalf("staff POST /uploads status = %d, want 200 (body=%s)", w.Code, w.Body.String())
		}
		assertStoredUpload(t, env.uploadDir, decodeUpload(t, w).Name, pngVariant(1))
	})

	t.Run("all_allowed_image_types_are_accepted", func(t *testing.T) {
		// Given/When/Then: 白名单内扩展名均可上传，jpeg 规范化为 jpg（存储名扩展名）。
		cases := []struct {
			filename string
			content  []byte
			wantExt  string
		}{
			{"photo.jpg", testJPEG, ".jpg"},
			{"photo.jpeg", testJPEG, ".jpg"},
			{"photo.png", testPNG, ".png"},
			{"photo.webp", testWebP, ".webp"},
			{"photo.gif", testGIF, ".gif"},
		}
		for _, tc := range cases {
			data := env.mustUpload(t, env.adminToken, tc.filename, tc.content)
			if !strings.HasSuffix(data.Name, tc.wantExt) {
				t.Errorf("%s 存储名 = %q, want 后缀 %s", tc.filename, data.Name, tc.wantExt)
			}
			assertStoredUpload(t, env.uploadDir, data.Name, tc.content)
		}
	})

	t.Run("unauthenticated_returns_401_with_zero_writes", func(t *testing.T) {
		// Given: 匿名请求（无 token）。
		filesBefore := len(regularFiles(t, env.uploadDir))

		// When: POST /api/v1/uploads。
		w := env.upload(t, "", "anon.png", pngVariant(2))

		// Then: 401 + code=40100 + data=null，且零写入。
		requireErrorEnvelope(t, "anon POST /uploads", w, http.StatusUnauthorized, service.CodeUnauthorized)
		if got := len(regularFiles(t, env.uploadDir)); got != filesBefore {
			t.Errorf("未登录上传后文件数 = %d, want %d（零写入）", got, filesBefore)
		}
	})

	t.Run("oversize_is_rejected", func(t *testing.T) {
		// Given: 签名合法但超过 2MB 的 PNG，以及超过请求体上限的巨物。
		filesBefore := len(regularFiles(t, env.uploadDir))
		oversize := append(pngVariant(3), bytes.Repeat([]byte{0}, int(service.MaxUploadSizeBytes))...)
		huge := append(pngVariant(4), bytes.Repeat([]byte{0}, int(service.MaxUploadRequestBytes))...)

		// When/Then: 超过 2MB → 400（04-API.md 状态集合内）+ 明确文案，且零写入。
		w := env.upload(t, env.adminToken, "big.png", oversize)
		envl := requireErrorEnvelope(t, "oversize POST /uploads", w, http.StatusBadRequest, service.CodeInvalidParams)
		if !strings.Contains(envl.Message, "2MB") {
			t.Errorf("超限文案 = %q, want 含「2MB」", envl.Message)
		}

		// When/Then: 超过请求体上限（MaxBytesReader 拦截路径）同样 400，绝不 500。
		w2 := env.upload(t, env.adminToken, "huge.png", huge)
		requireErrorEnvelope(t, "huge POST /uploads", w2, http.StatusBadRequest, service.CodeInvalidParams)

		if got := len(regularFiles(t, env.uploadDir)); got != filesBefore {
			t.Errorf("超限上传后文件数 = %d, want %d（零写入）", got, filesBefore)
		}
	})

	t.Run("extension_whitelist_rejects_executable_and_markup", func(t *testing.T) {
		// Given: 内容为真 PNG，仅扩展名不在白名单。
		filesBefore := len(regularFiles(t, env.uploadDir))

		// When/Then: exe/svg/html/js/无扩展名 一律 400。
		for _, name := range []string{"evil.exe", "evil.svg", "evil.html", "evil.js", "evil.php", "noext"} {
			w := env.upload(t, env.adminToken, name, testPNG)
			requireErrorEnvelope(t, "扩展名 "+name, w, http.StatusBadRequest, service.CodeInvalidParams)
		}
		if got := len(regularFiles(t, env.uploadDir)); got != filesBefore {
			t.Errorf("非法扩展名上传后文件数 = %d, want %d（零写入）", got, filesBefore)
		}
	})

	t.Run("magic_bytes_mismatch_is_rejected", func(t *testing.T) {
		// Given: 客户端声明与真实内容不一致（绝不信任客户端 filename/Content-Type）。
		filesBefore := len(regularFiles(t, env.uploadDir))
		cases := []struct {
			label    string
			filename string
			content  []byte
		}{
			{"文本冒充 png", "fake.png", []byte("this is definitely not an image")},
			{"jpeg 冒充 png", "fake.png", testJPEG},
			{"png 冒充 jpg", "fake.jpg", testPNG},
			{"gif 冒充 png", "fake.png", testGIF},
			{"html 冒充 gif", "fake.gif", []byte("<html><body>boom</body></html>")},
			{"svg 冒充 png", "fake.png", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)},
			{"空文件", "empty.png", nil},
		}

		// When/Then: 全部 400，且零写入。
		for _, tc := range cases {
			w := env.upload(t, env.adminToken, tc.filename, tc.content)
			requireErrorEnvelope(t, tc.label, w, http.StatusBadRequest, service.CodeInvalidParams)
		}
		if got := len(regularFiles(t, env.uploadDir)); got != filesBefore {
			t.Errorf("MIME 不匹配上传后文件数 = %d, want %d（零写入）", got, filesBefore)
		}
	})

	t.Run("client_filename_path_traversal_never_escapes", func(t *testing.T) {
		// Given: uploadDir 之外存在一份“机密文件”，用于验证文件名穿越不会越界读写。
		parentDir := filepath.Dir(env.uploadDir)
		secretPath := filepath.Join(parentDir, "secret.png")
		secretContent := []byte("TOP-SECRET-MARKER")
		if err := os.WriteFile(secretPath, secretContent, 0o644); err != nil {
			t.Fatalf("写入机密探针文件: %v", err)
		}
		filesBefore := len(regularFiles(t, env.uploadDir))

		// When: 恶意客户端文件名（.. / 反斜杠 / 绝对路径 / 编码穿越）。
		names := []string{
			"../../secret.png",
			`..\..\secret.png`,
			`C:\Windows\evil.png`,
			"/etc/passwd.png",
			"..%2f..%2fsecret.png",
			"a/../../b.png",
		}
		for _, name := range names {
			w := env.upload(t, env.adminToken, name, pngVariant(5))
			switch w.Code {
			case http.StatusOK:
				// 已净化为服务端哈希名：绝不含客户端路径片段。
				data := decodeUpload(t, w)
				if !storedUploadNamePattern.MatchString(data.Name) {
					t.Errorf("文件名 %q: 存储名 = %q, want 纯哈希名", name, data.Name)
				}
				assertStoredUpload(t, env.uploadDir, data.Name, pngVariant(5))
			case http.StatusBadRequest:
				// 直接被拒也是安全结果。
			default:
				t.Errorf("文件名 %q: status = %d, want 200（已净化）或 400（拒绝）(body=%s)", name, w.Code, w.Body.String())
			}
		}

		// Then: 目录外机密文件未被覆盖/删除。
		if got, err := os.ReadFile(secretPath); err != nil || !bytes.Equal(got, secretContent) {
			t.Errorf("uploadDir 之外的 secret.png 被改写: err=%v content=%q", err, got)
		}
		// Then: uploadDir 内只允许哈希名，且同一内容重复上传只落 1 个文件。
		for _, f := range regularFiles(t, env.uploadDir) {
			if !storedUploadNamePattern.MatchString(filepath.Base(f)) {
				t.Errorf("上传目录出现非哈希名文件: %s", f)
			}
		}
		if got := len(regularFiles(t, env.uploadDir)); got != filesBefore+1 {
			t.Errorf("同一内容重复上传后文件数 = %d, want %d（确定性同名，不重复落盘）", got, filesBefore+1)
		}
		// Then: 目录外不得出现任何由穿越文件名产生的新文件。
		for _, escaped := range []string{"evil.png", "passwd.png", "b.png"} {
			if _, err := os.Stat(filepath.Join(parentDir, escaped)); err == nil {
				t.Errorf("文件名穿越在 uploadDir 之外写出了文件: %s", escaped)
			}
		}
	})

	t.Run("same_content_is_deterministic", func(t *testing.T) {
		// Given: 同一份内容（不同客户端文件名）。
		content := pngVariant(6)
		filesBefore := len(regularFiles(t, env.uploadDir))

		// When: 连续两次上传。
		first := env.mustUpload(t, env.adminToken, "one.png", content)
		second := env.mustUpload(t, env.adminToken, "two.png", content)

		// Then: 存储名相同（sha256 前 16 位确定性），文件数不增加。
		if first.Name != second.Name {
			t.Errorf("同一内容两次上传存储名 = %q / %q, want 同名", first.Name, second.Name)
		}
		if got := len(regularFiles(t, env.uploadDir)); got != filesBefore+1 {
			t.Errorf("重复上传后文件数 = %d, want %d", got, filesBefore+1)
		}
	})

	t.Run("missing_file_field_is_400", func(t *testing.T) {
		// Given/When: 字段名不是 file。
		contentType, body := multipartUpload(t, "avatar", "a.png", testPNG)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/uploads", body)
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Authorization", "Bearer "+env.adminToken)
		w := httptest.NewRecorder()
		env.engine.ServeHTTP(w, req)

		// Then: 400（参数错误），不 panic、不 500。
		requireErrorEnvelope(t, "字段名不是 file", w, http.StatusBadRequest, service.CodeInvalidParams)

		// When: 完全不是 multipart 的请求体。
		w2 := env.authed(http.MethodPost, "/api/v1/uploads", `{"file":"x"}`, env.adminToken)

		// Then: 400。
		requireErrorEnvelope(t, "非 multipart 请求体", w2, http.StatusBadRequest, service.CodeInvalidParams)
	})

	t.Run("static_route_rejects_missing_and_traversal", func(t *testing.T) {
		// Given: uploadDir 之外存在机密文件（复用上一个子测试写入的 secret.png）。
		secretPath := filepath.Join(filepath.Dir(env.uploadDir), "secret.png")
		if _, err := os.Stat(secretPath); err != nil {
			t.Fatalf("机密探针文件不存在: %v", err)
		}

		// When/Then: 目录请求 / 不存在 / 非哈希名 / 穿越探针一律 404 信封，不泄漏、不回退 SPA HTML。
		probes := []string{
			"/uploads/",
			"/uploads/nonexistent.png",
			"/uploads/0123456789abcdef.exe",
			"/uploads/0123456789ABCDEF.png",
			"/uploads/0123456789abcdef.png/../../secret.png",
			"/uploads/../secret.png",
			"/uploads/..%2fsecret.png",
			"/uploads/..%5csecret.png",
			"/uploads/%2e%2e%2fsecret.png",
		}
		for _, target := range probes {
			w := env.do(http.MethodGet, target, "", nil)
			if w.Code != http.StatusNotFound {
				t.Errorf("GET %s status = %d, want 404 (body=%s)", target, w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "TOP-SECRET-MARKER") {
				t.Errorf("GET %s 泄漏了 uploadDir 之外的文件内容", target)
			}
			if strings.Contains(w.Body.String(), `<div id="app">`) {
				t.Errorf("GET %s 回退了 SPA HTML（应 404 信封）", target)
			}
		}

		// When: 裸 /uploads（gin 尾斜杠重定向或 404 均可，但绝不能 200 列目录）。
		w := env.do(http.MethodGet, "/uploads", "", nil)

		// Then: 非 200，且无目录内容。
		if w.Code == http.StatusOK {
			t.Errorf("GET /uploads status = 200, want 非 200（禁止目录列表）(body=%s)", w.Body.String())
		}
	})
}

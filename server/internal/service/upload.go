package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// 上传约束（D8 决议；02-AGENTS.md:82-83 上传类型/大小校验 + 防路径穿越）：
//   - 单张图片 ≤ 2MB；
//   - 扩展名 ∈ {jpg,jpeg,png,webp,gif}，且必须与内容嗅探出的真实类型一致（不信任客户端声明）；
//   - 存储名 = 内容 sha256 前 16 位 + 规范扩展名，永不使用客户端文件名；
//   - 客户端路径信息（.. / 反斜杠 / 绝对路径）在解析扩展名时被丢弃，存储路径不可能逃出上传目录。
const (
	// MaxUploadSizeBytes 是单张图片允许的最大字节数（2MB，D8）。
	MaxUploadSizeBytes int64 = 2 << 20
	// MaxUploadRequestBytes 是 multipart 请求体上限：文件上限 + 1MB 表单/边界开销。
	// 超过该值由 controller 的 http.MaxBytesReader 在读取阶段拒绝。
	MaxUploadRequestBytes int64 = MaxUploadSizeBytes + 1<<20
	// UploadURLPrefix 是上传文件的对外 URL 前缀（静态托管在 /uploads/）。
	UploadURLPrefix = "/uploads/"
	// uploadHashPrefixLen 是存储名中内容哈希前缀的十六进制长度。
	uploadHashPrefixLen = 16
)

// UploadResult 是上传成功的结果（controller 据此渲染 DTO）。
type UploadResult struct {
	// Name 是服务端生成的存储文件名（<sha256[0:16]>.<ext>）。
	Name string
	// URL 是可直接用于 <img src> 的相对路径（/uploads/<name>）。
	URL string
	// Size 是文件字节数。
	Size int64
}

// UploadService 校验并保存上传图片。
//
// 不依赖数据库、不开启事务：上传只落盘一个内容寻址文件，审计日志由 controller 在
// 落盘成功后经 OperationLogService 独立写入（不在任何业务事务内）。
type UploadService struct {
	dir string
}

// NewUploadService 构造上传服务；dir 为 config.UploadDir。
func NewUploadService(dir string) *UploadService {
	return &UploadService{dir: dir}
}

// imageFormat 是嗅探出的图片格式（ext 为落盘使用的规范扩展名，jpeg → jpg）。
type imageFormat struct {
	ext string
}

// uploadExtFormats 是允许的客户端扩展名 → 规范扩展名的映射（jpeg 与 jpg 归一到 jpg）。
var uploadExtFormats = map[string]string{
	".jpg":  "jpg",
	".jpeg": "jpg",
	".png":  "png",
	".webp": "webp",
	".gif":  "gif",
}

// storedUploadExts 是静态托管放行的存储名扩展名（与 uploadExtFormats 的规范值一致）。
var storedUploadExts = map[string]struct{}{
	"jpg":  {},
	"png":  {},
	"webp": {},
	"gif":  {},
}

var (
	// jpegMagic / pngMagic 是文件签名前缀。
	jpegMagic = []byte{0xFF, 0xD8, 0xFF}
	pngMagic  = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
)

// Save 校验并保存一张图片：
//  1. 大小 ≤ 2MB（空文件拒绝）；
//  2. magic bytes 嗅探出 jpg/png/webp/gif 之一；
//  3. 客户端文件名扩展名在白名单内，且与真实类型一致；
//  4. 以内容哈希命名落盘（同内容重复上传 = 幂等覆盖同名文件）。
func (s *UploadService) Save(clientName string, r io.Reader) (*UploadResult, error) {
	if s.dir == "" {
		return nil, Internal("上传目录未配置")
	}
	if r == nil {
		return nil, BadRequest("缺少文件内容")
	}
	content, err := io.ReadAll(io.LimitReader(r, MaxUploadSizeBytes+1))
	if err != nil {
		return nil, BadRequest("读取上传文件失败")
	}
	if len(content) == 0 {
		return nil, BadRequest("文件内容为空")
	}
	if int64(len(content)) > MaxUploadSizeBytes {
		return nil, BadRequest("文件大小超过 2MB 限制")
	}
	format, ok := sniffImageFormat(content)
	if !ok {
		return nil, BadRequest("仅支持 jpg/jpeg/png/webp/gif 图片")
	}
	ext, ok := declaredUploadExt(clientName)
	if !ok {
		return nil, BadRequest("文件扩展名必须是 jpg/jpeg/png/webp/gif")
	}
	if ext != format.ext {
		return nil, BadRequest("文件扩展名与图片实际类型不一致")
	}
	name := uploadHashName(content, ext)
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, Internal("创建上传目录失败")
	}
	if err := os.WriteFile(filepath.Join(s.dir, name), content, 0o644); err != nil {
		return nil, Internal("保存上传文件失败")
	}
	return &UploadResult{Name: name, URL: UploadURLPrefix + name, Size: int64(len(content))}, nil
}

// sniffImageFormat 按 magic bytes 嗅探真实图片类型（空/非图片返回 false）。
func sniffImageFormat(content []byte) (imageFormat, bool) {
	switch {
	case bytes.HasPrefix(content, jpegMagic):
		return imageFormat{ext: "jpg"}, true
	case bytes.HasPrefix(content, pngMagic):
		return imageFormat{ext: "png"}, true
	case len(content) >= 6 && (string(content[:6]) == "GIF87a" || string(content[:6]) == "GIF89a"):
		return imageFormat{ext: "gif"}, true
	case isWebP(content):
		return imageFormat{ext: "webp"}, true
	default:
		return imageFormat{}, false
	}
}

// isWebP 判断 RIFF 容器且块类型为 WEBP（覆盖 VP8 / VP8L / VP8X）。
func isWebP(content []byte) bool {
	return len(content) >= 12 && string(content[:4]) == "RIFF" && string(content[8:12]) == "WEBP"
}

// declaredUploadExt 从客户端文件名提取规范扩展名：
// 先把 Windows 分隔符归一化为 /，再取 base —— 路径信息（.. / 反斜杠 / 绝对路径）在此被丢弃。
func declaredUploadExt(clientName string) (string, bool) {
	normalized := strings.ReplaceAll(clientName, `\`, "/")
	ext := strings.ToLower(path.Ext(path.Base(normalized)))
	canonical, ok := uploadExtFormats[ext]
	return canonical, ok
}

// uploadHashName 生成内容寻址存储名：<sha256[0:16]>.<ext>。
func uploadHashName(content []byte, ext string) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])[:uploadHashPrefixLen] + "." + ext
}

// IsStoredUploadName 判断文件名是否为本服务生成的存储名（16 位小写十六进制 + 规范扩展名）。
//
// 静态托管只放行这一形态：目录请求、目录列表、.. / 反斜杠 / 绝对路径、编码穿越、
// 可执行/脚本类型全部在此被拒（防路径穿越，02-AGENTS.md:83）。
func IsStoredUploadName(name string) bool {
	stem, ext, ok := strings.Cut(name, ".")
	if !ok || len(stem) != uploadHashPrefixLen {
		return false
	}
	if _, ok := storedUploadExts[ext]; !ok {
		return false
	}
	for i := 0; i < len(stem); i++ {
		c := stem[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

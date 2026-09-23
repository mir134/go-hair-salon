package model_test

// TestNoFloat64InServerCode 是 todo 59 的静态门（计划：金额禁用 float64；AGENTS.md 第 5 节、
// 01-PROJECT.md、05-TASKS.md:192）：
//
//   - 遍历 server 模块全部**非测试** .go 文件；
//   - 用 go/parser 解析 AST，命中 float64 / float32 标识符（类型声明、字段、参数、
//     返回值、类型转换）即失败；
//   - 注释与字符串中的说明文字不会误报——这正是本门用 AST 而非文本 grep 的原因；
//   - 扫描文件数为 0 时视为门失效并失败（防止路径漂移导致空跑）。
//
// 等价的人工命令（应与本测试同样为空输出）：
//
//	grep -rn "float64" server/ --include="*.go" | grep -v _test
//
// 本测试由 `go test ./...` 自动执行（无需额外脚本/CI 配置）。

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// forbiddenFloatIdents 是金额代码中禁止出现的浮点类型标识符。
var forbiddenFloatIdents = map[string]struct{}{
	"float64": {},
	"float32": {},
}

func TestNoFloat64InServerCode(t *testing.T) {
	root := serverModuleRoot(t)
	fset := token.NewFileSet()
	var hits []string
	scanned := 0

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "vendor", "testdata", "dist":
				return fs.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return fmt.Errorf("解析 %s: %w", path, parseErr)
		}
		scanned++
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			if _, forbidden := forbiddenFloatIdents[ident.Name]; forbidden {
				hits = append(hits, fmt.Sprintf("%s:%d %s",
					relativeTo(root, path), fset.Position(ident.Pos()).Line, ident.Name))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("遍历模块源码失败: %v", err)
	}
	if scanned == 0 {
		t.Fatalf("未扫描到任何非测试 .go 文件（静态门失效，root=%s）", root)
	}
	if len(hits) != 0 {
		t.Errorf("发现浮点类型使用 %d 处（金额必须 int64 整数分，禁止浮点数）:\n%s",
			len(hits), strings.Join(hits, "\n"))
		return
	}
	t.Logf("静态门通过：扫描 %d 个非测试 .go 文件，0 处 float64/float32", scanned)
}

// serverModuleRoot 从本测试源文件向上定位含 go.mod 的模块根目录。
func serverModuleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller 失败")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("未找到 go.mod（从 %s 向上）", filepath.Dir(file))
		}
		dir = parent
	}
}

// relativeTo 返回相对模块根的路径（错误信息更短、可复现）。
func relativeTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

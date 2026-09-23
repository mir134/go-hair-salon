package logging_test

// TestNewDailyFile 是 todo 5 的日志验收：
//   - slog 同时写控制台与 LOG_DIR 当日文件 server-YYYY-MM-DD.log；
//   - 日志内容含启动行等结构化字段。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/logging"
)

func TestNewDailyFile(t *testing.T) {
	// Given: 空的日志目录。
	dir := t.TempDir()

	// When: 创建 logger 并写一条启动日志，然后关闭。
	logger, closer, err := logging.New(dir)
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}
	logger.Info("服务启动", "port", 8080, "db_path", "data/test.db")
	if err := closer.Close(); err != nil {
		t.Fatalf("closer.Close: %v", err)
	}

	// Then: 当日文件存在且包含启动行与结构化字段。
	name := "server-" + time.Now().Format("2006-01-02") + ".log"
	content, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("读取当日日志 %s: %v", name, err)
	}
	text := string(content)
	for _, want := range []string{"服务启动", "port=8080", "db_path=data/test.db"} {
		if !strings.Contains(text, want) {
			t.Errorf("日志缺少 %q:\n%s", want, text)
		}
	}
}

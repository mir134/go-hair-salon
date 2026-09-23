// Package logging 提供进程级日志：同时输出到控制台与 LOG_DIR 下的按天文件
// （08-DEPLOYMENT.md:88-94）。
//
// 禁止记录：JWT 完整 token、密码、不必要的敏感客户信息。日志调用点不得把这些值
// 作为字段传入；SQL 日志通过 GormLogger 的 ParamsFilter 丢弃参数。
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// dateLayout 是日志文件按天切分的日期格式。
const dateLayout = "2006-01-02"

// New 创建同时写控制台与 LOG_DIR/server-YYYY-MM-DD.log 的 slog.Logger。
// 返回的 io.Closer 用于进程退出时关闭文件。
func New(logDir string) (*slog.Logger, io.Closer, error) {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("创建日志目录 %s 失败: %w", logDir, err)
	}
	writer, err := newDailyWriter(logDir)
	if err != nil {
		return nil, nil, err
	}
	handler := slog.NewTextHandler(io.MultiWriter(os.Stdout, writer), &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(handler), writer, nil
}

// FileName 返回某天的日志文件名。
func FileName(date string) string { return "server-" + date + ".log" }

// dailyWriter 是“每天一个文件”的 io.Writer：跨天写入时自动切换文件。
type dailyWriter struct {
	mu   sync.Mutex
	dir  string
	date string
	file *os.File
}

func newDailyWriter(dir string) (*dailyWriter, error) {
	w := &dailyWriter{dir: dir}
	if err := w.rotate(time.Now().Format(dateLayout)); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *dailyWriter) rotate(date string) error {
	if w.file != nil {
		_ = w.file.Close()
	}
	path := filepath.Join(w.dir, FileName(date))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("打开日志文件 %s 失败: %w", path, err)
	}
	w.file, w.date = file, date
	return nil
}

// Write 实现 io.Writer；跨天时先切换文件再写入。
func (w *dailyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if date := time.Now().Format(dateLayout); date != w.date {
		if err := w.rotate(date); err != nil {
			return 0, err
		}
	}
	return w.file.Write(p)
}

// Close 关闭当前日志文件（幂等）。
func (w *dailyWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

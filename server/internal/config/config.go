// Package config 加载并校验服务端配置。
//
// 配置来源优先级：同名环境变量 > config.yaml > 默认值。
// JWT_SECRET 等敏感配置禁止硬编码进源码（08-DEPLOYMENT.md:64-68）。
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultPath 是默认配置文件路径（相对于进程工作目录）。
const DefaultPath = "config.yaml"

// 可选配置项的默认值。
const (
	defaultServerHost           = "0.0.0.0"
	defaultServerPort           = 8080
	defaultInitialAdminPassword = "admin123"
	defaultBackupTime           = "23:00"
)

// requiredKey 描述一个必填配置键，用于启动失败时给出字段说明。
type requiredKey struct {
	key  string
	desc string
}

// requiredKeys 是启动时必须提供值的配置键（顺序即提示顺序）。
var requiredKeys = []requiredKey{
	{"DB_PATH", "SQLite 数据库文件路径（必须位于本机本地磁盘）"},
	{"UPLOAD_DIR", "上传文件存放目录"},
	{"BACKUP_DIR", "备份文件存放目录"},
	{"LOG_DIR", "日志文件存放目录"},
	{"JWT_SECRET", "JWT 签名密钥（禁止为空、禁止硬编码或提交到仓库）"},
}

// Config 是服务端运行所需的全部配置。
type Config struct {
	DBPath               string `yaml:"DB_PATH"`
	UploadDir            string `yaml:"UPLOAD_DIR"`
	BackupDir            string `yaml:"BACKUP_DIR"`
	LogDir               string `yaml:"LOG_DIR"`
	ServerHost           string `yaml:"SERVER_HOST"`
	ServerPort           int    `yaml:"SERVER_PORT"`
	JWTSecret            string `yaml:"JWT_SECRET"`
	InitialAdminPassword string `yaml:"INITIAL_ADMIN_PASSWORD"`
	BackupTime           string `yaml:"BACKUP_TIME"`
}

// Load 从 path 读取 YAML 配置，应用环境变量覆盖与默认值，并完成校验。
// 配置文件缺失、缺必填项或取值非法时返回错误，调用方应终止启动。
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("未找到配置文件 %s\n%s", path, requiredKeysHint())
		}
		return nil, fmt.Errorf("读取配置文件 %s 失败: %w", path, err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件 %s 失败: %w", path, err)
	}
	if err := cfg.applyEnvOverrides(); err != nil {
		return nil, err
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// applyEnvOverrides 用同名环境变量覆盖配置文件中的值（环境变量优先）。
func (c *Config) applyEnvOverrides() error {
	stringKeys := map[string]*string{
		"DB_PATH":                &c.DBPath,
		"UPLOAD_DIR":             &c.UploadDir,
		"BACKUP_DIR":             &c.BackupDir,
		"LOG_DIR":                &c.LogDir,
		"SERVER_HOST":            &c.ServerHost,
		"JWT_SECRET":             &c.JWTSecret,
		"INITIAL_ADMIN_PASSWORD": &c.InitialAdminPassword,
		"BACKUP_TIME":            &c.BackupTime,
	}
	for key, target := range stringKeys {
		if value := os.Getenv(key); value != "" {
			*target = value
		}
	}
	if value := os.Getenv("SERVER_PORT"); value != "" {
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("环境变量 SERVER_PORT 不是合法端口（1-65535）: %q", value)
		}
		c.ServerPort = port
	}
	return nil
}

// applyDefaults 为未配置的可选项填入默认值。
func (c *Config) applyDefaults() {
	if c.ServerHost == "" {
		c.ServerHost = defaultServerHost
	}
	if c.ServerPort == 0 {
		c.ServerPort = defaultServerPort
	}
	if c.InitialAdminPassword == "" {
		c.InitialAdminPassword = defaultInitialAdminPassword
	}
	if c.BackupTime == "" {
		c.BackupTime = defaultBackupTime
	}
}

// validate 校验必填项与取值范围。
func (c *Config) validate() error {
	values := map[string]string{
		"DB_PATH":    c.DBPath,
		"UPLOAD_DIR": c.UploadDir,
		"BACKUP_DIR": c.BackupDir,
		"LOG_DIR":    c.LogDir,
		"JWT_SECRET": c.JWTSecret,
	}
	var missing []string
	for _, rk := range requiredKeys {
		if values[rk.key] == "" {
			missing = append(missing, rk.key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("缺少必填配置项: %s\n%s", strings.Join(missing, ", "), requiredKeysHint())
	}

	if c.ServerPort < 1 || c.ServerPort > 65535 {
		return fmt.Errorf("SERVER_PORT 必须是 1-65535 之间的端口号，当前为 %d", c.ServerPort)
	}
	if _, err := time.Parse("15:04", c.BackupTime); err != nil {
		return fmt.Errorf("BACKUP_TIME 必须是 HH:MM（24 小时制）格式，当前为 %q", c.BackupTime)
	}
	// AGENTS.md 第 5 节：SQLite 数据库必须位于本机磁盘，禁止网络共享目录。
	if strings.HasPrefix(c.DBPath, `\\`) || strings.HasPrefix(c.DBPath, "//") {
		return fmt.Errorf("DB_PATH 不允许指向网络共享目录: %q", c.DBPath)
	}
	return nil
}

// requiredKeysHint 生成配置缺失时的字段说明（列出必填项与可选项默认值）。
func requiredKeysHint() string {
	var b strings.Builder
	b.WriteString("必填配置项（写入 config.yaml，或设置同名环境变量，环境变量优先）:")
	for _, rk := range requiredKeys {
		fmt.Fprintf(&b, "\n  - %-22s %s", rk.key, rk.desc)
	}
	b.WriteString("\n可选配置项（含默认值）:")
	fmt.Fprintf(&b, "\n  - %-22s %s（默认 %s）", "SERVER_HOST", "HTTP 监听地址", defaultServerHost)
	fmt.Fprintf(&b, "\n  - %-22s %s（默认 %d）", "SERVER_PORT", "HTTP 监听端口", defaultServerPort)
	fmt.Fprintf(&b, "\n  - %-22s %s（默认 %s）", "INITIAL_ADMIN_PASSWORD", "初始管理员密码", defaultInitialAdminPassword)
	fmt.Fprintf(&b, "\n  - %-22s %s（默认 %s）", "BACKUP_TIME", "每日自动备份时间 HH:MM", defaultBackupTime)
	return b.String()
}

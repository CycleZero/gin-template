package conf

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	klog "github.com/go-kratos/kratos/v3/log"
)

// TestMain 屏蔽 Kratos config 内部 watcher 的日志，保持测试输出干净。
func TestMain(m *testing.M) {
	klog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

// 注意：环境变量覆盖依赖 ${KEY:default} 占位符
// （env source 产出的是「去掉前缀后的名字」这一层级的 key，
// 只有占位符会去引用它），因此示例配置里需要被覆盖的字段都写成占位符。
const sampleConfig = `
data:
  db:
    host: "${DB_HOST:localhost}"
    port: "${DB_PORT:3306}"
    user: "${DB_USER:root}"
    password: "${DB_PASSWORD:secret}"
    db_name: "${DB_NAME:gin_template}"
  redis:
    host: "${REDIS_HOST:localhost}"
    port: "${REDIS_PORT:6379}"
    password: ""
server:
  http:
    host: "${HTTP_HOST:0.0.0.0}"
    port: "${HTTP_PORT:8000}"
    pprof:
      enable: true
      port: 6060
log:
  mode: "${LOG_MODE:dev}"
  level: debug
  dir: ""
app:
  dev_mode: "${DEV_MODE:true}"
  enable_db_debug: "${ENABLE_DB_DEBUG:false}"
`

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}
	return path
}

func TestLoadPlaceholderDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, sampleConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer func() { _ = Close() }()

	if got, want := cfg.GetData().GetDb().DSN(),
		"root:secret@tcp(localhost:3306)/gin_template?charset=utf8mb4&parseTime=True&loc=Local"; got != want {
		t.Errorf("DB.DSN() = %q, want %q", got, want)
	}
	if got, want := cfg.GetData().GetRedis().Addr(), "localhost:6379"; got != want {
		t.Errorf("Redis.Addr() = %q, want %q", got, want)
	}
	if got, want := cfg.GetServer().GetHttp().Addr(), "0.0.0.0:8000"; got != want {
		t.Errorf("HTTP.Addr() = %q, want %q", got, want)
	}
	if got := cfg.GetLog().GetMode(); got != "dev" {
		t.Errorf("Log.Mode = %q, want dev", got)
	}
	if cfg.GetApp().GetEnableDbDebug() {
		t.Error("App.EnableDbDebug = true, want false")
	}
}

func TestLoadEnvOverride(t *testing.T) {
	t.Setenv("APP_DB_HOST", "10.0.0.12")
	t.Setenv("APP_DB_PORT", "3400")
	t.Setenv("APP_DB_PASSWORD", "s3cr3t")
	t.Setenv("APP_HTTP_PORT", "8123")
	t.Setenv("APP_LOG_MODE", "prod")
	t.Setenv("APP_DEV_MODE", "false")

	cfg, err := Load(writeConfig(t, sampleConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer func() { _ = Close() }()

	if got, want := cfg.GetData().GetDb().DSN(),
		"root:s3cr3t@tcp(10.0.0.12:3400)/gin_template?charset=utf8mb4&parseTime=True&loc=Local"; got != want {
		t.Errorf("DB.DSN() = %q, want %q", got, want)
	}
	if got, want := cfg.GetServer().GetHttp().Addr(), "0.0.0.0:8123"; got != want {
		t.Errorf("HTTP.Addr() = %q, want %q", got, want)
	}
	if got := cfg.GetLog().GetMode(); got != "prod" {
		t.Errorf("Log.Mode = %q, want prod", got)
	}
	if cfg.GetApp().GetDevMode() {
		t.Error("App.DevMode = true, want false")
	}
}

func TestLoadRejectsInvalidMode(t *testing.T) {
	t.Setenv("APP_LOG_MODE", "staging")

	_, err := Load(writeConfig(t, sampleConfig))
	if err == nil {
		t.Fatal("Load 应当因 log.mode 非法而失败")
	}
	if !strings.Contains(err.Error(), "log.mode") {
		t.Errorf("错误信息应指明 log.mode，实际为: %v", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "not-exist.yaml")); err == nil {
		t.Fatal("配置文件不存在时应当返回错误")
	}
}

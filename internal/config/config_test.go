package config

import (
	"os"
	"path/filepath"
	"testing"
)

const testDefault = "http://default.test:8081"

// setup 把配置目录指向临时目录，并清空相关环境变量。
func setup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	for _, k := range []string{EnvURL, EnvUser, EnvPass, EnvContext} {
		t.Setenv(k, "")
	}
	return dir
}

func mustSave(t *testing.T, c *Config) {
	t.Helper()
	if err := Save(c); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func TestSaveLoadAndPermissions(t *testing.T) {
	setup(t)
	cfg := &Config{Contexts: map[string]*Context{}}
	cfg.Set("prod", &Context{URL: "http://prod:8081", User: "ci", Password: "s1"})
	cfg.Set("dev", &Context{URL: "http://dev:8081", User: "me", Password: "s2"})
	mustSave(t, cfg)

	st, err := os.Stat(Path())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Errorf("权限 = %o，期望 600", perm)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Contexts) != 2 {
		t.Fatalf("context 数 = %d，期望 2", len(got.Contexts))
	}
	if got.Current != "prod" {
		t.Errorf("current = %q，期望 prod（首次 Set 应自动设为 current）", got.Current)
	}
	if c := got.Contexts["dev"]; c == nil || c.URL != "http://dev:8081" {
		t.Errorf("dev context 读回不符: %+v", c)
	}
}

func TestDeleteCurrentReassigns(t *testing.T) {
	setup(t)
	cfg := &Config{Contexts: map[string]*Context{}}
	cfg.Set("a", &Context{URL: "http://a"})
	cfg.Set("b", &Context{URL: "http://b"})
	cfg.Current = "a"

	if !cfg.Delete("a") {
		t.Fatal("Delete 返回 false")
	}
	if cfg.Current != "b" {
		t.Errorf("删除 current 后 current = %q，期望 b", cfg.Current)
	}
	if cfg.Delete("nope") {
		t.Error("删除不存在的 context 应返回 false")
	}
}

func TestResolveUsesCurrentContext(t *testing.T) {
	setup(t)
	cfg := &Config{Contexts: map[string]*Context{}}
	cfg.Set("prod", &Context{URL: "http://prod:8081", User: "ci", Password: "s1"})
	mustSave(t, cfg)

	r, err := Resolve("", "", testDefault)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.URL != "http://prod:8081" || r.User != "ci" || r.Pass != "s1" {
		t.Errorf("解析结果不符: %+v", r)
	}
	if r.Context != "prod" || r.ContextFrom != FromCurrent {
		t.Errorf("context/来源不符: %q/%q", r.Context, r.ContextFrom)
	}
	if !r.Complete() {
		t.Error("Complete() 应为 true")
	}
}

func TestResolveContextFlagWins(t *testing.T) {
	setup(t)
	cfg := &Config{Contexts: map[string]*Context{}}
	cfg.Set("prod", &Context{URL: "http://prod:8081", User: "p", Password: "p"})
	cfg.Set("dev", &Context{URL: "http://dev:8081", User: "d", Password: "d"})
	mustSave(t, cfg)

	r, err := Resolve("dev", "", testDefault)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Context != "dev" || r.URL != "http://dev:8081" || r.User != "d" {
		t.Errorf("--context 未生效: %+v", r)
	}
}

func TestResolveUnknownContextErrors(t *testing.T) {
	setup(t)
	if _, err := Resolve("nope", "", testDefault); err == nil {
		t.Fatal("不存在的 context 应当报错")
	}
}

// 这是本次改动的核心：给定地址时自动匹配到对应 context，避免拿 A 的凭据打 B。
func TestResolveAutoMatchesByURL(t *testing.T) {
	setup(t)
	cfg := &Config{Contexts: map[string]*Context{}}
	cfg.Set("prod", &Context{URL: "http://prod:8081", User: "p", Password: "p"})
	cfg.Set("dev", &Context{URL: "http://dev:8081", User: "d", Password: "d"})
	mustSave(t, cfg)

	r, err := Resolve("", "http://dev:8081/", testDefault) // 末尾斜杠应被忽略
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Context != "dev" || r.ContextFrom != FromAutoURL {
		t.Errorf("未按地址匹配到 dev: context=%q from=%q", r.Context, r.ContextFrom)
	}
	if r.User != "d" || r.Pass != "d" {
		t.Errorf("未使用 dev 的凭据: %+v", r)
	}
	if r.URL != "http://dev:8081/" {
		t.Errorf("URL = %q", r.URL)
	}
}

func TestResolveEnvCredsOverride(t *testing.T) {
	setup(t)
	cfg := &Config{Contexts: map[string]*Context{}}
	cfg.Set("prod", &Context{URL: "http://prod:8081", User: "file-user", Password: "file-pass"})
	mustSave(t, cfg)

	t.Setenv(EnvUser, "env-user")
	t.Setenv(EnvPass, "env-pass")
	r, err := Resolve("", "", testDefault)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.User != "env-user" || r.Pass != "env-pass" {
		t.Errorf("环境变量应优先: %+v", r)
	}
	if r.CredFrom != FromEnv {
		t.Errorf("凭据来源 = %q，期望 %q", r.CredFrom, FromEnv)
	}
}

func TestResolveEnvURLOverridesContextURL(t *testing.T) {
	setup(t)
	cfg := &Config{Contexts: map[string]*Context{}}
	cfg.Set("prod", &Context{URL: "http://prod:8081", User: "u", Password: "p"})
	mustSave(t, cfg)

	r, err := Resolve("prod", "http://other:8081", testDefault)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.URL != "http://other:8081" || r.URLFrom != FromFlag {
		t.Errorf("URL 应取参数: %+v", r)
	}
	// 但凭据仍来自 prod —— 这正是需要提示不一致的场景
	if r.User != "u" {
		t.Errorf("凭据应来自 context: %+v", r)
	}
	if r.ContextURL != "http://prod:8081" {
		t.Errorf("ContextURL 应记录 context 的地址: %q", r.ContextURL)
	}
}

func TestResolveNoConfigFallsBackToDefault(t *testing.T) {
	setup(t)
	r, err := Resolve("", "", testDefault)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.URL != testDefault || r.URLFrom != FromDefault {
		t.Errorf("应回退到编译期默认值: %+v", r)
	}
	if r.Complete() {
		t.Error("没有凭据时 Complete() 应为 false")
	}
}

func TestLegacyMigration(t *testing.T) {
	dir := setup(t)
	legacy := filepath.Join(dir, "nexus-cli", "env")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	body := "# comment\nNEXUS_USER=old-user\nNEXUS_PASSWORD=old-pass\n"
	if err := os.WriteFile(legacy, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load() // 触发迁移
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ctx := cfg.Contexts[DefaultContext]
	if ctx == nil || ctx.User != "old-user" || ctx.Password != "old-pass" {
		t.Fatalf("迁移结果不符: %+v", ctx)
	}
	if cfg.Current != DefaultContext {
		t.Errorf("current = %q，期望 %q", cfg.Current, DefaultContext)
	}
	if _, err := os.Stat(Path()); err != nil {
		t.Errorf("config.json 未生成: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Error("旧文件应已重命名")
	}
	if _, err := os.Stat(legacy + ".migrated"); err != nil {
		t.Errorf("备份文件不存在: %v", err)
	}
}

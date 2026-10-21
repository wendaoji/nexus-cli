// Package config 管理 nexus-cli 的连接上下文（context）：目标地址 + 凭据。
//
// 存储：~/.config/nexus-cli/config.json（0600），形如
//
//	{"current":"prod","contexts":{"prod":{"url":"...","user":"...","password":"..."}}}
//
// 解析优先级（高 → 低）：
//
//	context 名：--context 参数 > NEXUS_CONTEXT > config.current
//	地址      ：--url 参数 > NEXUS_URL > 所选 context 的 url > 编译期默认值
//	凭据      ：NEXUS_USER / NEXUS_PASSWORD > 所选 context 的凭据
//
// 另有一条便利规则：若显式给了地址（参数或环境变量）且它与某个 context 的
// url 相同，则自动使用那个 context 的凭据 —— 这样 `NEXUS_URL=... nexus-cli ...`
// 不需要额外加 --context。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	EnvURL     = "NEXUS_URL"
	EnvUser    = "NEXUS_USER"
	EnvPass    = "NEXUS_PASSWORD"
	EnvContext = "NEXUS_CONTEXT"

	// DefaultContext 是首次登录时使用的 context 名。
	DefaultContext = "default"
)

// Context 是一个可命名的连接目标。
type Context struct {
	URL      string `json:"url"`
	User     string `json:"user"`
	Password string `json:"password"`
}

// Config 是磁盘上的配置。
type Config struct {
	Current  string              `json:"current,omitempty"`
	Contexts map[string]*Context `json:"contexts,omitempty"`
}

// 解析来源，用于 whoami 展示与排障。
const (
	FromFlag    = "命令行参数"
	FromEnv     = "环境变量"
	FromContext = "context"
	FromCurrent = "current"
	FromAutoURL = "按地址自动匹配"
	FromDefault = "编译期默认值"
)

// Resolved 是一次解析的最终结果。
type Resolved struct {
	Context     string // 生效的 context 名（可能为空）
	ContextFrom string // 名字是怎么定下来的
	URL         string
	URLFrom     string
	User        string
	Pass        string
	CredFrom    string // 凭据来自哪里
	ContextURL  string // 该 context 记录的地址（用于不一致提示）
	ConfigPath  string
}

// Complete 表示用户名与密码都齐全。
func (r *Resolved) Complete() bool { return r != nil && r.User != "" && r.Pass != "" }

// Dir 返回配置目录。
func Dir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return filepath.Join(os.TempDir(), "nexus-cli")
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "nexus-cli")
}

// Path 返回 config.json 的路径。
func Path() string { return filepath.Join(Dir(), "config.json") }

// LegacyPath 返回旧版单主机凭据文件的路径。
func LegacyPath() string { return filepath.Join(Dir(), "env") }

// Load 读取配置。config.json 不存在时会尝试从旧版 env 文件迁移。
func Load() (*Config, error) {
	b, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return migrateLegacy()
		}
		return nil, fmt.Errorf("读取 %s: %w", Path(), err)
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", Path(), err)
	}
	if c.Contexts == nil {
		c.Contexts = map[string]*Context{}
	}
	return &c, nil
}

// Save 以 0600 原子写入配置。
func Save(c *Config) error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return fmt.Errorf("创建目录 %s: %w", Dir(), err)
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	tmp, err := os.CreateTemp(Dir(), ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("写入 %s: %w", Path(), err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), Path())
}

// Set 新增或更新一个 context（空名字视为 DefaultContext）。
func (c *Config) Set(name string, ctx *Context) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = DefaultContext
	}
	if c.Contexts == nil {
		c.Contexts = map[string]*Context{}
	}
	c.Contexts[name] = ctx
	if c.Current == "" {
		c.Current = name
	}
}

// Delete 删除一个 context；若删的是 current 则把 current 指向剩余的第一个。
func (c *Config) Delete(name string) bool {
	if _, ok := c.Contexts[name]; !ok {
		return false
	}
	delete(c.Contexts, name)
	if c.Current == name {
		names := c.Names()
		if len(names) > 0 {
			c.Current = names[0]
		} else {
			c.Current = ""
		}
	}
	return true
}

// Names 返回排序后的 context 名。
func (c *Config) Names() []string {
	out := make([]string, 0, len(c.Contexts))
	for n := range c.Contexts {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Resolve 按优先级算出本次调用要用的地址与凭据。
// 显式指定的 context 不存在时报错 —— 避免 `--context` 拼错后静默用错目标。
func Resolve(contextFlag, urlFlag, defaultOrigin string) (*Resolved, error) {
	return resolve(contextFlag, urlFlag, defaultOrigin, false)
}

// ResolveForLogin 与 Resolve 相同，但允许 context 尚不存在（login 会创建它）。
func ResolveForLogin(contextFlag, urlFlag, defaultOrigin string) (*Resolved, error) {
	return resolve(contextFlag, urlFlag, defaultOrigin, true)
}

func resolve(contextFlag, urlFlag, defaultOrigin string, allowMissingContext bool) (*Resolved, error) {
	cfg, err := Load()
	if err != nil {
		return nil, err
	}
	r := &Resolved{ConfigPath: Path()}

	explicitCtx := strings.TrimSpace(contextFlag)
	ctxFrom := FromFlag
	if explicitCtx == "" {
		explicitCtx = strings.TrimSpace(os.Getenv(EnvContext))
		ctxFrom = FromEnv
	}
	explicitURL := strings.TrimSpace(urlFlag)
	urlFromFlag := explicitURL != ""
	if !urlFromFlag {
		explicitURL = strings.TrimSpace(os.Getenv(EnvURL))
	}

	// 1. 选 context
	switch {
	case explicitCtx != "":
		r.Context = explicitCtx
		r.ContextFrom = ctxFrom
		if _, ok := cfg.Contexts[r.Context]; !ok && !allowMissingContext {
			return nil, fmt.Errorf("context %q 不存在（先用 `nexus-cli login --context %s` 创建）", r.Context, r.Context)
		}
	case explicitURL != "":
		// 按地址自动匹配已有 context
		if name := cfg.MatchURL(explicitURL); name != "" {
			r.Context, r.ContextFrom = name, FromAutoURL
		}
	}
	if r.Context == "" && cfg.Current != "" {
		if _, ok := cfg.Contexts[cfg.Current]; ok {
			r.Context, r.ContextFrom = cfg.Current, FromCurrent
		}
	}

	var ctx *Context
	if r.Context != "" {
		ctx = cfg.Contexts[r.Context]
		if ctx != nil {
			r.ContextURL = ctx.URL
		}
	}

	// 2. 定地址
	switch {
	case explicitURL != "":
		r.URL = explicitURL
		if urlFromFlag {
			r.URLFrom = FromFlag
		} else {
			r.URLFrom = FromEnv
		}
	case ctx != nil && ctx.URL != "":
		r.URL, r.URLFrom = ctx.URL, FromContext
	default:
		r.URL, r.URLFrom = defaultOrigin, FromDefault
	}

	// 3. 定凭据
	r.User = strings.TrimSpace(os.Getenv(EnvUser))
	r.Pass = os.Getenv(EnvPass)
	switch {
	case r.User != "" || r.Pass != "":
		r.CredFrom = FromEnv
		if ctx != nil {
			if r.User == "" {
				r.User = ctx.User
			}
			if r.Pass == "" {
				r.Pass = ctx.Password
			}
		}
	case ctx != nil:
		r.User, r.Pass = ctx.User, ctx.Password
		if ctx.User != "" || ctx.Password != "" {
			r.CredFrom = FromContext
		}
	}
	return r, nil
}

// MatchURL 返回 url 完全相同的 context 名（忽略末尾斜杠与大小写）。
func (c *Config) MatchURL(u string) string {
	target := normalizeURL(u)
	for _, n := range c.Names() {
		if ctx := c.Contexts[n]; ctx != nil && normalizeURL(ctx.URL) == target {
			return n
		}
	}
	return ""
}

func normalizeURL(u string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(u), "/"))
}

// ── 旧版迁移 ────────────────────────────────────────────────────────────────

// migrateLegacy 把旧版单主机 env 文件转成一个 default context，并保留备份。
// 旧文件没有主机信息，只能假设它对应编译期默认地址。
func migrateLegacy() (*Config, error) {
	cfg := &Config{Contexts: map[string]*Context{}}

	user, pass, ok, err := readLegacy(LegacyPath())
	if err != nil {
		return nil, err
	}
	if !ok {
		return cfg, nil
	}

	cfg.Set(DefaultContext, &Context{User: user, Password: pass})
	if err := Save(cfg); err != nil {
		return nil, err
	}
	// 保留备份，不删除用户的凭据
	backup := LegacyPath() + ".migrated"
	if err := os.Rename(LegacyPath(), backup); err != nil {
		// 迁移本身已成功，备份失败只提示，不算错误
		fmt.Fprintf(os.Stderr, "nexus-cli: 旧凭据文件已迁移，但备份失败（%v）；请手动处理 %s\n", err, LegacyPath())
		return cfg, nil
	}
	fmt.Fprintf(os.Stderr, "nexus-cli: 已把 %s 迁移为 context %q；旧文件备份在 %s\n",
		LegacyPath(), DefaultContext, backup)
	fmt.Fprintln(os.Stderr, "          如需指定该 context 的目标地址，重跑 `nexus-cli login --context default --url <地址>`")
	return cfg, nil
}

func readLegacy(path string) (user, pass string, ok bool, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", false, nil
		}
		return "", "", false, fmt.Errorf("读取 %s: %w", path, err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		switch strings.TrimSpace(k) {
		case EnvUser:
			user = strings.TrimSpace(v)
		case EnvPass:
			pass = strings.TrimSpace(v)
		}
	}
	if user == "" && pass == "" {
		return "", "", false, nil
	}
	return user, pass, true, nil
}

// ErrNoCredentials 是「没配置任何凭据」时的统一错误。
var ErrNoCredentials = errors.New(
	"缺少凭据：运行 `nexus-cli login --user <账号>`，或设置 NEXUS_USER / NEXUS_PASSWORD")

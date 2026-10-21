package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/wendaoji/nexus-cli/internal/config"
	"github.com/wendaoji/nexus-cli/internal/nexus"
)

const loginUsage = `用法: nexus-cli login --user <账号> [--password <密码> | --password-stdin]
                       [--url <地址>] [--context <名字>]

把目标地址与凭据写入 context（配置文件 ~/.config/nexus-cli/config.json，0600）。
写入前会先校验一次。

  --user <账号>          要保存的账号（建议专用只读账号）
  --password <密码>      直接给密码；会出现在 ps / shell history 里
  --password-stdin       从 stdin 读密码（适合脚本/CI）
  --url <地址>           目标地址；默认取 NEXUS_URL / 当前 context / 编译期默认值
  --context <名字>       context 名；默认取「按地址匹配到的」或当前 context 或 default
  -o json, --json        JSON 输出

示例:
  nexus-cli login --user ci-readonly                          # 登录默认目标
  nexus-cli login --url http://dev:8081 --user ci --context dev
  nexus-cli context use dev

关于 token：
  Nexus 的 User Token 是 Pro 专属功能（Community 版没有该接口，调用会 404）。
  若你在 Pro 版或 Web UI 里拿到了 user token，直接把它当 --password 传入即可。
`

// Login 保存并校验一个 context 的地址与凭据。
func Login(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		user      string
		pass      string
		passStdin bool
		out       outputFlags
	)
	fs.StringVar(&user, "user", "", "账号")
	fs.StringVar(&pass, "password", "", "密码或 user token passcode")
	fs.BoolVar(&passStdin, "password-stdin", false, "从 stdin 读取密码")
	out.addTo(fs)
	fs.Usage = func() { fmt.Fprint(os.Stderr, loginUsage) }

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(os.Stdout, loginUsage)
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("login: 多余的参数 %q（--help 看用法）", fs.Arg(0))
	}
	if strings.TrimSpace(user) == "" {
		return errors.New("login: 需要 --user")
	}

	if passStdin {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("读取 stdin: %w", err)
		}
		pass = strings.TrimRight(string(b), "\r\n")
	} else if pass == "" {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("login: 非交互环境请用 --password-stdin（或 --password）")
		}
		fmt.Fprintf(os.Stderr, "%s 的密码（或 user token passcode）: ", user)
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return fmt.Errorf("读取密码: %w", err)
		}
		pass = string(b)
	}
	if pass == "" {
		return errors.New("login: 密码为空，未写入")
	}

	r, err := resolveTarget()
	if err != nil {
		return err
	}

	// 写入前先校验，避免把错凭据存下来后到处 401（还会触发 Nexus 限流）。
	c, err := nexus.New(r.URL, user, pass)
	if err != nil {
		return err
	}
	res, err := c.Check(context.Background())
	if err != nil {
		return err
	}
	if !res.Valid() {
		return errors.New(nexus.ExplainCheck(res))
	}

	name := strings.TrimSpace(GlobalContext)
	if name == "" {
		name = r.Context
	}
	if name == "" {
		name = config.DefaultContext
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if prev := cfg.Contexts[name]; prev != nil && prev.URL != "" && normalize(prev.URL) != normalize(r.URL) {
		fmt.Fprintf(os.Stderr, "nexus-cli: context %q 的地址由 %s 更新为 %s\n", name, prev.URL, r.URL)
	}
	cfg.Set(name, &config.Context{URL: r.URL, User: user, Password: pass})
	if cfg.Current == "" {
		cfg.Current = name
	}
	if err := config.Save(cfg); err != nil {
		return err
	}

	type result struct {
		Context    string `json:"context"`
		URL        string `json:"url"`
		User       string `json:"user"`
		ConfigPath string `json:"config_path"`
		Checked    string `json:"checked"`
		IsCurrent  bool   `json:"is_current"`
	}
	res2 := result{
		Context: name, URL: r.URL, User: user, ConfigPath: config.Path(),
		Checked: nexus.ExplainCheck(res), IsCurrent: cfg.Current == name,
	}
	return emit(out.JSON, res2, func(w io.Writer) {
		fmt.Fprintln(w, res2.Checked)
		fmt.Fprintf(w, "context: %s%s\n", name, currentMark(res2.IsCurrent))
		fmt.Fprintf(w, "已写入 %s（chmod 600）\n目标: %s\n", res2.ConfigPath, res2.URL)
		if !res2.IsCurrent {
			fmt.Fprintf(w, "切换: nexus-cli context use %s\n", name)
		}
	})
}

const whoamiUsage = `用法: nexus-cli whoami [--context <名字>] [--url <地址>] [--check] [-o json]

显示当前生效的 context、目标地址与凭据来源。
  --context <名字>   查看指定 context
  --url <地址>       按指定地址解析
  --check            再请求一次接口确认凭据可用
`

// Whoami 展示生效的上下文。
func Whoami(args []string) error {
	fs := flag.NewFlagSet("whoami", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		check bool
		out   outputFlags
	)
	fs.BoolVar(&check, "check", false, "验证连通性")
	out.addTo(fs)
	fs.Usage = func() { fmt.Fprint(os.Stderr, whoamiUsage) }

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(os.Stdout, whoamiUsage)
			return nil
		}
		return err
	}

	r, err := resolveTarget()
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	info := struct {
		Context     string   `json:"context"`
		ContextFrom string   `json:"context_from,omitempty"`
		URL         string   `json:"url"`
		URLFrom     string   `json:"url_from"`
		User        string   `json:"user"`
		CredFrom    string   `json:"credential_from,omitempty"`
		PasswordSet bool     `json:"password_set"`
		ConfigPath  string   `json:"config_path"`
		Contexts    []string `json:"contexts"`
		Mismatch    string   `json:"mismatch,omitempty"`
		Check       string   `json:"check,omitempty"`
	}{
		Context:     r.Context,
		ContextFrom: r.ContextFrom,
		URL:         r.URL,
		URLFrom:     r.URLFrom,
		User:        redact(r.User),
		CredFrom:    r.CredFrom,
		PasswordSet: r.Pass != "",
		ConfigPath:  r.ConfigPath,
		Contexts:    cfg.Names(),
	}
	// 地址与 context 记录不一致时明确提示，避免拿 A 的凭据打 B
	if r.ContextURL != "" && normalize(r.ContextURL) != normalize(r.URL) {
		info.Mismatch = fmt.Sprintf("context %q 记录的是 %s，本次目标是 %s", r.Context, r.ContextURL, r.URL)
	}

	if check {
		c, err := nexus.New(r.URL, r.User, r.Pass)
		if err != nil {
			return err
		}
		res, err := c.Check(context.Background())
		if err != nil {
			info.Check = "失败: " + err.Error()
		} else {
			info.Check = nexus.ExplainCheck(res)
		}
	}

	err = emit(out.JSON, info, func(w io.Writer) {
		ctx := r.Context
		if ctx == "" {
			ctx = "<无>"
		}
		fmt.Fprintf(w, "context: %s%s\n", ctx, from(r.ContextFrom))
		fmt.Fprintf(w, "地址: %s%s\n", r.URL, from(r.URLFrom))
		fmt.Fprintf(w, "账号: %s%s\n", orUnset(info.User), from(r.CredFrom))
		if info.PasswordSet {
			fmt.Fprintln(w, "密码: 已设置")
		} else {
			fmt.Fprintln(w, "密码: <未设置>")
		}
		fmt.Fprintf(w, "配置文件: %s\n", r.ConfigPath)
		if len(info.Contexts) > 0 {
			fmt.Fprintf(w, "所有 context: %s（current=%s）\n", strings.Join(info.Contexts, ", "), orUnset(cfg.Current))
		}
		if info.Mismatch != "" {
			fmt.Fprintf(w, "⚠ %s\n", info.Mismatch)
		}
		if info.Check != "" {
			fmt.Fprintf(w, "连通性: %s\n", info.Check)
		}
	})
	if err != nil {
		return err
	}
	if info.Mismatch != "" {
		return fmt.Errorf("目标地址与 context 记录不一致（见上）")
	}
	if check && !strings.Contains(info.Check, "有效") {
		return errors.New("连通性检查未通过")
	}
	return nil
}

const contextUsage = `用法: nexus-cli context <子命令>

  context list              列出所有 context（* 为当前）
  context use <名字>        切换当前 context
  context show [名字]       查看某个 context（不填则当前）
  context remove <名字>     删除 context
  -o json, --json           JSON 输出
`

// ContextCmd 实现 context 子命令。
func ContextCmd(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, contextUsage)
		return errors.New("context: 需要子命令")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "-h", "--help", "help":
		fmt.Fprint(os.Stdout, contextUsage)
		return nil
	case "list", "ls":
		return contextList(rest)
	case "use":
		return contextUse(rest)
	case "show":
		return contextShow(rest)
	case "remove", "rm":
		return contextRemove(rest)
	default:
		return fmt.Errorf("context: 未知子命令 %q（--help 看用法）", sub)
	}
}

func contextList(args []string) error {
	fs := flag.NewFlagSet("context list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var out outputFlags
	out.addTo(fs)
	if err := fs.Parse(args); err != nil && !errors.Is(err, flag.ErrHelp) {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	type row struct {
		Name    string `json:"name"`
		URL     string `json:"url"`
		User    string `json:"user"`
		Current bool   `json:"current"`
	}
	rows := make([]row, 0, len(cfg.Contexts))
	for _, n := range cfg.Names() {
		c := cfg.Contexts[n]
		rows = append(rows, row{Name: n, URL: c.URL, User: c.User, Current: n == cfg.Current})
	}
	return emit(out.JSON, map[string]any{"current": cfg.Current, "contexts": rows}, func(w io.Writer) {
		if len(rows) == 0 {
			fmt.Fprintln(w, "还没有任何 context。运行 `nexus-cli login --user <账号>` 创建。")
			return
		}
		for _, r := range rows {
			mark := "  "
			if r.Current {
				mark = "* "
			}
			fmt.Fprintf(w, "%s%-12s %-32s %s\n", mark, r.Name, orUnset(r.URL), orUnset(r.User))
		}
	})
}

func contextUse(args []string) error {
	fs := flag.NewFlagSet("context use", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var out outputFlags
	out.addTo(fs)
	if err := fs.Parse(args); err != nil && !errors.Is(err, flag.ErrHelp) {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("用法: nexus-cli context use <名字>")
	}
	name := fs.Arg(0)
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, ok := cfg.Contexts[name]
	if !ok {
		return fmt.Errorf("context %q 不存在（现有: %s）", name, strings.Join(cfg.Names(), ", "))
	}
	cfg.Current = name
	if err := config.Save(cfg); err != nil {
		return err
	}
	return emit(out.JSON, map[string]string{"current": name, "url": ctx.URL}, func(w io.Writer) {
		fmt.Fprintf(w, "已切换到 context %q（%s）\n", name, orUnset(ctx.URL))
	})
}

func contextShow(args []string) error {
	fs := flag.NewFlagSet("context show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var out outputFlags
	out.addTo(fs)
	if err := fs.Parse(args); err != nil && !errors.Is(err, flag.ErrHelp) {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	name := cfg.Current
	if fs.NArg() > 0 {
		name = fs.Arg(0)
	}
	if name == "" {
		return errors.New("没有当前 context；先运行 `nexus-cli login`")
	}
	c, ok := cfg.Contexts[name]
	if !ok {
		return fmt.Errorf("context %q 不存在", name)
	}
	info := map[string]any{
		"name": name, "url": c.URL, "user": c.User,
		"password_set": c.Password != "", "is_current": cfg.Current == name,
	}
	return emit(out.JSON, info, func(w io.Writer) {
		mark := ""
		if cfg.Current == name {
			mark = "（当前）"
		}
		fmt.Fprintf(w, "context %s%s\n", name, mark)
		fmt.Fprintf(w, "  地址: %s\n", orUnset(c.URL))
		fmt.Fprintf(w, "  账号: %s\n", orUnset(c.User))
		if c.Password != "" {
			fmt.Fprintln(w, "  密码: 已设置")
		} else {
			fmt.Fprintln(w, "  密码: <未设置>")
		}
	})
}

func contextRemove(args []string) error {
	if len(args) != 1 {
		return errors.New("用法: nexus-cli context remove <名字>")
	}
	name := args[0]
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	wasCurrent := cfg.Current == name
	if !cfg.Delete(name) {
		return fmt.Errorf("context %q 不存在", name)
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Printf("已删除 context %q", name)
	if wasCurrent {
		fmt.Printf("（原为当前，current 现在是 %s）", orUnset(cfg.Current))
	}
	fmt.Println()
	return nil
}

func normalize(u string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(u), "/"))
}

func currentMark(isCurrent bool) string {
	if isCurrent {
		return "（当前）"
	}
	return ""
}

func from(src string) string {
	if src == "" {
		return ""
	}
	return "（来自" + src + "）"
}

func redact(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= 4 {
		return strings.Repeat("*", len(r))
	}
	return string(r[:4]) + "…"
}

func orUnset(s string) string {
	if s == "" {
		return "<未设置>"
	}
	return s
}

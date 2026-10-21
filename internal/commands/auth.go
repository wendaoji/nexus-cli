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

	"github.com/wendaoji/nexus-cli/internal/creds"
	"github.com/wendaoji/nexus-cli/internal/nexus"
)

const loginUsage = `用法: nexus-cli login --user <账号> [--password <密码> | --password-stdin]

把凭据写入 ` + "`~/.config/nexus-cli/env`" + `（chmod 600），写入前先校验一次。

  --user <账号>          要保存的账号（建议专用只读账号）
  --password <密码>      直接给密码；会出现在 ps / shell history 里
  --password-stdin       从 stdin 读密码（适合脚本/CI）
  -o json, --json        以 JSON 输出结果
  （都不给则交互式输入，不回显）

关于 token：
  Nexus 的 User Token 是 Pro 专属功能（Community 版没有该接口，调用会 404）。
  若你在 Pro 版或 Web UI 里拿到了 user token，直接把它当 --password 传入即可 ——
  Nexus 就是用「token name + passcode」替换「用户名 + 密码」做 Basic 认证的。
`

// Login 保存并校验凭据。
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

	// 写入前先校验，避免把错的凭据存下来后在后续操作里反复 401（还会触发 Nexus 限流）。
	c, err := nexus.New(&creds.Credentials{User: user, Pass: pass})
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

	if err := creds.Save(user, pass); err != nil {
		return err
	}

	type result struct {
		User    string `json:"user"`
		Origin  string `json:"origin"`
		Path    string `json:"path"`
		Checked string `json:"checked"`
	}
	r := result{User: user, Origin: c.Origin, Path: creds.FilePath(), Checked: nexus.ExplainCheck(res)}
	return emit(out.JSON, r, func(w io.Writer) {
		fmt.Fprintln(w, r.Checked)
		fmt.Fprintf(w, "已写入 %s（chmod 600）\n目标: %s\n", r.Path, r.Origin)
		fmt.Fprintln(w, "验证: nexus-cli whoami --check")
	})
}

const whoamiUsage = `用法: nexus-cli whoami [--check] [-o json|--json]

显示当前生效的凭据来源、账号与目标地址。
  --check   再请求一次 /v1/status 确认连通性
`

// Whoami 展示凭据来源与目标。
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

	c, err := creds.Load()
	if err != nil {
		return err
	}

	info := struct {
		Source       string `json:"source"`
		File         string `json:"file"`
		FileExists   bool   `json:"file_exists"`
		User         string `json:"user"`
		UserFromEnv  bool   `json:"user_from_env"`
		PassSet      bool   `json:"password_set"`
		PassLength   int    `json:"password_length"`
		PassFromEnv  bool   `json:"password_from_env"`
		Origin       string `json:"origin"`
		Connectivity string `json:"connectivity,omitempty"`
	}{
		Source:      string(c.Source),
		File:        c.FilePath,
		User:        redact(c.User),
		UserFromEnv: c.EnvUser,
		PassSet:     c.Pass != "",
		PassLength:  len(c.Pass),
		PassFromEnv: c.EnvPass,
		Origin:      nexus.Origin(),
	}
	if st, err := os.Stat(c.FilePath); err == nil && !st.IsDir() {
		info.FileExists = true
	}

	if check {
		cl, err := nexus.New(c)
		if err != nil {
			return err
		}
		res, err := cl.Check(context.Background())
		if err != nil {
			info.Connectivity = "失败: " + err.Error()
		} else if res.Valid() {
			info.Connectivity = nexus.ExplainCheck(res)
		} else {
			info.Connectivity = nexus.ExplainCheck(res)
		}
	}

	err = emit(out.JSON, info, func(w io.Writer) {
		fmt.Fprintf(w, "凭据来源: %s\n", info.Source)
		exists := ""
		if !info.FileExists {
			exists = "（不存在）"
		}
		fmt.Fprintf(w, "凭据文件: %s%s\n", info.File, exists)
		fmt.Fprintf(w, "NEXUS_USER: %s%s\n", orUnset(info.User), fromEnv(info.UserFromEnv))
		if info.PassSet {
			fmt.Fprintf(w, "NEXUS_PASSWORD: 已设置（%d 字符）%s\n", info.PassLength, fromEnv(info.PassFromEnv))
		} else {
			fmt.Fprintln(w, "NEXUS_PASSWORD: <未设置>")
		}
		fmt.Fprintf(w, "目标: %s\n", info.Origin)
		if info.Connectivity != "" {
			fmt.Fprintf(w, "连通性: %s\n", info.Connectivity)
		}
	})
	if err != nil {
		return err
	}
	if check && !strings.Contains(info.Connectivity, "有效") {
		return errors.New("连通性检查未通过")
	}
	return nil
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

func fromEnv(b bool) string {
	if b {
		return "（来自环境变量）"
	}
	return ""
}

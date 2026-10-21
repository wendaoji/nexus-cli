// Package creds 管理 Nexus 凭据。
//
// 优先级：环境变量 > 凭据文件。文件只「填补」环境里没设置的字段，
// 所以 CI/agent 用环境变量、日常用 `nexus-cli login` 写下的文件，互不打架。
//
// 文件路径遵循 XDG：$XDG_CONFIG_HOME/nexus-cli/env，默认 ~/.config/nexus-cli/env
// （macOS 也用 ~/.config，与 Restish 自身的配置路径习惯一致）。权限 0600。
package creds

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	EnvUser = "NEXUS_USER"
	EnvPass = "NEXUS_PASSWORD"
)

// Source 表示凭据来自哪里，用于 whoami 展示与排查。
type Source string

const (
	SourceNone Source = "未设置"
	SourceEnv  Source = "环境变量"
	SourceFile Source = "凭据文件"
)

// Credentials 是一次解析后的生效凭据。
type Credentials struct {
	User     string
	Pass     string
	Source   Source
	EnvUser  bool // 用户名是否来自环境变量（用于区分「环境变量优先」的粒度）
	EnvPass  bool
	FilePath string
}

// FilePath 返回凭据文件路径。
func FilePath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return filepath.Join(os.TempDir(), "nexus-cli", "env")
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "nexus-cli", "env")
}

// Load 解析生效凭据。环境变量优先，文件补齐。
func Load() (*Credentials, error) {
	c := &Credentials{Source: SourceNone, FilePath: FilePath()}

	c.User = os.Getenv(EnvUser)
	c.Pass = os.Getenv(EnvPass)
	c.EnvUser = c.User != ""
	c.EnvPass = c.Pass != ""

	if c.User == "" || c.Pass == "" {
		fileVals, err := readFile(c.FilePath)
		if err != nil {
			return nil, err
		}
		filled := false
		if c.User == "" {
			if v, ok := fileVals[EnvUser]; ok {
				c.User, filled = v, true
			}
		}
		if c.Pass == "" {
			if v, ok := fileVals[EnvPass]; ok {
				c.Pass, filled = v, true
			}
		}
		if filled {
			c.Source = SourceFile
		}
	}
	if c.EnvUser || c.EnvPass {
		// 只要有任何一项来自环境变量，就标注为环境变量（whoami 里再分项显示）
		c.Source = SourceEnv
	}
	return c, nil
}

// Export 把凭据写回环境，供嵌入的 Restish 解析 `env:NEXUS_USER` 形式的引用。
func (c *Credentials) Export() {
	if c == nil {
		return
	}
	if c.User != "" {
		_ = os.Setenv(EnvUser, c.User)
	}
	if c.Pass != "" {
		_ = os.Setenv(EnvPass, c.Pass)
	}
}

// Complete 表示用户名和密码都齐全。
func (c *Credentials) Complete() bool {
	return c != nil && c.User != "" && c.Pass != ""
}

// Save 以 0600 写入凭据文件。
func Save(user, pass string) error {
	path := FilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建目录 %s: %w", filepath.Dir(path), err)
	}
	body := strings.Join([]string{
		"# 由 `nexus-cli login` 生成。勿提交、勿外泄。",
		"# 优先级：环境变量 " + EnvUser + " / " + EnvPass + " 会覆盖这里的值。",
		EnvUser + "=" + user,
		EnvPass + "=" + pass,
		"",
	}, "\n")

	// 先以 0600 创建，避免出现「短暂可读」的窗口
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("写入 %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(body); err != nil {
		return fmt.Errorf("写入 %s: %w", path, err)
	}
	return os.Chmod(path, 0o600)
}

// readFile 解析 KEY=VALUE 形式的凭据文件。文件不存在时返回空且不报错。
func readFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 %s: %w", path, err)
	}
	defer f.Close()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		switch k {
		case EnvUser, EnvPass:
			out[k] = strings.TrimSpace(v)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("读取 %s: %w", path, err)
	}
	return out, nil
}

// Command nexus-cli 是 Nexus Repository Manager 的 CLI。
//
// 架构：单一静态二进制，内嵌 Restish（源码依赖）+ 内嵌 OpenAPI spec。
//   - 生成的命令（repo/component/search/... 共 409 个操作）由嵌入的 Restish 提供
//   - Restish 表达不了的能力（multipart 上传、context/凭据、任务轮询、MCP）由本仓库实现
//
// 这样用户只需要一个文件：不需要 restish 可执行文件、jq、curl、python 或 shell。
package main

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	restish "github.com/rest-sh/restish/v2"

	"github.com/wendaoji/nexus-cli/internal/commands"
	"github.com/wendaoji/nexus-cli/internal/config"
	"github.com/wendaoji/nexus-cli/internal/nexus"
	"github.com/wendaoji/nexus-cli/internal/specs"
)

// version 由 -ldflags "-X main.version=..." 注入（make build / goreleaser）。
var version = "dev"

// resolveVersion 在 ldflags 没注入时回退到构建信息里的模块版本。
// 这样 `go install ...@v1.2.3` 装出来的二进制也会显示正确版本，而不是 dev。
func resolveVersion() string {
	if version != "" && version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return version
}

const apiName = "nexus-cli"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "nexus-cli: %v\n", err)
		// task wait 超时用 124（与 coreutils timeout 一致，且不与 Restish 的
		// 1=执行错误 / 2=用法错误 / 4=HTTP 错误冲突）。
		var te *commands.TimeoutError
		if errors.As(err, &te) {
			os.Exit(124)
		}
		os.Exit(1)
	}
}

func run() error {
	// 全局 --context / --url 先抽出来，这样对所有子命令都生效，
	// 包括透传给 Restish 的命令。剩下的参数才交给后续逻辑。
	args := extractGlobals(os.Args[1:])

	// 解析一次上下文：供自有命令复用，并导出给嵌入的 Restish。
	// login 允许 context 尚不存在（它的职责就是创建），其余命令严格校验。
	resolveFn := config.Resolve
	if len(args) > 0 && args[0] == "login" {
		resolveFn = config.ResolveForLogin
	}
	r, err := resolveFn(commands.GlobalContext, commands.GlobalURL, nexus.DefaultOrigin)
	if err != nil {
		return err
	}
	// 只解析这一次：后续命令直接复用，避免 Restish 用的环境变量反过来污染解析。
	commands.Current = r
	// Restish 的 profile 用 env:NEXUS_USER / env:NEXUS_PASSWORD 取凭据，
	// 这里把解析结果注入环境（只覆盖为空的值，不覆盖用户显式导出的）。
	exportCreds(r)

	// 自有命令必须在进入 Restish 之前拦截：嵌入 API 只提供
	// SetCommandName/DefaultConfig/CommandSurface 等定制点，没有 AddCommand。
	if handled, err := dispatch(args); handled {
		return err
	}

	files, err := specs.Files()
	if err != nil {
		return err
	}

	cli := restish.New()
	cli.SetCommandName("nexus-cli")
	cli.SetCommandDescription(
		"Nexus Repository Manager CLI",
		"Nexus Repository Manager CLI —— 内嵌 Restish 与 OpenAPI spec，单文件分发。",
	)
	cli.SetVersion(resolveVersion())
	cli.SetDefaultConfig(&restish.Config{APIs: map[string]*restish.APIConfig{
		apiName: {
			BaseURL:       r.URL,
			SpecFiles:     files,
			CommandLayout: "tags",
			RetryMaxWait:  "3s",
			Profiles: map[string]*restish.ProfileConfig{
				"default": {
					Auth: &restish.AuthConfig{
						Type: "http-basic",
						Params: map[string]string{
							"username": "env:" + config.EnvUser,
							"password": "env:" + config.EnvPass,
						},
					},
				},
			},
		},
	}})
	cli.SetCommandSurface(restish.CommandSurface{
		PromotedAPI: apiName,
		// 生成的命令提到根；内置命令收到 restish 命名空间下，
		// 既避免与 Nexus 的 config 分组撞名，也保留排障入口。
		SupportCommandNamespace: "restish",
	})
	// 传抽出全局参数后的 argv（Restish 拿到的参数里不含 --context/--url）
	return cli.Run(append([]string{os.Args[0]}, args...))
}

// extractGlobals 抽出全局 --context / --url，写入 commands 包的全局变量。
// 支持 "--flag value" 与 "--flag=value" 两种写法。
func extractGlobals(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--context":
			if i+1 < len(args) {
				commands.GlobalContext = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--context="):
			commands.GlobalContext = strings.TrimPrefix(a, "--context=")
		case a == "--url":
			if i+1 < len(args) {
				commands.GlobalURL = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--url="):
			commands.GlobalURL = strings.TrimPrefix(a, "--url=")
		default:
			out = append(out, a)
		}
	}
	return out
}

// exportCreds 把解析出的凭据导出到环境，供 Restish 的 env: 引用解析。
func exportCreds(r *config.Resolved) {
	if r == nil {
		return
	}
	if r.User != "" && os.Getenv(config.EnvUser) == "" {
		_ = os.Setenv(config.EnvUser, r.User)
	}
	if r.Pass != "" && os.Getenv(config.EnvPass) == "" {
		_ = os.Setenv(config.EnvPass, r.Pass)
	}
}

// dispatch 拦截自有命令。返回 handled=false 表示交给 Restish。
func dispatch(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "login":
		return true, commands.Login(rest)
	case "whoami":
		return true, commands.Whoami(rest)
	case "context":
		return true, commands.ContextCmd(rest)
	case "upload":
		return true, commands.Upload(rest)
	case "upload-dir":
		return true, commands.UploadDir(rest)
	case "mcp":
		return true, commands.MCP(rest)
	case "version", "--version":
		fmt.Printf("nexus-cli %s\n", resolveVersion())
		return true, nil
	case "task":
		// task 组里只有 wait 是我们的，其它（list/get/run/...）交给 Restish。
		if len(rest) > 0 && rest[0] == "wait" {
			return true, commands.TaskWait(rest[1:])
		}
	}
	return false, nil
}

// Command nexus-cli 是 Nexus Repository Manager 的 CLI。
//
// 架构：单一静态二进制，内嵌 Restish（源码依赖）+ 内嵌 OpenAPI spec。
//   - 生成的命令（repo/component/search/... 共 409 个操作）由嵌入的 Restish 提供
//   - Restish 表达不了的能力（multipart 上传、凭据管理、任务轮询、MCP）由本仓库实现
//
// 这样用户只需要一个文件：不需要 restish 可执行文件、jq、curl、python 或 shell。
package main

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"

	restish "github.com/rest-sh/restish/v2"

	"github.com/wendaoji/nexus-cli/internal/commands"
	"github.com/wendaoji/nexus-cli/internal/creds"
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
	// 凭据先解析并导出到环境，供 Restish 解析 profile 里的 env:NEXUS_USER / env:NEXUS_PASSWORD。
	c, err := creds.Load()
	if err != nil {
		return err
	}
	c.Export()

	// 自有命令必须在进入 Restish 之前拦截：嵌入 API 只提供
	// SetCommandName/DefaultConfig/CommandSurface 等定制点，没有 AddCommand。
	if handled, err := dispatch(os.Args[1:]); handled {
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
			BaseURL:       nexus.Origin(),
			SpecFiles:     files,
			CommandLayout: "tags",
			RetryMaxWait:  "3s",
			Profiles: map[string]*restish.ProfileConfig{
				"default": {
					Auth: &restish.AuthConfig{
						Type: "http-basic",
						Params: map[string]string{
							"username": "env:" + creds.EnvUser,
							"password": "env:" + creds.EnvPass,
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
	return cli.Run(os.Args)
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

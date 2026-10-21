// Command nexus-cli is the Nexus Repository Manager CLI.
//
// Architecture: a single static binary that embeds Restish (as a source
// dependency) and the OpenAPI spec.
//   - Generated commands (repo/component/search/... - 409 operations) come from
//     the embedded Restish
//   - What Restish cannot express (multipart upload, contexts/credentials, task
//     polling, MCP) is implemented in this repository
//
// Users therefore need exactly one file: no restish binary, jq, curl, python or
// shell.
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
	"github.com/wendaoji/nexus-cli/internal/i18n"
	"github.com/wendaoji/nexus-cli/internal/nexus"
	"github.com/wendaoji/nexus-cli/internal/specs"
)

// version is injected via -ldflags "-X main.version=..." (make build / goreleaser).
var version = "dev"

// resolveVersion falls back to the module version from the build info when
// ldflags were not set, so `go install ...@v1.2.3` does not report "dev".
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
	// Language must be resolved before any output, including error messages.
	i18n.Set(extractLang(os.Args[1:]))

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "nexus-cli: %v\n", err)
		// task wait timeout uses 124 (matching coreutils timeout, and not
		// colliding with Restish's 1=error / 2=usage / 4=HTTP status).
		var te *commands.TimeoutError
		if errors.As(err, &te) {
			os.Exit(124)
		}
		os.Exit(1)
	}
}

func run() error {
	// Pull global flags first so they work for every subcommand, including the
	// ones passed through to Restish.
	args := extractGlobals(os.Args[1:])

	// Resolve the context once: reuse it for our own commands and export it for
	// the embedded Restish. login may create a context that does not exist yet;
	// every other command validates strictly.
	resolveFn := config.Resolve
	if len(args) > 0 && args[0] == "login" {
		resolveFn = config.ResolveForLogin
	}
	r, err := resolveFn(commands.GlobalContext, commands.GlobalURL, nexus.DefaultOrigin)
	if err != nil {
		return err
	}
	commands.Current = r
	// Restish profiles read credentials from env:NEXUS_USER / env:NEXUS_PASSWORD.
	exportCreds(r)

	// Our own commands must be intercepted before Restish: the embedding API
	// only offers SetCommandName/DefaultConfig/CommandSurface, not AddCommand.
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
		i18n.T("Nexus Repository Manager CLI - embedded Restish and OpenAPI spec, single-file distribution."),
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
		// Generated commands move to the root; built-in commands move under the
		// "restish" namespace so they neither collide with Nexus's "config"
		// group nor disappear as a debugging escape hatch.
		SupportCommandNamespace: "restish",
	})
	// Pass argv without the global flags we consumed.
	return cli.Run(append([]string{os.Args[0]}, args...))
}

// extractGlobals pulls --context / --url out of argv and stores them in the
// commands package globals. Supports "--flag value" and "--flag=value".
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
		case a == "--lang":
			// 已在 main 开头读取，这里只负责把它从 argv 里去掉，
			// 否则会传给不识别该标志的子命令（如 version）。
			if i+1 < len(args) {
				i++
			}
		case strings.HasPrefix(a, "--lang="):
			// 同上
		default:
			out = append(out, a)
		}
	}
	return out
}

// extractLang finds --lang / --lang=value without consuming it from argv
// (commands may still want to see it, though currently none do).
func extractLang(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--lang" && i+1 < len(args):
			return args[i+1]
		case strings.HasPrefix(a, "--lang="):
			return strings.TrimPrefix(a, "--lang=")
		}
	}
	return ""
}

// exportCreds exports resolved credentials for Restish's env: references.
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

// dispatch intercepts our own commands. handled=false means "pass to Restish".
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
		return true, commands.Version(rest, resolveVersion())
	case "task":
		// Only "task wait" is ours; list/get/run/... go to Restish.
		if len(rest) > 0 && rest[0] == "wait" {
			return true, commands.TaskWait(rest[1:])
		}
	}
	return false, nil
}

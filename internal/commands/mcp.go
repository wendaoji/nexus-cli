package commands

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/wendaoji/nexus-cli/internal/mcpserver"
)

const mcpUsage = `用法: nexus-cli mcp [--read-only | --allow-destructive] [-o 无]

以 MCP server 方式运行（stdio），供 AI agent 接入。

档位:
  默认                    读操作 + 生产性写（构件上传、任务触发）
  --read-only             仅 GET/HEAD
  --allow-destructive     额外放开 delete*、security 变更、configuration set、script run
`

// MCP 启动内嵌的 MCP server。
func MCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var readOnly, destructive bool
	fs.BoolVar(&readOnly, "read-only", false, "只暴露读操作")
	fs.BoolVar(&destructive, "allow-destructive", false, "放开破坏性操作")
	fs.Usage = func() { fmt.Fprint(os.Stderr, mcpUsage) }

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(os.Stdout, mcpUsage)
			return nil
		}
		return err
	}
	if readOnly && destructive {
		return errors.New("mcp: --read-only 与 --allow-destructive 互斥")
	}

	// 实现在 internal/mcpserver。
	return runMCPServer(readOnly, destructive)
}

var runMCPServer = func(readOnly, destructive bool) error {
	return mcpserver.Serve(mcpserver.Options{
		ReadOnly:           readOnly,
		IncludeDestructive: destructive,
	})
}

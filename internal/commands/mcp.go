package commands

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/wendaoji/nexus-cli/internal/i18n"
	"github.com/wendaoji/nexus-cli/internal/mcpserver"
)

const mcpUsageEN = `Usage: nexus-cli mcp [--read-only | --allow-destructive]

Runs an MCP server over stdio for AI agents.

Tiers:
  default               reads plus productive writes (artifact upload, task run)
  --read-only           GET/HEAD only
  --allow-destructive   additionally exposes deletes, security changes,
                        configuration set, and script run
`

func mcpUsage() string { return i18n.H("help.mcp", mcpUsageEN) }

// MCP starts the embedded MCP server.
func MCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var readOnly, destructive bool
	fs.BoolVar(&readOnly, "read-only", false, "expose read operations only")
	fs.BoolVar(&destructive, "allow-destructive", false, "expose destructive operations")
	fs.Usage = func() { fmt.Fprint(os.Stderr, mcpUsage()) }

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(os.Stdout, mcpUsage())
			return nil
		}
		return err
	}
	if readOnly && destructive {
		return errors.New(i18n.T("mcp: --read-only and --allow-destructive are mutually exclusive"))
	}

	return runMCPServer(readOnly, destructive)
}

var runMCPServer = func(readOnly, destructive bool) error {
	return mcpserver.Serve(mcpserver.Options{
		ReadOnly:           readOnly,
		IncludeDestructive: destructive,
		Target:             Current,
	})
}

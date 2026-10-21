package mcpserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/wendaoji/nexus-cli/internal/config"
	"github.com/wendaoji/nexus-cli/internal/i18n"
)

// Options 控制暴露哪些工具。
type Options struct {
	// ReadOnly 只暴露 GET 操作。
	ReadOnly bool
	// IncludeDestructive 额外暴露破坏性操作（delete、security 变更、script run 等）。
	IncludeDestructive bool
	// Target 是调用方已解析好的上下文；为空时退化为按环境重新解析。
	Target *config.Resolved
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve 在 stdin/stdout 上运行 MCP server（stdio 传输，按行分隔 JSON-RPC）。
func Serve(opt Options) error {
	ops, err := LoadOperations(opt.IncludeDestructive)
	if err != nil {
		return err
	}
	if opt.ReadOnly {
		filtered := ops[:0]
		for _, o := range ops {
			if o.Method == "GET" || o.Method == "HEAD" {
				filtered = append(filtered, o)
			}
		}
		ops = filtered
	}

	tools := make([]toolDef, 0, len(ops))
	byName := make(map[string]*Operation, len(ops))
	for i := range ops {
		t := buildTool(&ops[i])
		tools = append(tools, t)
		byName[ops[i].Name] = &ops[i]
	}

	// 手工实现的工具（multipart 上传等）
	synByName := map[string]syntheticTool{}
	for _, s := range syntheticTools(opt.ReadOnly) {
		tools = append(tools, s.def)
		synByName[s.def.Name] = s
	}

	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })

	// 诊断信息走 stderr：stdout 必须只承载协议消息。
	fmt.Fprintf(os.Stderr, i18n.T("nexus-cli mcp: %d tools (read_only=%v, destructive=%v)")+"\n",
		len(tools), opt.ReadOnly, opt.IncludeDestructive)

	in := bufio.NewReaderSize(os.Stdin, 1<<20)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	for {
		line, err := in.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		line = []byte(strings.TrimSpace(string(line)))
		if len(line) == 0 {
			if err != nil {
				return nil
			}
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			writeErr(out, nil, -32700, i18n.T("failed to parse JSON: %s", err.Error()))
			continue
		}

		// 通知（无 id）不需要响应
		isNotification := len(req.ID) == 0 || string(req.ID) == "null"

		switch req.Method {
		case "initialize":
			if !isNotification {
				writeResult(out, req.ID, map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo": map[string]any{
						"name":    "nexus-cli",
						"version": "1",
					},
				})
			}
		case "notifications/initialized":
			// 无需响应
		case "ping":
			if !isNotification {
				writeResult(out, req.ID, map[string]any{})
			}
		case "tools/list":
			if !isNotification {
				writeResult(out, req.ID, map[string]any{"tools": tools})
			}
		case "tools/call":
			if !isNotification {
				result := handleCall(byName, synByName, opt.Target, req.Params)
				writeResult(out, req.ID, result)
			}
		default:
			if !isNotification {
				writeErr(out, req.ID, -32601, i18n.T("unsupported method: %s", req.Method))
			}
		}

		if err != nil {
			return nil
		}
	}
}

func writeResult(w *bufio.Writer, id json.RawMessage, result any) {
	_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
	_ = w.Flush()
}

func writeErr(w *bufio.Writer, id json.RawMessage, code int, msg string) {
	_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
	_ = w.Flush()
}

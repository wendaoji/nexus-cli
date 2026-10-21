package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"gogs.wendaoji.com/wdj/nexus-cli/internal/creds"
	"gogs.wendaoji.com/wdj/nexus-cli/internal/nexus"
)

type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

// buildTool 把一条操作转成 MCP 工具定义。
//
// JSON 请求体的字段被「摊平」到顶层参数里（而不是塞进一个嵌套的 body），
// 这样模型更容易填对；非对象请求体才退化成一个 body 字符串参数。
func buildTool(op *Operation) toolDef {
	props := map[string]any{}
	var required []string

	for _, p := range op.Params {
		if p.In == "cookie" {
			continue // cookie 参数对 MCP 不友好，忽略
		}
		schema := map[string]any{}
		for k, v := range p.Schema {
			schema[k] = v
		}
		schema["x-in"] = p.In
		if p.Description != "" && schema["description"] == nil {
			schema["description"] = p.Description
		}
		props[p.Name] = schema
		if p.Required {
			required = append(required, p.Name)
		}
	}

	if bodyProps, ok := objectProps(op.BodySchema); ok {
		for k, v := range bodyProps {
			if _, exists := props[k]; !exists {
				props[k] = v
			}
		}
		if op.BodyRequired {
			if req, ok := op.BodySchema["required"].([]any); ok {
				for _, r := range req {
					if s, ok := r.(string); ok {
						required = append(required, s)
					}
				}
			}
		}
	} else if op.BodySchema != nil {
		props["body"] = map[string]any{
			"description": "请求体（JSON）",
			"x-in":        "body",
		}
		if op.BodyRequired {
			required = append(required, "body")
		}
	}

	desc := strings.TrimSpace(op.Summary)
	if desc == "" {
		desc = op.Method + " " + op.Path
	}
	desc = fmt.Sprintf("%s  [%s %s]", desc, op.Method, op.Path)
	if op.Destructive {
		desc += "  ⚠ 破坏性操作"
	}

	return toolDef{
		Name:        op.Name,
		Description: desc,
		InputSchema: map[string]any{
			"type":       "object",
			"properties": props,
			"required":   dedupeStrings(required),
		},
	}
}

func objectProps(schema map[string]any) (map[string]any, bool) {
	if schema == nil {
		return nil, false
	}
	if t, _ := schema["type"].(string); t != "object" {
		return nil, false
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		return nil, false
	}
	return props, true
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func handleCall(byName map[string]*Operation, synByName map[string]syntheticTool, raw json.RawMessage) map[string]any {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return toolError("参数解析失败: " + err.Error())
	}
	op, isOp := byName[p.Name]
	syn, isSyn := synByName[p.Name]
	if !isOp && !isSyn {
		return toolError("未知工具: " + p.Name)
	}
	if p.Arguments == nil {
		p.Arguments = map[string]any{}
	}

	cr, err := creds.Load()
	if err != nil {
		return toolError("读取凭据失败: " + err.Error())
	}
	if !cr.Complete() {
		return toolError("缺少凭据：先运行 `nexus-cli login --user <账号>` 或设置 NEXUS_USER / NEXUS_PASSWORD")
	}
	c, err := nexus.New(cr)
	if err != nil {
		return toolError(err.Error())
	}

	var (
		status int
		body   string
	)
	if isSyn {
		status, body, err = syn.call(p.Arguments, c)
	} else {
		status, body, err = execute(c, op, p.Arguments)
	}
	if err != nil {
		return toolError(err.Error())
	}
	return map[string]any{
		"content": []any{map[string]any{
			"type": "text",
			"text": fmt.Sprintf("HTTP %d\n%s", status, body),
		}},
		"isError": status >= 400,
	}
}

// execute 把工具参数翻译成一次真实 HTTP 请求。
func execute(c *nexus.Client, op *Operation, args map[string]any) (int, string, error) {
	path := op.Path
	q := url.Values{}
	headers := map[string]string{}
	bodyArgs := map[string]any{}

	// 先按参数声明分配：path 替换、query 收集、header 收集
	for _, p := range op.Params {
		v, ok := args[p.Name]
		if !ok || v == nil {
			continue
		}
		switch p.In {
		case "path":
			path = strings.ReplaceAll(path, "{"+p.Name+"}", url.PathEscape(argString(v)))
		case "query":
			for _, s := range argStrings(v) {
				q.Add(p.Name, s)
			}
		case "header":
			headers[p.Name] = argString(v)
		}
	}

	// 其余参数归入请求体
	if op.BodySchema != nil {
		if raw, ok := args["body"]; ok {
			bodyArgs = nil
			b, err := json.Marshal(raw)
			if err != nil {
				return 0, "", fmt.Errorf("序列化 body: %w", err)
			}
			return send(c, op, path, q, headers, b)
		}
		if bodyProps, ok := objectProps(op.BodySchema); ok {
			for k := range bodyProps {
				if v, exists := args[k]; exists {
					bodyArgs[k] = v
				}
			}
		}
	}

	if len(bodyArgs) > 0 {
		b, err := json.Marshal(bodyArgs)
		if err != nil {
			return 0, "", fmt.Errorf("序列化请求体: %w", err)
		}
		return send(c, op, path, q, headers, b)
	}
	return send(c, op, path, q, headers, nil)
}

func send(c *nexus.Client, op *Operation, path string, q url.Values, headers map[string]string, body []byte) (int, string, error) {
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var rdr io.Reader
	if body != nil {
		rdr = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(context.Background(), op.Method, c.URL(path), rdr)
	if err != nil {
		return 0, "", err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if c.User != "" || c.Pass != "" {
		req.SetBasicAuth(c.User, c.Pass)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	return resp.StatusCode, string(raw), nil
}

func toolError(msg string) map[string]any {
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": msg}},
		"isError": true,
	}
}

// argString 把任意 JSON 值转成查询串/路径里的字符串。
func argString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		if t == math.Trunc(t) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case nil:
		return ""
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(b)
	}
}

// argStrings 展开数组型参数（query 数组在 Nexus 里是重复 key）。
func argStrings(v any) []string {
	if arr, ok := v.([]any); ok {
		out := make([]string, 0, len(arr))
		for _, e := range arr {
			out = append(out, argString(e))
		}
		return out
	}
	return []string{argString(v)}
}

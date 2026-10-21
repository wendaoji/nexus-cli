// Package mcpserver 把内嵌的 OpenAPI spec 暴露成 MCP 工具。
//
// 与上游 restish 的 MCP 插件不同，这里是进程内运行：不需要宿主进程，
// HTTP 请求由 internal/nexus 直接发出（凭据来自 nexus-cli 自己的凭据文件）。
package mcpserver

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pb33f/libopenapi"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"gopkg.in/yaml.v3"

	nexuscli "github.com/wendaoji/nexus-cli"
	"github.com/wendaoji/nexus-cli/internal/i18n"
	"github.com/wendaoji/nexus-cli/internal/specs"
)

// Operation 是一条可作为 MCP 工具暴露的 API 操作。
type Operation struct {
	Name         string
	OperationID  string
	Method       string
	Path         string
	Summary      string
	Description  string
	Params       []Param
	BodySchema   map[string]any // nil 表示无 JSON 请求体
	BodyRequired bool
	Destructive  bool
}

// Param 是一个操作参数。
type Param struct {
	Name        string
	In          string // path | query | header | cookie
	Required    bool
	Description string
	Schema      map[string]any
}

type methodOp struct {
	method string
	op     *v3.Operation
}

// LoadOperations 解析内嵌 spec，返回按工具名排序的操作列表。
// includeDestructive=false 时过滤掉破坏性操作（默认档位）。
func LoadOperations(includeDestructive bool) ([]Operation, error) {
	names, err := specs.EmbeddedOrder()
	if err != nil {
		return nil, err
	}
	vendor, err := nexuscli.SpecFS.ReadFile("spec/" + names[0])
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("read vendor spec"), err)
	}
	doc, err := libopenapi.NewDocument(vendor)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("parse vendor spec"), err)
	}
	model, err := doc.BuildV3Model()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("build OpenAPI model"), err)
	}
	if model.Model.Paths == nil || model.Model.Paths.PathItems == nil {
		return nil, fmt.Errorf("%s", i18n.T("spec has no paths"))
	}

	overlays, err := loadOverlays(names[1:])
	if err != nil {
		return nil, err
	}

	var ops []Operation
	for path, item := range model.Model.Paths.PathItems.FromOldest() {
		if item == nil || pathIgnored(overlays, path) {
			continue
		}
		for _, mo := range methodsOf(item) {
			if mo.op == nil || opIgnored(overlays, path, mo.method) {
				continue
			}
			method := strings.ToUpper(mo.method)
			destructive := isDestructive(method, path, opMCPIgnore(overlays, path, mo.method))
			if destructive && !includeDestructive {
				continue
			}
			body, bodyReq := bodySchema(mo.op)
			ops = append(ops, Operation{
				Name:         toolName(overlays, path, mo.method, mo.op, method),
				OperationID:  mo.op.OperationId,
				Method:       method,
				Path:         path,
				Summary:      firstNonEmpty(mo.op.Summary, mo.op.Description),
				Description:  mo.op.Description,
				Params:       collectParams(item, mo.op),
				BodySchema:   body,
				BodyRequired: bodyReq,
				Destructive:  destructive,
			})
		}
	}

	sort.Slice(ops, func(i, j int) bool { return ops[i].Name < ops[j].Name })
	return dedupeNames(ops), nil
}

func methodsOf(item *v3.PathItem) []methodOp {
	return []methodOp{
		{"get", item.Get},
		{"post", item.Post},
		{"put", item.Put},
		{"patch", item.Patch},
		{"delete", item.Delete},
	}
}

// isDestructive 决定一条操作属于哪一档。
//
// 破坏性 = 显式 x-mcp-ignore，或 DELETE，或对 security/configuration/email 的写，
// 或 script run（服务端任意 Groovy 代码）。其余写操作属于「生产性写」。
func isDestructive(method, path string, mcpIgnore bool) bool {
	if mcpIgnore {
		return true
	}
	if method == "GET" || method == "HEAD" || method == "OPTIONS" {
		return false
	}
	switch {
	case method == "DELETE":
		return true
	case strings.HasPrefix(path, "/v1/security/"):
		return true
	case path == "/v1/configuration", path == "/v1/email":
		return true
	case strings.Contains(path, "/script/") && strings.HasSuffix(path, "/run"):
		return true
	}
	return false
}

// toolName 生成 MCP 工具名：<分组>_<动词>。
//
// x-cli-name 是「分组内」的动词（如 list/get），在 MCP 的扁平命名空间里会撞名，
// 所以必须把分组名拼上，得到 repo_list / component_list / security_user_list。
func toolName(overlays []map[string]any, path, method string, op *v3.Operation, httpMethod string) string {
	verb := opString(overlays, path, method, "x-cli-name")
	if verb == "" {
		verb = kebab(op.OperationId)
	}
	if verb == "" {
		verb = strings.ToLower(httpMethod) + "-" + strings.Trim(path, "/")
	}
	if g := groupOf(overlays, path, method, op); g != "" {
		return sanitize(g + "_" + verb)
	}
	return sanitize(verb)
}

// groupOf 取命令分组：覆盖层的 tags[0] 优先，其次 vendor 的 tags[0]。
func groupOf(overlays []map[string]any, path, method string, op *v3.Operation) string {
	for i := len(overlays) - 1; i >= 0; i-- {
		mm := methodMap([]map[string]any{overlays[i]}, path, method)
		if mm == nil {
			continue
		}
		if tags, ok := mm["tags"].([]any); ok && len(tags) > 0 {
			if s, ok := tags[0].(string); ok && s != "" {
				return s
			}
		}
	}
	if len(op.Tags) > 0 {
		return op.Tags[0]
	}
	return ""
}

func sanitize(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func kebab(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// dedupeNames 处理重名（Nexus spec 里有 3 处 operationId 重复）。
func dedupeNames(ops []Operation) []Operation {
	seen := map[string]int{}
	for i := range ops {
		n := ops[i].Name
		if c, ok := seen[n]; ok {
			seen[n] = c + 1
			ops[i].Name = fmt.Sprintf("%s_%d", n, c+1)
			continue
		}
		seen[n] = 1
	}
	return ops
}

func collectParams(item *v3.PathItem, op *v3.Operation) []Param {
	var out []Param
	byKey := map[string]int{}
	add := func(p *v3.Parameter) {
		if p == nil || p.Name == "" {
			return
		}
		key := p.In + ":" + p.Name
		np := Param{
			Name:        p.Name,
			In:          p.In,
			Required:    p.Required != nil && *p.Required,
			Description: p.Description,
			Schema:      paramSchema(p),
		}
		if i, ok := byKey[key]; ok {
			out[i] = np // 操作级覆盖路径级
			return
		}
		byKey[key] = len(out)
		out = append(out, np)
	}
	for _, p := range item.Parameters {
		add(p)
	}
	for _, p := range op.Parameters {
		add(p)
	}
	return out
}

func paramSchema(p *v3.Parameter) map[string]any {
	if p.Schema == nil {
		return map[string]any{"type": "string"}
	}
	s := p.Schema.Schema()
	if s == nil {
		return map[string]any{"type": "string"}
	}
	out := map[string]any{"type": "string"}
	if len(s.Type) > 0 && s.Type[0] != "" {
		out["type"] = s.Type[0]
	}
	if s.Description != "" {
		out["description"] = s.Description
	}
	if len(s.Enum) > 0 {
		vals := make([]any, 0, len(s.Enum))
		for _, e := range s.Enum {
			vals = append(vals, e.Value)
		}
		out["enum"] = vals
	}
	if s.Default != nil && s.Default.Value != "" {
		out["default"] = s.Default.Value
	}
	if s.Items != nil && s.Items.A != nil {
		if it := s.Items.A.Schema(); it != nil && len(it.Type) > 0 && it.Type[0] != "" {
			out["items"] = map[string]any{"type": it.Type[0]}
		}
	}
	return out
}

func bodySchema(op *v3.Operation) (map[string]any, bool) {
	if op.RequestBody == nil || op.RequestBody.Content == nil {
		return nil, false
	}
	mt, ok := op.RequestBody.Content.Get("application/json")
	if !ok || mt == nil || mt.Schema == nil {
		return nil, false
	}
	s := mt.Schema.Schema()
	if s == nil {
		return nil, false
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, false
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false
	}
	req := op.RequestBody.Required != nil && *op.RequestBody.Required
	return out, req
}

// ── overlay 查表 ────────────────────────────────────────────────────────────

func loadOverlays(names []string) ([]map[string]any, error) {
	var out []map[string]any
	for _, n := range names {
		b, err := nexuscli.SpecFS.ReadFile("spec/" + n)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", i18n.T("read overlay %s", n), err)
		}
		var m map[string]any
		if err := yaml.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("%s: %w", i18n.T("parse overlay %s", n), err)
		}
		out = append(out, m)
	}
	return out, nil
}

func pathMap(overlays []map[string]any, path string) map[string]any {
	for i := len(overlays) - 1; i >= 0; i-- {
		paths, _ := overlays[i]["paths"].(map[string]any)
		if paths == nil {
			continue
		}
		if pm, ok := paths[path].(map[string]any); ok {
			return pm
		}
	}
	return nil
}

func methodMap(overlays []map[string]any, path, method string) map[string]any {
	pm := pathMap(overlays, path)
	if pm == nil || method == "" {
		return nil
	}
	mm, _ := pm[method].(map[string]any)
	return mm
}

func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true"
	}
	return false
}

func extBool(overlays []map[string]any, path, method, key string) bool {
	if mm := methodMap(overlays, path, method); mm != nil && truthy(mm[key]) {
		return true
	}
	if pm := pathMap(overlays, path); pm != nil {
		return truthy(pm[key])
	}
	return false
}

func opString(overlays []map[string]any, path, method, key string) string {
	if mm := methodMap(overlays, path, method); mm != nil {
		if s, ok := mm[key].(string); ok {
			return s
		}
	}
	return ""
}

func pathIgnored(overlays []map[string]any, path string) bool {
	if pm := pathMap(overlays, path); pm != nil && truthy(pm["x-cli-ignore"]) {
		return true
	}
	return false
}

func opIgnored(overlays []map[string]any, path, method string) bool {
	return extBool(overlays, path, method, "x-cli-ignore")
}

func opMCPIgnore(overlays []map[string]any, path, method string) bool {
	return extBool(overlays, path, method, "x-mcp-ignore")
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

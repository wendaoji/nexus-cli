package mcpserver

import (
	"context"
	"fmt"

	"gogs.wendaoji.com/wdj/nexus-cli/internal/nexus"
)

// syntheticTool 是那些「不是简单 GET/POST JSON」的操作：它们需要 multipart
// 或额外的客户端逻辑，因此没有对应的 OpenAPI 工具，而是手工实现。
type syntheticTool struct {
	def  toolDef
	call func(args map[string]any, c *nexus.Client) (int, string, error)
}

// 构件上传是 agent 最常用的写操作，而 Nexus 的 components 接口要求 multipart
// 里用字面量点号字段名，无法用通用的「JSON 参数 → 请求体」通道表达，
// 所以单独暴露一个工具。
func componentUploadTool() syntheticTool {
	prop := func(desc string) map[string]any {
		return map[string]any{"type": "string", "description": desc}
	}
	props := map[string]any{
		"repository": prop("目标仓库名"),
		"format":     prop("maven|raw|yum|go|conda|composer|npm|pypi|nuget|helm|rubygems|r|apt"),
		"file":       prop("要上传的文件绝对路径"),
		"group":      prop("maven groupId"),
		"artifact":   prop("maven artifactId"),
		"version":    prop("版本"),
		"packaging":  prop("maven packaging，默认 jar"),
		"classifier": prop("maven classifier"),
		"extension":  prop("扩展名，默认取自文件名"),
		"directory":  prop("raw/yum 的目录前缀"),
		"filename":   prop("raw/yum 的文件名，默认取自路径"),
	}
	return syntheticTool{
		def: toolDef{
			Name: "component_upload",
			Description: "上传单个构件（multipart）。Nexus 的 components 接口要求字面量点号字段名，" +
				"因此单独实现。[POST /v1/components]",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": props,
				"required":   []string{"repository", "format", "file"},
			},
		},
		call: func(args map[string]any, c *nexus.Client) (int, string, error) {
			repo := argString(args["repository"])
			spec := nexus.ComponentSpec{
				Format:     argString(args["format"]),
				File:       argString(args["file"]),
				Group:      argString(args["group"]),
				Artifact:   argString(args["artifact"]),
				Version:    argString(args["version"]),
				Packaging:  argString(args["packaging"]),
				Classifier: argString(args["classifier"]),
				Extension:  argString(args["extension"]),
				Directory:  argString(args["directory"]),
				Filename:   argString(args["filename"]),
			}
			if repo == "" {
				return 0, "", fmt.Errorf("缺少 repository")
			}
			fields, err := nexus.BuildFields(spec)
			if err != nil {
				return 0, "", err
			}
			status, body, err := c.PostComponent(context.Background(), repo, fields)
			return status, string(body), err
		},
	}
}

func syntheticTools(readOnly bool) []syntheticTool {
	if readOnly {
		return nil
	}
	return []syntheticTool{componentUploadTool()}
}

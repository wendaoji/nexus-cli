// Package commands 实现那些 Restish 表达不了、需要自己动手的命令：
// 凭据管理、multipart 上传、批量上传、任务轮询、MCP。
package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
)

// outputFlags 统一处理 -o json / --json。与 Restish 的 -o 语义保持一致。
type outputFlags struct {
	JSON bool
}

// addTo 注册到 flag set。同时支持 --json 和 -o json。
func (o *outputFlags) addTo(fs *flag.FlagSet) {
	fs.BoolVar(&o.JSON, "json", false, "以 JSON 输出")
	fs.Func("o", "输出格式（当前支持 json）", func(v string) error {
		switch v {
		case "json":
			o.JSON = true
			return nil
		case "", "auto":
			return nil
		default:
			return fmt.Errorf("不支持的输出格式 %q（目前只支持 json）", v)
		}
	})
}

// emit 输出一个结构：JSON 模式打印 JSON，否则调用 human 渲染。
func emit(jsonMode bool, v any, human func(io.Writer)) error {
	if jsonMode {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	human(os.Stdout)
	return nil
}

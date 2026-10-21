// Package commands implements everything Restish cannot express: credential and
// context management, multipart upload, batch upload, task polling, and MCP.
package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/wendaoji/nexus-cli/internal/i18n"
)

// outputFlags handles "-o json" / "--json", matching Restish's -o semantics.
type outputFlags struct {
	JSON bool
}

// addTo registers the flags on a flag set.
func (o *outputFlags) addTo(fs *flag.FlagSet) {
	fs.BoolVar(&o.JSON, "json", false, i18n.T("output as JSON"))
	fs.Func("o", i18n.T("output format (currently json)"), func(v string) error {
		switch v {
		case "json":
			o.JSON = true
			return nil
		case "", "auto":
			return nil
		default:
			return fmt.Errorf("%s", i18n.T("unsupported output format %q (only json)", v))
		}
	})
}

// emit writes v as JSON, or calls human for plain text.
func emit(jsonMode bool, v any, human func(io.Writer)) error {
	if jsonMode {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	human(os.Stdout)
	return nil
}

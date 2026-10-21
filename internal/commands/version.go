package commands

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/wendaoji/nexus-cli/internal/i18n"
	"github.com/wendaoji/nexus-cli/internal/specs"
)

const versionUsageEN = `Usage: nexus-cli version [-o json]

Prints the CLI version, the version of the embedded OpenAPI spec, and the
build target. The spec version is what the generated command surface is based
on; it is not necessarily the version of the server you talk to.
`

// Version implements "nexus-cli version".
func Version(args []string, appVersion string) error {
	var out outputFlags
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out.addTo(fs)
	fs.Usage = func() { fmt.Fprint(os.Stderr, i18n.H("help.version", versionUsageEN)) }

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(os.Stdout, i18n.H("help.version", versionUsageEN))
			return nil
		}
		return err
	}

	specVersion, _ := specs.Version()
	specFile, _ := specs.VendorFile()

	info := struct {
		Version     string `json:"version"`
		SpecVersion string `json:"spec_version"`
		SpecFile    string `json:"spec_file"`
		GoVersion   string `json:"go_version"`
		OS          string `json:"os"`
		Arch        string `json:"arch"`
		Lang        string `json:"lang"`
	}{
		Version:     appVersion,
		SpecVersion: specVersion,
		SpecFile:    specFile,
		GoVersion:   runtime.Version(),
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		Lang:        string(i18n.Current()),
	}

	return emit(out.JSON, info, func(w io.Writer) {
		fmt.Fprintf(w, "nexus-cli %s\n", info.Version)
		fmt.Fprintf(w, "%-9s %s (%s)\n", i18n.T("spec"), info.SpecVersion, info.SpecFile)
		fmt.Fprintf(w, "%-9s %s %s/%s\n", "go", info.GoVersion, info.OS, info.Arch)
		fmt.Fprintf(w, "%-9s %s\n", i18n.T("lang"), info.Lang)
	})
}

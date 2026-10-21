package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/wendaoji/nexus-cli/internal/config"
	"github.com/wendaoji/nexus-cli/internal/i18n"
	"github.com/wendaoji/nexus-cli/internal/nexus"
)

const uploadUsageEN = `Usage: nexus-cli upload --repo <repo> --format <format> --file <file> [options]

Uploads a single component. The Nexus components endpoint requires literal
dotted multipart field names (e.g. maven2.asset1); Restish's body shorthand
treats dots as nesting, so this command builds the multipart request itself.

Common options:
  --repo <repo>          required
  --format <format>      maven | raw | yum | go | conda | composer |
                         npm | pypi | nuget | helm | rubygems | r | apt
  --file <file>          required
  --field k=v            extra form field (repeatable)
  --dry-run              print the request instead of sending it
  -o json, --json        JSON output

maven: --group --artifact --version [--packaging jar] [--classifier s] [--extension jar]
raw:   [--directory /path] [--filename name]
yum:   [--directory /x86_64]
go:    --version <v>
conda: [--filename name] [--version v]
composer: [--version v]
`

func uploadUsage() string { return i18n.H("help.upload", uploadUsageEN) }

type uploadOptions struct {
	Repo       string
	Format     string
	File       string
	Directory  string
	Filename   string
	Group      string
	Artifact   string
	Version    string
	Packaging  string
	Classifier string
	Extension  string
	DryRun     bool
	Extra      []string
}

// Upload implements "nexus-cli upload".
func Upload(args []string) error {
	var o uploadOptions
	var out outputFlags
	fs := flag.NewFlagSet("upload", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.Repo, "repo", "", "target repository")
	fs.StringVar(&o.Format, "format", "", "component format")
	fs.StringVar(&o.File, "file", "", "file to upload")
	fs.StringVar(&o.Directory, "directory", "", "directory prefix for raw/yum")
	fs.StringVar(&o.Filename, "filename", "", "file name for raw/yum")
	fs.StringVar(&o.Group, "group", "", "maven groupId")
	fs.StringVar(&o.Group, "groupId", "", "maven groupId")
	fs.StringVar(&o.Artifact, "artifact", "", "maven artifactId")
	fs.StringVar(&o.Artifact, "artifactId", "", "maven artifactId")
	fs.StringVar(&o.Version, "version", "", "version")
	fs.StringVar(&o.Packaging, "packaging", "jar", "maven packaging")
	fs.StringVar(&o.Classifier, "classifier", "", "maven classifier")
	fs.StringVar(&o.Extension, "extension", "", "maven extension")
	fs.BoolVar(&o.DryRun, "dry-run", false, "print the request only")
	extra := &stringList{}
	fs.Var(extra, "field", "extra form field k=v (repeatable)")
	out.addTo(fs)
	fs.Usage = func() { fmt.Fprint(os.Stderr, uploadUsage()) }

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(os.Stdout, uploadUsage())
			return nil
		}
		return err
	}
	o.Extra = *extra

	switch {
	case o.Repo == "":
		return errors.New(i18n.T("upload: --repo is required"))
	case o.Format == "":
		return errors.New(i18n.T("upload: --format is required"))
	case o.File == "":
		return errors.New(i18n.T("upload: --file is required"))
	}
	if st, err := os.Stat(o.File); err != nil || st.IsDir() {
		return fmt.Errorf("%s", i18n.T("upload: not a regular file: %s", o.File))
	}
	if o.Extension == "" {
		o.Extension = strings.TrimPrefix(filepath.Ext(o.File), ".")
	}
	if o.Filename == "" {
		o.Filename = filepath.Base(o.File)
	}

	fields, err := buildFields(o)
	if err != nil {
		return err
	}

	r, err := resolveTarget()
	if err != nil {
		return err
	}
	c, err := nexus.New(r.URL, r.User, r.Pass)
	if err != nil {
		return err
	}
	target := c.URL("/v1/components?repository=" + url.QueryEscape(o.Repo))

	if o.DryRun {
		type plan struct {
			URL    string            `json:"url"`
			Format string            `json:"format"`
			File   string            `json:"file"`
			Fields []nexus.FormField `json:"fields"`
		}
		return emit(out.JSON, plan{URL: target, Format: o.Format, File: o.File, Fields: fields},
			func(w io.Writer) {
				fmt.Fprintf(w, "POST %s\n", target)
				for _, f := range fields {
					if f.File != "" {
						fmt.Fprintf(w, "  -F %s=@%s\n", f.Name, f.File)
					} else {
						fmt.Fprintf(w, "  -F %s=%s\n", f.Name, f.Value)
					}
				}
			})
	}

	// Credentials are only required when actually sending; --dry-run must work
	// without them.
	if !r.Complete() {
		return config.ErrNoCredentials()
	}

	status, body, err := c.PostComponent(context.Background(), o.Repo, fields)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		if status == 401 || status == 403 {
			return fmt.Errorf("%s", i18n.T("upload rejected (HTTP %d): check credentials and write access to %s", status, o.Repo))
		}
		return fmt.Errorf("%s", i18n.T("upload failed (HTTP %d): %s", status, truncate(string(body), 400)))
	}

	type result struct {
		OK     bool   `json:"ok"`
		Status int    `json:"status"`
		File   string `json:"file"`
		Repo   string `json:"repo"`
	}
	return emit(out.JSON, result{true, status, filepath.Base(o.File), o.Repo}, func(w io.Writer) {
		fmt.Fprintln(w, i18n.T("uploaded (HTTP %d): %s -> %s", status, filepath.Base(o.File), o.Repo))
	})
}

// buildFields reuses the nexus field mapping and appends --field values.
func buildFields(o uploadOptions) ([]nexus.FormField, error) {
	f, err := nexus.BuildFields(nexus.ComponentSpec{
		Format:     o.Format,
		File:       o.File,
		Directory:  o.Directory,
		Filename:   o.Filename,
		Group:      o.Group,
		Artifact:   o.Artifact,
		Version:    o.Version,
		Packaging:  o.Packaging,
		Classifier: o.Classifier,
		Extension:  o.Extension,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("upload"), err)
	}
	for _, kv := range o.Extra {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("%s", i18n.T("upload: --field expects k=v, got %q", kv))
		}
		f = append(f, nexus.FormField{Name: k, Value: v})
	}
	return f, nil
}

// stringList is a flag.Value that collects repeated flags.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

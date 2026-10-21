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
	"github.com/wendaoji/nexus-cli/internal/nexus"
)

const uploadUsage = `用法: nexus-cli upload --repo <仓库> --format <格式> --file <文件> [选项]

单文件上传。Nexus 的 components 接口要求 multipart 里用字面量点号字段名
（如 maven2.asset1）；Restish 的 shorthand 会把点号当嵌套结构改写掉，
所以这条命令自己发 multipart。

通用选项:
  --repo <仓库>          必填
  --format <格式>        maven | raw | yum | go | conda | composer |
                         npm | pypi | nuget | helm | rubygems | r | apt
  --file <文件>          必填
  --field k=v            追加任意字段（可重复）
  --dry-run              只打印将要发送的请求，不发送
  -o json, --json        JSON 输出

maven: --group --artifact --version [--packaging jar] [--classifier s] [--extension jar]
raw:   [--directory /path] [--filename name]
yum:   [--directory /x86_64]
go:    --version <v>
conda: [--filename name] [--version v]
composer: [--version v]
`

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

// Upload 实现 nexus-cli upload。
func Upload(args []string) error {
	var o uploadOptions
	var out outputFlags
	fs := flag.NewFlagSet("upload", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.Repo, "repo", "", "目标仓库")
	fs.StringVar(&o.Format, "format", "", "构件格式")
	fs.StringVar(&o.File, "file", "", "要上传的文件")
	fs.StringVar(&o.Directory, "directory", "", "raw/yum 的目录前缀")
	fs.StringVar(&o.Filename, "filename", "", "raw/yum 的文件名")
	fs.StringVar(&o.Group, "group", "", "maven groupId")
	fs.StringVar(&o.Group, "groupId", "", "maven groupId")
	fs.StringVar(&o.Artifact, "artifact", "", "maven artifactId")
	fs.StringVar(&o.Artifact, "artifactId", "", "maven artifactId")
	fs.StringVar(&o.Version, "version", "", "版本")
	fs.StringVar(&o.Packaging, "packaging", "jar", "maven packaging")
	fs.StringVar(&o.Classifier, "classifier", "", "maven classifier")
	fs.StringVar(&o.Extension, "extension", "", "maven 扩展名")
	fs.BoolVar(&o.DryRun, "dry-run", false, "只打印请求")
	extra := &stringList{}
	fs.Var(extra, "field", "追加字段 k=v（可重复）")
	out.addTo(fs)
	fs.Usage = func() { fmt.Fprint(os.Stderr, uploadUsage) }

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(os.Stdout, uploadUsage)
			return nil
		}
		return err
	}
	o.Extra = *extra

	switch {
	case o.Repo == "":
		return errors.New("upload: 需要 --repo")
	case o.Format == "":
		return errors.New("upload: 需要 --format")
	case o.File == "":
		return errors.New("upload: 需要 --file")
	}
	if st, err := os.Stat(o.File); err != nil || st.IsDir() {
		return fmt.Errorf("upload: 文件不存在或不是普通文件: %s", o.File)
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

	// 只有真正要发送时才要求凭据齐全：dry-run 不应依赖凭据。
	if !r.Complete() {
		return config.ErrNoCredentials
	}

	status, body, err := c.PostComponent(context.Background(), o.Repo, fields)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		if status == 401 || status == 403 {
			return fmt.Errorf("上传被拒（HTTP %d）：检查凭据与对 %s 的写权限", status, o.Repo)
		}
		return fmt.Errorf("上传失败（HTTP %d）: %s", status, truncate(string(body), 400))
	}

	type result struct {
		OK     bool   `json:"ok"`
		Status int    `json:"status"`
		File   string `json:"file"`
		Repo   string `json:"repo"`
	}
	return emit(out.JSON, result{true, status, filepath.Base(o.File), o.Repo}, func(w io.Writer) {
		fmt.Fprintf(w, "上传成功 (HTTP %d): %s -> %s\n", status, filepath.Base(o.File), o.Repo)
	})
}

// buildFields 复用 nexus 包的字段映射，并追加 --field 自定义字段。
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
		return nil, fmt.Errorf("upload: %w", err)
	}
	for _, kv := range o.Extra {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("upload: --field 需要 k=v 形式，收到 %q", kv)
		}
		f = append(f, nexus.FormField{Name: k, Value: v})
	}
	return f, nil
}

// stringList 是 flag.Value 的切片实现，用于可重复的 --field。
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

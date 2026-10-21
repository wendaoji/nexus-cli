package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gogs.wendaoji.com/wdj/nexus-cli/internal/nexus"
)

const uploadDirUsage = `用法: nexus-cli upload-dir --repo <仓库> --dir <目录> [选项]

批量上传一个目录。Nexus 没有批量接口，这里在客户端并发。

  --repo <仓库>          必填
  --dir <目录>           必填
  --format raw|maven     默认 raw
  --prefix <路径>        raw 模式的目录前缀，例如 /release/1.0
  --include <glob>       只传匹配的文件（按 basename 匹配）
  --exclude <glob>       跳过匹配的文件（按 basename 匹配）
  --jobs N               并发数（默认 4）
  --dry-run              只打印计划，不发送（不需要凭据）
  -o json, --json        JSON 汇总

raw 模式：保留目录结构，相对路径变成 raw.directory
  ./dist/docs/a.txt  ->  raw.directory=<prefix>/docs, filename=a.txt

maven 模式：按 <group 路径>/<artifactId>/<version>/<文件> 推断坐标
  ./m2/com/example/app/1.0.0/app-1.0.0.jar
    -> groupId=com.example artifactId=app version=1.0.0
  同目录文件按「主件 → pom → 带 classifier」排序，一次请求最多 3 个 asset。
  maven 模式默认跳过 maven-metadata*.xml（Nexus 自己维护）。

失败不会中断，全部跑完后打印失败清单；有任何失败则退出码 1。
`

type uploadDirOptions struct {
	Repo    string
	Format  string
	Dir     string
	Prefix  string
	Include string
	Exclude string
	Jobs    int
	DryRun  bool
}

type jobResult struct {
	Label  string   `json:"target"`
	OK     bool     `json:"ok"`
	Skip   bool     `json:"skipped,omitempty"`
	Err    string   `json:"error,omitempty"`
	Detail []string `json:"detail,omitempty"`
}

type uploadJob struct {
	label string
	plan  []string
	run   func() jobResult
}

// UploadDir 实现 nexus-cli upload-dir。
func UploadDir(args []string) error {
	o := uploadDirOptions{Format: "raw", Jobs: 4}
	var out outputFlags
	fs := flag.NewFlagSet("upload-dir", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.Repo, "repo", "", "目标仓库")
	fs.StringVar(&o.Format, "format", "raw", "raw 或 maven")
	fs.StringVar(&o.Dir, "dir", "", "要上传的目录")
	fs.StringVar(&o.Prefix, "prefix", "", "raw 模式目录前缀")
	fs.StringVar(&o.Include, "include", "", "只传匹配的文件")
	fs.StringVar(&o.Exclude, "exclude", "", "跳过匹配的文件")
	fs.IntVar(&o.Jobs, "jobs", 4, "并发数")
	fs.BoolVar(&o.DryRun, "dry-run", false, "只打印计划")
	out.addTo(fs)
	fs.Usage = func() { fmt.Fprint(os.Stderr, uploadDirUsage) }

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(os.Stdout, uploadDirUsage)
			return nil
		}
		return err
	}
	switch {
	case o.Repo == "":
		return errors.New("upload-dir: 需要 --repo")
	case o.Dir == "":
		return errors.New("upload-dir: 需要 --dir")
	case o.Jobs < 1:
		return errors.New("upload-dir: --jobs 需 >= 1")
	}
	switch o.Format {
	case "raw", "maven", "maven2":
	default:
		return fmt.Errorf("upload-dir: --format 目前支持 raw | maven，收到 %q", o.Format)
	}
	if st, err := os.Stat(o.Dir); err != nil || !st.IsDir() {
		return fmt.Errorf("upload-dir: 目录不存在: %s", o.Dir)
	}

	jobs, err := planUploadDir(o)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return errors.New("upload-dir: 没有匹配到文件（检查 --dir / --include / --exclude）")
	}

	var results []jobResult
	if o.DryRun {
		for _, j := range jobs {
			results = append(results, jobResult{Label: j.label, OK: true, Detail: j.plan})
		}
	} else {
		var (
			mu sync.Mutex
			wg sync.WaitGroup
		)
		sem := make(chan struct{}, o.Jobs)
		for _, j := range jobs {
			j := j
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				r := j.run()
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
			}()
		}
		wg.Wait()
	}

	sort.Slice(results, func(i, k int) bool { return results[i].Label < results[k].Label })
	var ok, failed, skipped int
	for _, r := range results {
		switch {
		case r.Skip:
			skipped++
		case r.OK:
			ok++
		default:
			failed++
		}
	}

	summary := struct {
		Repo    string      `json:"repo"`
		Format  string      `json:"format"`
		Dir     string      `json:"dir"`
		Total   int         `json:"total"`
		OK      int         `json:"ok"`
		Failed  int         `json:"failed"`
		Skipped int         `json:"skipped"`
		DryRun  bool        `json:"dry_run"`
		Results []jobResult `json:"results"`
	}{o.Repo, o.Format, o.Dir, len(results), ok, failed, skipped, o.DryRun, results}

	if err := emit(out.JSON, summary, func(w io.Writer) {
		for _, r := range results {
			switch {
			case r.Skip:
				fmt.Fprintf(w, "SKIP  %s\n", r.Label)
			case o.DryRun:
				fmt.Fprintf(w, "PLAN  %s\n", r.Label)
				for _, d := range r.Detail {
					fmt.Fprintf(w, "        %s\n", d)
				}
			case r.OK:
				fmt.Fprintf(w, "OK    %s\n", r.Label)
			default:
				fmt.Fprintf(w, "FAIL  %s: %s\n", r.Label, r.Err)
			}
		}
		if o.DryRun {
			fmt.Fprintf(w, "\n计划 %d 个目标（未发送）\n", len(results))
			return
		}
		if failed > 0 {
			fmt.Fprintln(w, "\n失败清单:")
			for _, r := range results {
				if !r.OK && !r.Skip {
					fmt.Fprintf(w, "  - %s: %s\n", r.Label, r.Err)
				}
			}
		}
		fmt.Fprintf(w, "\n汇总: 成功 %d / 失败 %d", ok, failed)
		if skipped > 0 {
			fmt.Fprintf(w, " / 跳过 %d", skipped)
		}
		fmt.Fprintf(w, "（共 %d 个目标）\n", len(results))
	}); err != nil {
		return err
	}
	if failed > 0 {
		return fmt.Errorf("有 %d 个目标失败", failed)
	}
	return nil
}

// planUploadDir 枚举并构造任务，不执行。
func planUploadDir(o uploadDirOptions) ([]uploadJob, error) {
	var client *nexus.Client
	if !o.DryRun {
		c, err := newAuthedClient()
		if err != nil {
			return nil, err
		}
		client = c
	}
	base, err := filepath.Abs(o.Dir)
	if err != nil {
		return nil, err
	}
	files, err := walkFiles(base, o)
	if err != nil {
		return nil, err
	}

	var jobs []uploadJob
	switch o.Format {
	case "maven", "maven2":
		byDir := map[string][]string{}
		for _, f := range files {
			d := filepath.Dir(f)
			byDir[d] = append(byDir[d], f)
		}
		dirs := make([]string, 0, len(byDir))
		for d := range byDir {
			dirs = append(dirs, d)
		}
		sort.Strings(dirs)
		for _, d := range dirs {
			jobs = append(jobs, newMavenJob(base, d, byDir[d], o, client))
		}
	default:
		for _, f := range files {
			jobs = append(jobs, newRawJob(base, f, o, client))
		}
	}
	return jobs, nil
}

func walkFiles(base string, o uploadDirOptions) ([]string, error) {
	var out []string
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", ".svn", ".hg":
				return filepath.SkipDir
			}
			return nil
		}
		if !shouldInclude(name, o.Include, o.Exclude, o.Format) {
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// shouldInclude 决定某个文件是否参与上传。枚举阶段与执行阶段共用同一规则。
func shouldInclude(name, includeGlob, excludeGlob, format string) bool {
	switch name {
	case ".DS_Store", ".gitignore":
		return false
	}
	if (format == "maven" || format == "maven2") && includeGlob == "" {
		if ok, _ := filepath.Match("maven-metadata*.xml", name); ok {
			return false
		}
	}
	if includeGlob != "" {
		if ok, _ := filepath.Match(includeGlob, name); !ok {
			return false
		}
	}
	if excludeGlob != "" {
		if ok, _ := filepath.Match(excludeGlob, name); ok {
			return false
		}
	}
	return true
}

func newRawJob(base, file string, o uploadDirOptions, client *nexus.Client) uploadJob {
	rel, _ := filepath.Rel(base, file)
	rel = filepath.ToSlash(rel)
	name := filepath.Base(file)
	dir := filepath.Dir(rel)
	if dir == "." {
		dir = ""
	}
	full := strings.Trim(o.Prefix, "/")
	if dir != "" {
		if full != "" {
			full += "/"
		}
		full += dir
	}

	fields := []nexus.FormField{
		{Name: "raw.asset1", File: file},
		{Name: "raw.asset1.filename", Value: name},
	}
	plan := []string{"asset: " + name}
	if full != "" {
		fields = append(fields, nexus.FormField{Name: "raw.directory", Value: "/" + full})
		plan = append(plan, "directory: /"+full)
	}

	return uploadJob{label: rel, plan: plan, run: func() jobResult {
		return post(client, o.Repo, fields, rel)
	}}
}

func newMavenJob(base, dir string, files []string, o uploadDirOptions, client *nexus.Client) uploadJob {
	rel, _ := filepath.Rel(base, dir)
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	var group, artifact, version string
	if len(parts) >= 2 {
		version = parts[len(parts)-1]
		artifact = parts[len(parts)-2]
		group = strings.Join(parts[:len(parts)-2], ".")
	}
	label := group + ":" + artifact + ":" + version

	// 排序：主件 → pom → 带 classifier → 其它
	pre := artifact + "-" + version
	var prim, poms, classed, other []string
	for _, f := range files {
		n := filepath.Base(f)
		ext := strings.TrimPrefix(filepath.Ext(n), ".")
		stem := strings.TrimSuffix(n, filepath.Ext(n))
		switch {
		case stem == pre && ext == "pom":
			poms = append(poms, f)
		case stem == pre:
			prim = append(prim, f)
		case strings.HasPrefix(stem, pre+"-"):
			classed = append(classed, f)
		default:
			other = append(other, f)
		}
	}
	sort.Strings(prim)
	sort.Strings(poms)
	sort.Strings(classed)
	sort.Strings(other)
	ordered := append(append(append(prim, poms...), classed...), other...)

	// Nexus maven2 每次最多 3 个 asset，超出切分
	var chunks [][]string
	for i := 0; i < len(ordered); i += 3 {
		end := min(i+3, len(ordered))
		chunks = append(chunks, ordered[i:end])
	}

	var plan []string
	for _, chunk := range chunks {
		var parts []string
		for i, f := range chunk {
			n := filepath.Base(f)
			stem := strings.TrimSuffix(n, filepath.Ext(n))
			desc := fmt.Sprintf("asset%d=%s", i+1, n)
			if cls := strings.TrimPrefix(stem, pre+"-"); cls != stem {
				desc += " (classifier=" + cls + ")"
			}
			parts = append(parts, desc)
		}
		plan = append(plan, strings.Join(parts, "  "))
	}

	return uploadJob{label: label, plan: plan, run: func() jobResult {
		for _, chunk := range chunks {
			fields := []nexus.FormField{
				{Name: "maven2.groupId", Value: group},
				{Name: "maven2.artifactId", Value: artifact},
				{Name: "maven2.version", Value: version},
				{Name: "maven2.packaging", Value: "jar"},
			}
			for i, f := range chunk {
				n := filepath.Base(f)
				stem := strings.TrimSuffix(n, filepath.Ext(n))
				ext := strings.TrimPrefix(filepath.Ext(n), ".")
				fields = append(fields, nexus.FormField{Name: fmt.Sprintf("maven2.asset%d", i+1), File: f})
				if ext != "" {
					fields = append(fields, nexus.FormField{Name: fmt.Sprintf("maven2.asset%d.extension", i+1), Value: ext})
				}
				if strings.HasPrefix(stem, pre+"-") {
					cls := strings.TrimPrefix(stem, pre+"-")
					fields = append(fields, nexus.FormField{Name: fmt.Sprintf("maven2.asset%d.classifier", i+1), Value: cls})
				}
			}
			if r := post(client, o.Repo, fields, label); !r.OK {
				return r
			}
		}
		return jobResult{Label: label, OK: true}
	}}
}

func post(c *nexus.Client, repo string, fields []nexus.FormField, label string) jobResult {
	if c == nil {
		return jobResult{Label: label, OK: true}
	}
	status, body, err := c.PostComponent(context.Background(), repo, fields)
	if err != nil {
		return jobResult{Label: label, Err: err.Error()}
	}
	if status < 200 || status >= 300 {
		return jobResult{Label: label, Err: fmt.Sprintf("HTTP %d: %s", status, truncate(string(body), 200))}
	}
	return jobResult{Label: label, OK: true}
}

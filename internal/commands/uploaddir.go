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

	"github.com/wendaoji/nexus-cli/internal/i18n"
	"github.com/wendaoji/nexus-cli/internal/nexus"
)

const uploadDirUsageEN = `Usage: nexus-cli upload-dir --repo <repo> --dir <directory> [options]

Uploads a whole directory. Nexus has no batch endpoint, so this runs requests
concurrently on the client side.

  --repo <repo>        required
  --dir <directory>    required
  --format raw|maven   default: raw
  --prefix <path>      directory prefix for raw, e.g. /release/1.0
  --include <glob>     only upload files whose base name matches
  --exclude <glob>     skip files whose base name matches
  --jobs N             concurrency (default 4)
  --dry-run            print the plan without sending (no credentials needed)
  -o json, --json      JSON summary

raw mode keeps the directory layout:
  ./dist/docs/a.txt  ->  raw.directory=<prefix>/docs, filename=a.txt

maven mode infers coordinates from <group path>/<artifactId>/<version>/<file>:
  ./m2/com/example/app/1.0.0/app-1.0.0.jar
    -> groupId=com.example artifactId=app version=1.0.0
  Files in the same directory are ordered main artifact, then pom, then
  classifiers, with at most 3 assets per request.
  maven mode skips maven-metadata*.xml by default.

Failures do not stop the run; a failure list is printed at the end and the
exit code is 1 if anything failed.
`

func uploadDirUsage() string { return i18n.H("help.upload-dir", uploadDirUsageEN) }

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

// UploadDir implements "nexus-cli upload-dir".
func UploadDir(args []string) error {
	o := uploadDirOptions{Format: "raw", Jobs: 4}
	var out outputFlags
	fs := flag.NewFlagSet("upload-dir", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.Repo, "repo", "", "target repository")
	fs.StringVar(&o.Format, "format", "raw", "raw or maven")
	fs.StringVar(&o.Dir, "dir", "", "directory to upload")
	fs.StringVar(&o.Prefix, "prefix", "", "raw directory prefix")
	fs.StringVar(&o.Include, "include", "", "only upload matching files")
	fs.StringVar(&o.Exclude, "exclude", "", "skip matching files")
	fs.IntVar(&o.Jobs, "jobs", 4, "concurrency")
	fs.BoolVar(&o.DryRun, "dry-run", false, "print the plan only")
	out.addTo(fs)
	fs.Usage = func() { fmt.Fprint(os.Stderr, uploadDirUsage()) }

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(os.Stdout, uploadDirUsage())
			return nil
		}
		return err
	}
	switch {
	case o.Repo == "":
		return errors.New(i18n.T("upload-dir: --repo is required"))
	case o.Dir == "":
		return errors.New(i18n.T("upload-dir: --dir is required"))
	case o.Jobs < 1:
		return errors.New(i18n.T("upload-dir: --jobs must be >= 1"))
	}
	switch o.Format {
	case "raw", "maven", "maven2":
	default:
		return fmt.Errorf("%s", i18n.T("upload-dir: --format supports raw | maven, got %q", o.Format))
	}
	if st, err := os.Stat(o.Dir); err != nil || !st.IsDir() {
		return fmt.Errorf("%s", i18n.T("upload-dir: no such directory: %s", o.Dir))
	}

	jobs, err := planUploadDir(o)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return errors.New(i18n.T("upload-dir: no files matched (check --dir / --include / --exclude)"))
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
			fmt.Fprintf(w, "\n%s\n", i18n.T("planned %d targets (nothing sent)", len(results)))
			return
		}
		if failed > 0 {
			fmt.Fprintln(w, "\n"+i18n.T("failures:"))
			for _, r := range results {
				if !r.OK && !r.Skip {
					fmt.Fprintf(w, "  - %s: %s\n", r.Label, r.Err)
				}
			}
		}
		line := i18n.T("summary: %d ok / %d failed", ok, failed)
		if skipped > 0 {
			line += i18n.T(" / %d skipped", skipped)
		}
		fmt.Fprintf(w, "\n%s%s\n", line, i18n.T(" (%d targets)", len(results)))
	}); err != nil {
		return err
	}
	if failed > 0 {
		return fmt.Errorf("%s", i18n.T("%d targets failed", failed))
	}
	return nil
}

// planUploadDir enumerates jobs without executing them.
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

// shouldInclude decides whether a file takes part. Enumeration and execution
// share this rule.
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
	plan := []string{i18n.T("asset: %s", name)}
	if full != "" {
		fields = append(fields, nexus.FormField{Name: "raw.directory", Value: "/" + full})
		plan = append(plan, i18n.T("directory: /%s", full))
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

	// Order: main artifact, then pom, then classifiers, then anything else.
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

	// Nexus maven2 accepts at most 3 assets per request.
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

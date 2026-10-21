package i18n

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestCatalogCoversSource 扫描源码里所有 i18n.T("...") / i18n.H("...") 的字面量键，
// 断言它们在 catalog / helpText 里有中文翻译。
//
// 目的不是覆盖率数字，而是防止「新增文案忘了翻译」——那会悄悄显示英文，
// 在没有中文语境的测试里很难被发现。
func TestCatalogCoversSource(t *testing.T) {
	root := filepath.Join("..", "..")
	tKey := regexp.MustCompile(`i18n\.T\(\s*"((?:[^"\\]|\\.)*)"`)
	hKey := regexp.MustCompile(`i18n\.H\(\s*"([^"]+)"`)

	var missingT, missingH []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".cache", "dist", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// catalog 自己不算
		if filepath.Base(path) == "zh.go" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(b)
		for _, m := range tKey.FindAllStringSubmatch(src, -1) {
			if _, ok := catalog[m[1]]; !ok {
				missingT = append(missingT, m[1])
			}
		}
		for _, m := range hKey.FindAllStringSubmatch(src, -1) {
			if _, ok := helpText[m[1]]; !ok {
				missingH = append(missingH, m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	for _, k := range dedupe(missingT) {
		t.Errorf("缺少中文翻译 (i18n.T): %q", k)
	}
	for _, k := range dedupe(missingH) {
		t.Errorf("缺少中文帮助文本 (i18n.H): %q", k)
	}
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name     string
		explicit string
		env      map[string]string
		want     Lang
	}{
		{"default is English", "", nil, EN},
		{"LANG zh", "", map[string]string{"LANG": "zh_CN.UTF-8"}, ZH},
		{"LANG en", "", map[string]string{"LANG": "en_US.UTF-8"}, EN},
		{"LC_ALL wins over LANG", "", map[string]string{"LC_ALL": "zh_TW.UTF-8", "LANG": "en_US.UTF-8"}, ZH},
		{"NEXUS_CLI_LANG wins over LC_ALL", "", map[string]string{"NEXUS_CLI_LANG": "en", "LC_ALL": "zh_CN.UTF-8"}, EN},
		{"explicit wins over everything", "zh", map[string]string{"NEXUS_CLI_LANG": "en", "LC_ALL": "en_US.UTF-8"}, ZH},
		{"empty values ignored", "", map[string]string{"LANG": "  ", "LC_ALL": ""}, EN},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, k := range []string{"LANG", "LC_ALL", "LC_MESSAGES", "NEXUS_CLI_LANG"} {
				t.Setenv(k, "")
			}
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			if got := Detect(c.explicit); got != c.want {
				t.Errorf("Detect(%q) = %q, want %q", c.explicit, got, c.want)
			}
		})
	}
}

func TestTranslateFallsBackToEnglish(t *testing.T) {
	Set("zh")
	defer Set("")
	if got := T("Credentials valid (HTTP 200)"); !strings.Contains(got, "凭据有效") {
		t.Errorf("zh translation missing: %q", got)
	}
	// 未知键原样返回，绝不显示键名之外的占位符
	if got := T("no such message"); got != "no such message" {
		t.Errorf("unknown key = %q", got)
	}
	Set("en")
	if got := T("Credentials valid (HTTP 200)"); got != "Credentials valid (HTTP 200)" {
		t.Errorf("en should return the source text, got %q", got)
	}
}

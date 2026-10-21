// Package specs 把内嵌的 OpenAPI spec 落盘到缓存目录，交给嵌入的 Restish 加载。
//
// 为什么要落盘：Restish 的 spec_files 只接受文件路径或 URL，而我们的目标是
// 单文件分发（不要求用户先跑 install.sh、也不写全局 Restish 配置）。
// 缓存目录以内容哈希命名，所以升级 spec 后会自动换目录，不会用到旧的。
package specs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	nexuscli "gogs.wendaoji.com/wdj/nexus-cli"
)

const embedDir = "spec"

// order 决定 deep-merge 顺序（后者覆盖前者）：
// vendor 快照 → 机械层 → 人工层。不要在运行时依赖文件名字典序。
var order = []string{
	"nexus-tags.generated.yaml",
	"nexus-overlay.yaml",
}

// Files 返回可直接传给 Restish APIConfig.SpecFiles 的路径列表。
func Files() ([]string, error) {
	names, err := embeddedNames()
	if err != nil {
		return nil, err
	}
	dir, err := materialize(names)
	if err != nil {
		return nil, err
	}
	paths := make([]string, len(names))
	for i, n := range names {
		paths[i] = filepath.Join(dir, n)
	}
	return paths, nil
}

// EmbeddedOrder 暴露按 deep-merge 顺序排列的内嵌 spec 文件名，供 MCP 复用。
func EmbeddedOrder() ([]string, error) { return embeddedNames() }

// APIRoot 返回 vendor spec 中声明的 API 根路径（Nexus 通常为 /service/rest）。
// 直接 HTTP 调用（上传/校验）需要它，避免把路径硬编码两遍。
func APIRoot() (string, error) {
	names, err := embeddedNames()
	if err != nil {
		return "", err
	}
	b, err := nexuscli.SpecFS.ReadFile(embedDir + "/" + names[0])
	if err != nil {
		return "", fmt.Errorf("读取内嵌 %s: %w", names[0], err)
	}
	var doc struct {
		Servers []struct {
			URL string `json:"url"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return "", fmt.Errorf("解析 %s 的 servers: %w", names[0], err)
	}
	for _, s := range doc.Servers {
		if s.URL != "" {
			return s.URL, nil
		}
	}
	return "/service/rest", nil
}

// embeddedNames 返回按合并顺序排列的文件名。
func embeddedNames() ([]string, error) {
	entries, err := fs.ReadDir(nexuscli.SpecFS, embedDir)
	if err != nil {
		return nil, fmt.Errorf("读取内嵌 spec 目录: %w", err)
	}

	var vendor []string
	rest := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".json") {
			vendor = append(vendor, name)
			continue
		}
		rest[name] = true
	}
	if len(vendor) != 1 {
		return nil, fmt.Errorf("期望恰好一个 vendor spec (.json)，实际 %d 个: %v", len(vendor), vendor)
	}

	names := append([]string{}, vendor...)
	for _, n := range order {
		if rest[n] {
			names = append(names, n)
			delete(rest, n)
		}
	}
	// 未来新增的覆盖层：按名字排序追加，保证确定性
	var extra []string
	for n := range rest {
		extra = append(extra, n)
	}
	sort.Strings(extra)
	return append(names, extra...), nil
}

// materialize 把内嵌文件写到按内容哈希命名的缓存目录，返回该目录。
func materialize(names []string) (string, error) {
	h := sha256.New()
	blobs := make([][]byte, len(names))
	for i, n := range names {
		b, err := nexuscli.SpecFS.ReadFile(embedDir + "/" + n)
		if err != nil {
			return "", fmt.Errorf("读取内嵌 %s: %w", n, err)
		}
		blobs[i] = b
		_, _ = h.Write([]byte(n))
		_, _ = h.Write(b)
	}
	dir := filepath.Join(cacheRoot(), "spec", hex.EncodeToString(h.Sum(nil))[:16])

	// 已落盘则直接复用
	if ok, _ := complete(dir, names); ok {
		return dir, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建缓存目录 %s: %w", dir, err)
	}
	for i, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), blobs[i], 0o644); err != nil {
			return "", fmt.Errorf("写入缓存 %s: %w", n, err)
		}
	}
	return dir, nil
}

func complete(dir string, names []string) (bool, error) {
	for _, n := range names {
		if st, err := os.Stat(filepath.Join(dir, n)); err != nil || st.IsDir() {
			return false, nil
		}
	}
	return true, nil
}

func cacheRoot() string {
	if d, err := os.UserCacheDir(); err == nil && d != "" {
		return filepath.Join(d, "nexus-cli")
	}
	return filepath.Join(os.TempDir(), "nexus-cli")
}

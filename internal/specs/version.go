package specs

import (
	"encoding/json"
	"fmt"

	nexuscli "github.com/wendaoji/nexus-cli"
	"github.com/wendaoji/nexus-cli/internal/i18n"
)

// Version 返回内嵌 vendor spec 的 info.version —— 也就是这份 spec 来自哪个
// Nexus 版本。用于 `nexus-cli version` 与兼容性排查：API 以 spec 为准，
// 而目标服务器版本可能不同。
func Version() (string, error) {
	names, err := embeddedNames()
	if err != nil {
		return "", err
	}
	b, err := nexuscli.SpecFS.ReadFile(embedDir + "/" + names[0])
	if err != nil {
		return "", fmt.Errorf("%s: %w", i18n.T("read embedded %s", names[0]), err)
	}
	var doc struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return "", fmt.Errorf("%s: %w", i18n.T("parse info.version of %s", names[0]), err)
	}
	return doc.Info.Version, nil
}

// VendorFile 返回内嵌 vendor spec 的文件名（用于诊断输出）。
func VendorFile() (string, error) {
	names, err := embeddedNames()
	if err != nil {
		return "", err
	}
	return names[0], nil
}

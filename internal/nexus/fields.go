package nexus

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/wendaoji/nexus-cli/internal/i18n"
)

// ComponentSpec 描述一次构件上传所需的坐标与文件。
// CLI 与 MCP 两条通道共用同一套字段映射，避免两处各写一遍。
type ComponentSpec struct {
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
}

// WithDefaults 补齐从文件名推导出的字段。
func (s ComponentSpec) WithDefaults() ComponentSpec {
	if s.Extension == "" {
		s.Extension = strings.TrimPrefix(filepath.Ext(s.File), ".")
	}
	if s.Filename == "" {
		s.Filename = filepath.Base(s.File)
	}
	if s.Packaging == "" {
		s.Packaging = "jar"
	}
	return s
}

// BuildFields 把格式映射成 Nexus components 接口的 multipart 字段名。
//
// Nexus 要求字面量点号字段名（maven2.asset1）；Restish 的请求体 shorthand 会把
// 点号当嵌套结构，所以这条链路只能自己拼 multipart。
func BuildFields(s ComponentSpec) ([]FormField, error) {
	s = s.WithDefaults()
	var f []FormField
	add := func(name, value string) { f = append(f, FormField{Name: name, Value: value}) }
	addFile := func(name, path string) { f = append(f, FormField{Name: name, File: path}) }

	switch s.Format {
	case "maven", "maven2":
		if s.Group == "" {
			return nil, errors.New(i18n.T("maven requires group"))
		}
		if s.Artifact == "" {
			return nil, errors.New(i18n.T("maven requires artifact"))
		}
		if s.Version == "" {
			return nil, errors.New(i18n.T("maven requires version"))
		}
		add("maven2.groupId", s.Group)
		add("maven2.artifactId", s.Artifact)
		add("maven2.version", s.Version)
		add("maven2.packaging", s.Packaging)
		addFile("maven2.asset1", s.File)
		if s.Extension != "" {
			add("maven2.asset1.extension", s.Extension)
		}
		if s.Classifier != "" {
			add("maven2.asset1.classifier", s.Classifier)
		}
	case "raw":
		addFile("raw.asset1", s.File)
		add("raw.asset1.filename", s.Filename)
		if s.Directory != "" {
			add("raw.directory", s.Directory)
		}
	case "yum":
		addFile("yum.asset", s.File)
		add("yum.asset.filename", s.Filename)
		if s.Directory != "" {
			add("yum.directory", s.Directory)
		}
	case "go":
		if s.Version == "" {
			return nil, errors.New(i18n.T("go requires version"))
		}
		addFile("go.asset", s.File)
		add("go.version", s.Version)
	case "conda":
		addFile("conda.asset", s.File)
		add("conda.filename", s.Filename)
		if s.Version != "" {
			add("conda.version", s.Version)
		}
	case "composer":
		addFile("composer.asset", s.File)
		if s.Version != "" {
			add("composer.version", s.Version)
		}
	case "npm", "pypi", "nuget", "helm", "rubygems", "r", "apt":
		addFile(s.Format+".asset", s.File)
	case "docker", "oci", "huggingface":
		return nil, fmt.Errorf("%s", i18n.T("%q is not uploaded over REST; use a native client such as docker/helm push", s.Format))
	default:
		return nil, fmt.Errorf("%s", i18n.T("unsupported format %q", s.Format))
	}
	return f, nil
}

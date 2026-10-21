package nexus

import (
	"path/filepath"
	"testing"
)

func TestBuildFieldsMaven(t *testing.T) {
	fields, err := BuildFields(ComponentSpec{
		Format:    "maven",
		File:      filepath.Join("tmp", "app-1.0.0.jar"),
		Group:     "com.example",
		Artifact:  "app",
		Version:   "1.0.0",
		Extension: "jar",
	})
	if err != nil {
		t.Fatalf("BuildFields: %v", err)
	}
	got := map[string]string{}
	for _, f := range fields {
		if f.Value != "" {
			got[f.Name] = f.Value
		}
	}
	want := map[string]string{
		"maven2.groupId":          "com.example",
		"maven2.artifactId":       "app",
		"maven2.version":          "1.0.0",
		"maven2.packaging":        "jar",
		"maven2.asset1.extension": "jar",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("字段 %s = %q，期望 %q", k, got[k], v)
		}
	}
	// 文件字段必须是字面量点号名（这是整条链路存在的理由）
	var fileField string
	for _, f := range fields {
		if f.File != "" {
			fileField = f.Name
		}
	}
	if fileField != "maven2.asset1" {
		t.Errorf("文件字段名 = %q，期望 maven2.asset1", fileField)
	}
}

func TestBuildFieldsRaw(t *testing.T) {
	fields, err := BuildFields(ComponentSpec{
		Format:    "raw",
		File:      "/tmp/docs/a.txt",
		Directory: "/docs",
	})
	if err != nil {
		t.Fatalf("BuildFields: %v", err)
	}
	got := map[string]string{}
	for _, f := range fields {
		if f.Value != "" {
			got[f.Name] = f.Value
		}
	}
	if got["raw.asset1.filename"] != "a.txt" {
		t.Errorf("filename = %q", got["raw.asset1.filename"])
	}
	if got["raw.directory"] != "/docs" {
		t.Errorf("directory = %q", got["raw.directory"])
	}
}

func TestBuildFieldsMavenRequiresCoordinates(t *testing.T) {
	if _, err := BuildFields(ComponentSpec{Format: "maven", File: "x.jar"}); err == nil {
		t.Fatal("缺少坐标时应当报错")
	}
}

func TestBuildFieldsRejectsDocker(t *testing.T) {
	if _, err := BuildFields(ComponentSpec{Format: "docker", File: "x.tar"}); err == nil {
		t.Fatal("docker 不走 REST 上传，应当报错")
	}
}

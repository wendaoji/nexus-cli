package creds

import (
	"os"
	"path/filepath"
	"testing"
)

func withTempConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv(EnvUser, "")
	t.Setenv(EnvPass, "")
}

func TestSaveAndLoadFromFile(t *testing.T) {
	withTempConfig(t)

	if err := Save("readonly", "s3cret"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	st, err := os.Stat(FilePath())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Errorf("权限 = %o，期望 600", perm)
	}

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !c.Complete() || c.User != "readonly" || c.Pass != "s3cret" {
		t.Errorf("读回的凭据不符: %+v", c)
	}
	if c.Source != SourceFile {
		t.Errorf("来源 = %q，期望 %q", c.Source, SourceFile)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	withTempConfig(t)
	if err := Save("fileuser", "filepass"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	t.Setenv(EnvUser, "envuser")
	t.Setenv(EnvPass, "envpass")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.User != "envuser" || c.Pass != "envpass" {
		t.Errorf("环境变量应优先，得到 %+v", c)
	}
	if c.Source != SourceEnv {
		t.Errorf("来源 = %q，期望 %q", c.Source, SourceEnv)
	}
}

// 只设置了一个环境变量时，另一个应由文件补齐。
func TestFileFillsMissingEnvField(t *testing.T) {
	withTempConfig(t)
	if err := Save("fileuser", "filepass"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	t.Setenv(EnvUser, "envuser")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.User != "envuser" {
		t.Errorf("用户名应来自环境变量，得到 %q", c.User)
	}
	if c.Pass != "filepass" {
		t.Errorf("密码应由文件补齐，得到 %q", c.Pass)
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	withTempConfig(t)
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Complete() {
		t.Errorf("没有凭据时 Complete() 应为 false: %+v", c)
	}
}

func TestFilePathRespectsXDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	want := filepath.Join(dir, "nexus-cli", "env")
	if got := FilePath(); got != want {
		t.Errorf("FilePath = %q，期望 %q", got, want)
	}
}

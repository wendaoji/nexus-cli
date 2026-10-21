package mcpserver

import "testing"

func loadTier(t *testing.T, destructive bool) map[string]Operation {
	t.Helper()
	ops, err := LoadOperations(destructive)
	if err != nil {
		t.Fatalf("LoadOperations(%v): %v", destructive, err)
	}
	if len(ops) == 0 {
		t.Fatal("没有解析出任何操作")
	}
	byName := map[string]Operation{}
	for _, o := range ops {
		if _, dup := byName[o.Name]; dup {
			t.Fatalf("工具名重复: %s", o.Name)
		}
		byName[o.Name] = o
	}
	return byName
}

func TestDefaultTierExcludesDestructive(t *testing.T) {
	def := loadTier(t, false)
	for name, op := range def {
		if op.Destructive {
			t.Errorf("默认档位出现了破坏性操作: %s", name)
		}
		if op.Method == "DELETE" {
			t.Errorf("默认档位出现了 DELETE: %s", name)
		}
	}
}

func TestDefaultTierHasProductiveWrites(t *testing.T) {
	def := loadTier(t, false)
	for _, want := range []string{"task_run", "component_upload_unsupported"} {
		// component_upload_unsupported 被 x-mcp-ignore 屏蔽（这个接口本身是坏的），
		// 上传改由 synthetic tool 提供，见测试下方。
		if want == "component_upload_unsupported" {
			if _, ok := def[want]; ok {
				t.Errorf("%s 不应暴露（Restish shorthand 会破坏点号字段名）", want)
			}
			continue
		}
		if _, ok := def[want]; !ok {
			t.Errorf("默认档位缺少生产性写操作 %s", want)
		}
	}
}

func TestDestructiveTierIncludesDangerous(t *testing.T) {
	all := loadTier(t, true)
	for _, want := range []string{"script_run", "repo_delete", "component_delete"} {
		if _, ok := all[want]; !ok {
			t.Errorf("破坏性档位缺少 %s", want)
		}
	}
	hasDelete := false
	for _, op := range all {
		if op.Method == "DELETE" {
			hasDelete = true
			break
		}
	}
	if !hasDelete {
		t.Error("破坏性档位应包含 DELETE 操作")
	}
}

func TestToolNamingIsGroupedAndFlat(t *testing.T) {
	def := loadTier(t, false)
	for _, want := range []string{"repo_list", "component_list", "asset_list", "status_show"} {
		if _, ok := def[want]; !ok {
			t.Errorf("缺少工具 %s", want)
		}
	}
}

func TestOperationsCarryParams(t *testing.T) {
	def := loadTier(t, false)
	op, ok := def["component_list"]
	if !ok {
		t.Fatal("缺少 component_list")
	}
	found := false
	for _, p := range op.Params {
		if p.Name == "repository" && p.In == "query" {
			found = true
			if !p.Required {
				t.Error("listComponents 的 repository 参数应为必填")
			}
		}
	}
	if !found {
		t.Error("component_list 缺少 query 参数 repository")
	}
}

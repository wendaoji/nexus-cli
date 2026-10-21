package commands

import (
	"github.com/wendaoji/nexus-cli/internal/config"
	"github.com/wendaoji/nexus-cli/internal/nexus"
)

// 全局 --context / --url：由 main 在分发前统一提取，因此对所有子命令
// （包括透传给 Restish 的命令）都生效，例如：
//
//	nexus-cli --context dev repo list
//	nexus-cli repo list --context dev
var (
	GlobalContext string
	GlobalURL     string
)

// Current 是 main 解析一次后向下传递的结果。
//
// 为什么必须只解析一次：main 会把凭据导出成 NEXUS_USER / NEXUS_PASSWORD
// 供嵌入的 Restish 使用，而 Resolve 又会读这两个变量。若各命令各自再解析，
// 就会把自己刚写入的环境变量当成「凭据来自环境变量」，来源显示错误。
var Current *config.Resolved

// resolveTarget 返回本次调用的目标地址与凭据。
func resolveTarget() (*config.Resolved, error) {
	if Current != nil {
		return Current, nil
	}
	return config.Resolve(GlobalContext, GlobalURL, nexus.DefaultOrigin)
}

// newAuthedClient 构造一个凭据齐全的客户端。
func newAuthedClient() (*nexus.Client, error) {
	r, err := resolveTarget()
	if err != nil {
		return nil, err
	}
	if !r.Complete() {
		return nil, config.ErrNoCredentials
	}
	return nexus.New(r.URL, r.User, r.Pass)
}

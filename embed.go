// Package nexuscli 是 nexus-cli 的根包，只负责把内嵌资源暴露出去。
//
// 嵌入必须发生在被嵌目录的父目录所在包（go:embed 不支持 ".."），
// 所以 embed 指令放在模块根而不是 internal/ 下。
package nexuscli

import "embed"

// SpecFS 内嵌了 spec/ 目录下的全部文件：
//
//	nexus-<version>.json        vendor 快照（Nexus 原始 swagger）
//	nexus-tags.generated.yaml   机械层（scripts/gen-tags.sh 生成）
//	nexus-overlay.yaml          人工语义层
//
// 运行时由 internal/specs 落盘到缓存目录再交给嵌入的 Restish 加载 ——
// Restish 的 spec_files 只接受文件路径，而我们的目标是单文件分发。
//
//go:embed spec
var SpecFS embed.FS

// DefaultOriginText 是编译期默认的 Nexus 地址（单一来源）。
// 运行时 NEXUS_URL 会覆盖它；维护脚本 scripts/sync-spec.sh 也读同一个文件。
//
//go:embed config/default-origin.txt
var DefaultOriginText string

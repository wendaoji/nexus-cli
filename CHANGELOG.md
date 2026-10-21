# Changelog

本项目遵循语义化版本。破坏性变更会标 **BREAKING**。

## v0.3.0

- **新增** 中英双语（默认英文）。按 `--lang` > `NEXUS_CLI_LANG` > `LC_ALL` >
  `LC_MESSAGES` > `LANG` 选择，`zh*` 为中文。帮助文本与错误信息都已覆盖。
- **新增** `nexus-cli version` 输出内嵌 OpenAPI spec 版本、Go 版本与语言，
  支持 `-o json`；用于回答「这份 CLI 对应哪个 Nexus API 版本」。
- **变更** 编译期默认地址改为 `http://127.0.0.1:8081`（Nexus 默认发布地址）。
  需要连自己的实例时用 `nexus-cli login --url …` 或 `NEXUS_URL`。
- **新增** MIT License。
- **文档** README 精简为安装/使用/开发；架构与演进决策移到 `docs/DESIGN.md`；
  历史记录集中到本文件。

## v0.2.0（BREAKING）

- **BREAKING** 凭据与目标改为命名 context，配置移到
  `$XDG_CONFIG_HOME/nexus-cli/config.json`（0600）。旧版单主机
  `~/.config/nexus-cli/env` 会在首次运行自动迁移为 context `default`，
  原文件备份为 `env.migrated`。
- **新增** `nexus-cli context list|use|show|remove`；
  `login` 支持 `--url` 与 `--context`。
- **新增** 全局 `--context` / `--url`，对所有子命令生效（含透传给 Restish 的命令）。
- **新增** 按地址自动匹配 context 的凭据。
- **修复** 目标地址与 context 记录不一致时 `whoami` 报错退出，避免拿 A 的凭据打 B。
- **修复** 凭据来源误报为「环境变量」（解析结果只计算一次并向下传递）。

## v0.1.2

- **修复** `go install …@vX.Y.Z` 装出的二进制显示 `dev`：无 ldflags 时回退到
  构建信息里的模块版本。

## v0.1.1

- **变更** 模块路径改为 `github.com/wendaoji/nexus-cli`，使 `go install` 可用。

## v0.1.0

- 首个单二进制版本：内嵌 Restish 与 OpenAPI spec，零运行期依赖。
- 覆盖 409 个 REST 操作；自有 `upload` / `upload-dir` / `login` / `whoami` /
  `task wait` / `mcp`。
- MCP 三档：`--read-only` / 默认（读 + 生产性写）/ `--allow-destructive`。
- 多平台发布（macOS/Linux/Windows × amd64/arm64）。

## 更早的实现（已移除）

最初的实现是「bash wrapper + 外部 restish 可执行文件 + jq + curl」，
用户需要 3~5 个组件且不支持 Windows。v0.1.0 起被单二进制方案取代，
相关脚本（`bin/nexus-cli`、`config/restish.api.json`、`scripts/install.sh`）已删除。

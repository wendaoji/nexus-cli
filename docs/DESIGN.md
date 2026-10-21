# 设计说明

这份文档记录 **为什么这样实现**，以及后续演进时需要守住的东西。
日常使用看 [README](../README.md)，版本历史看 [CHANGELOG](../CHANGELOG.md)。

## 目标与非目标

目标：

- 用 Nexus 的 REST API 覆盖日常运维与发布流程，命令面与 Nexus 自身一致
- 交付形态尽量简单：单文件、零运行期依赖、跨平台
- 对 AI agent 友好（结构化输出、可发现的命令面、MCP）
- 凭据可管理：多目标、可查来源、不误用

非目标：

- 不追求 100% 复刻 Nexus Web UI 的全部管理功能（Pro 专属接口不实现）
- 不替代构建工具（Maven/Gradle/npm 仍应直连仓库上传）
- 不内置重试式批量编排（只做客户端并发，不做工作流引擎）

## 架构

```
nexus-cli（单静态二进制）
├── 嵌入 Restish（源码依赖 github.com/rest-sh/restish/v2）
│     OpenAPI → 命令生成、-o 输出、-f 过滤、auth、重试、内置命令
├── 嵌入 OpenAPI spec（go:embed）
│     spec/nexus-<version>.json         vendor 快照
│     spec/nexus-tags.generated.yaml    机械层（脚本生成）
│     spec/nexus-overlay.yaml           人工语义层
└── 自有实现（internal/）
      ├── config/      context 解析（地址 + 凭据 + 优先级）
      ├── nexus/       HTTP 客户端、multipart 字段映射
      ├── commands/    upload / upload-dir / login / whoami / context / task wait / mcp
      ├── mcpserver/   MCP server（进程内，spec → 工具）
      ├── specs/       内嵌 spec 落盘与版本信息
      └── i18n/        中英文案
```

`main()` 在进入 Restish 之前拦截自有命令，其余全部交给嵌入的 Restish。

## 关键决策

### 1. 为什么内嵌 Restish，而不是调用可执行文件或自研

Restish 提供 OpenAPI → CLI 的能力（409 个操作零手写），并且官方公开了嵌入 API
（`restish.New()` / `SetDefaultConfig` / `SetCommandSurface` / `Run`）。

- 调用外部 `restish` 二进制：用户要多装一个程序，且 bash wrapper 不支持 Windows
- 自研命令生成：等于重写 Restish
- **内嵌**：`SetDefaultConfig` 把 base_url 与 spec 编进二进制，用户无需任何全局配置

注意：嵌入 API 没有 `AddCommand`，所以自有命令在 `main()` 里拦截，不注册进 Restish。

### 2. 为什么 spec 分三层

```
vendor 快照        原样保存，可重新拉取
机械层（生成）     path 前缀 → 分组名；所有 DELETE → x-mcp-ignore
人工语义层         认证方案、x-cli-name、隐藏 Pro 接口、额外的 mcp 屏蔽
```

Restish 按 `spec_files` 顺序 deep-merge（对象递归、标量覆盖、数组覆盖），
所以定制不必改 vendor 文件，git diff 干净。

⚠️ **deep-merge 会凭空造出 method**：如果 overlay 给一个 vendor 里不存在的
method 写覆盖（例如给只有 GET/DELETE 的 path 加 `put:`），会生成一个缺参数的
假接口。`scripts/validate-overlay.py` 专门拦这个，改 overlay 后必须跑。

### 3. 为什么上传自己实现

Restish 的请求体 shorthand 里 `.` 永远是嵌套语义，无法转义：

```
输入:      maven2.asset1: @app.jar
Restish:   name="maven2[asset1]"（字段名被改写，二进制部分被丢弃）
Nexus 要求: name="maven2.asset1" 且 filename="app.jar"
```

因此 multipart 由本工具用 `mime/multipart` 直接构造。字段映射集中在
`internal/nexus/fields.go`，CLI 与 MCP 两条通道共用，避免两处各写一遍。

### 4. context 模型与优先级

凭据文件早期只有 `NEXUS_USER`/`NEXUS_PASSWORD`，没有主机维度：`NEXUS_URL=… login`
之后不带 `NEXUS_URL` 再执行命令，会拿 A 的凭据打 B，且无任何提示。改为命名
context 后：

```
context 名：--context > NEXUS_CONTEXT > current
地址      ：--url > NEXUS_URL > context.url > 编译期默认
凭据      ：NEXUS_USER/PASSWORD > context 凭据
```

另有便利规则：显式地址与某 context 的 url 相同时，自动用它的凭据。
地址与 context 记录不一致时 `whoami` 报错退出。

**解析只做一次**：`main` 把结果导出成 `NEXUS_USER`/`NEXUS_PASSWORD` 供嵌入的
Restish 使用；若各命令再各自解析，会把自己刚写的环境变量误报成「来自环境变量」。

### 5. i18n

源码文案以**英文**书写（默认语言），中文放在 `internal/i18n/zh.go` 的
`catalog`（键为英文原文）与 `helpText`（多行帮助）。缺失翻译时回退英文，
不会出现键名。

语言检测：`--lang` > `NEXUS_CLI_LANG` > `LC_ALL` > `LC_MESSAGES` > `LANG` > 默认 en；
值以 `zh` 开头即为中文。

`internal/i18n` 有个测试会扫描源码里所有 `i18n.T("…")` / `i18n.H("…")` 字面量，
断言它们在 catalog 里存在——防止新增文案忘了翻译。

### 6. MCP 三档

`x-mcp-ignore`（以及代码里的规则）把操作分成三档，默认档位对齐「agent 要能干活，
但不能随手做不可逆操作」：

| 档位 | 判定 |
|---|---|
| `--read-only` | 仅 GET/HEAD |
| 默认 | 非破坏性操作（读 + 上传 + 任务触发/停止） |
| `--allow-destructive` | 破坏性操作：DELETE、`/v1/security/*` 的写、`configuration`、`email`、`script run` |

这不是安全边界：agent 若有 shell 权限可直接用 CLI。它保护的是「只有 MCP、
没有 shell」的客户端。

### 7. MCP 只做 stdio，不提供网络监听

**凭据来源**：MCP 用的是**运行 `nexus-cli mcp` 那台机器**上的 context
（`$XDG_CONFIG_HOME/nexus-cli/config.json`，或 `NEXUS_USER`/`NEXUS_PASSWORD`）。
MCP 客户端不传 Nexus 凭据；`--context` / `--url` 全局参数对 `mcp` 同样生效。

上下文**只在启动时解析一次**：MCP 运行期间改配置不会热加载，需要重启进程。
这是有意的——避免每个请求都读盘，也保证一次会话内身份稳定。

**为什么不做 `--listen`**：

- 进程是 MCP 客户端拉起的子进程，stdio 就是通道。这是 MCP 的常规形态，
  Claude Code / Cursor / Codex 都按这个方式接入。
- MCP server 本质是**持权代理**：谁能连上它，就拿到了那台机器配置的 Nexus
  权限。现在的形态天然安全（进程属于本机用户，通道是私有管道）。一旦开端口，
  就变成「任何能访问该端口的人都能用你的 Nexus 账号」，而且默认档位还包含
  构件上传与任务触发。
- 要安全地开网络，缺的不只是一个监听参数，而是：端点自身的鉴权（Bearer）、
  TLS、以及身份模型（所有客户端共享一个 Nexus 身份，还是每请求透传各自的凭据）。
  这些没想清楚之前，加一个 `--listen` 只会制造一个静默的提权面。

**需要跨机使用时**，用现成的传输层来补，而不是在本工具里重造：

| 场景 | 做法 |
|---|---|
| agent 与 Nexus 不在同一网段 | 让 agent 跑在能访问 Nexus 的机器上，MCP 仍走本地 stdio |
| agent 在本地、Nexus 在远端 | 用 SSH 端口转发或跳板机，`nexus-cli` 与 MCP 进程都留在远端主机 |
| 必须给多用户/多团队开放 | 单独部署一个带鉴权与 TLS 的 MCP 网关，按调用者注入上下文；本项目不承担这个角色 |

原则：**把「谁有权限」交给已有的、经过审计的边界（SSH、VPN、网关），
CLI 只负责在边界内部把那台机器已有的权限用好。**

## 版本与兼容性

命令面由**内嵌 spec** 决定，与目标服务器版本解耦：

- `nexus-cli version` 同时显示 CLI 版本与内嵌 spec 版本
- 服务器更旧：较新的接口返回 404，其余可用
- 服务器更新：已有接口可用；新增接口要刷新 spec（重新构建）后才会出现

因此**升级 Nexus 不需要升级 CLI**；升级 CLI 也不要求服务器同版本。
真正需要关注的是「想用的新接口是否已在命令面里」。

### 升级 spec

```bash
scripts/sync-spec.sh      # 拉取 → 重生成机械层 → 校验 overlay → 删除旧 vendor spec
make test && make verify
git commit && git tag vX.Y.Z     # 触发多平台发布
```

脚本会打印新增/删除的 operation。**删除项若出现在人工层 overlay 里，校验会报错**
（`validate-overlay.py`），按提示清理即可。

### 版本号约定

语义化版本；配置文件格式、命令名、MCP 档位语义发生变化时升主/次版本并在
CHANGELOG 标注破坏性变更。

## 维护脚本

| 脚本 | 何时用 | 依赖 |
|---|---|---|
| `scripts/gen-tags.sh` | 改了分组规则，或 sync-spec 自动调用 | jq |
| `scripts/sync-spec.sh` | Nexus 版本变化、想用新接口 | jq、curl、python3+PyYAML |
| `scripts/validate-overlay.py` | 每次改 `spec/nexus-overlay.yaml` 后 | python3+PyYAML |
| `scripts/verify.sh` | 提交前 / CI | go、jq |

运行 CLI 本身不需要以上任何依赖。

## 已知限制

- 不做自动翻页：Nexus 用 `continuationToken`，需手动循环
- `repo update` 不存在：Nexus 没有通用仓库更新接口，更新是逐格式的
- spec 有 3 处 `operationId` 重复（`updatePlan`/`deletePlan`/
  `updateEvaluationSettingsWithRepos`），MCP 工具名会带 `_2` 后缀
- Community 版没有 user token 接口；有 token 时按密码传入即可
- `upload-dir` 不跳过已存在的坐标（重复上传的行为取决于仓库策略）

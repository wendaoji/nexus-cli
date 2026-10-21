# nexus-cli

Nexus Repository Manager 的 CLI 与 MCP server。

**一个静态二进制，零运行期依赖。** 不需要 shell、Python、Node、jq、curl ——
也不需要单独安装 Restish：Restish 与 Nexus 的 OpenAPI spec 都已内嵌进二进制。

- 命令面由 OpenAPI spec 生成，覆盖目标实例（3.97.0-13 Community）的全部 **409 个 REST 操作**
- 另有手工实现的部分：multipart 上传、批量上传、凭据管理、任务轮询、MCP
- 跨平台：macOS / Linux / Windows，amd64 与 arm64

```
$ nexus-cli repo list -o json
$ nexus-cli component upload --repo maven-releases --file app.jar ...
$ nexus-cli mcp          # 作为 MCP server 供 AI agent 使用
```

---

## 安装

```bash
# 方式一：go install（模块路径即 GitHub 仓库路径）
go install github.com/wendaoji/nexus-cli/cmd/nexus-cli@latest

# 方式二：从 release 下载对应平台的单文件，放到 PATH 即可
#   https://github.com/wendaoji/nexus-cli/releases
#   macOS/Linux: tar xzf nexus-cli_<ver>_<os>_<arch>.tar.gz

# 方式三：从源码构建
git clone git@github.com:wendaoji/nexus-cli.git
cd nexus-cli && make build      # 产出 dist/nexus-cli
```

首次运行会自动把内嵌 spec 落到缓存目录（`~/Library/Caches/nexus-cli` 或
`$XDG_CACHE_HOME/nexus-cli`），以内容哈希命名，升级后自动换目录。
**不写任何全局配置文件。**

---

## 快速开始

```bash
# 1. 写入 context（地址 + 凭据，写入前会先校验一次）
nexus-cli login --user <账号>                     # 交互式输入，不回显
nexus-cli login --user <账号> --password-stdin    # 脚本/CI

# 2. 确认
nexus-cli whoami --check
nexus-cli context list

# 3. 用
nexus-cli status show -o json
nexus-cli repo list -o json
```

默认目标是编译期写入的地址（`config/default-origin.txt`）。接多台 Nexus 时用
context，见下一节。

---

## context（多目标）与凭据

`~/.config/nexus-cli/config.json`（`chmod 600`）保存若干 context：

```jsonc
{
  "current": "prod",
  "contexts": {
    "prod": { "url": "http://172.16.41.26:8081", "user": "ci-readonly", "password": "…" },
    "dev":  { "url": "http://dev-nexus:8081",    "user": "me",          "password": "…" }
  }
}
```

```bash
nexus-cli login --url http://dev-nexus:8081 --user me --context dev   # 新建/更新
nexus-cli context list                 # * 标记当前
nexus-cli context use prod             # 切换
nexus-cli context show [dev]
nexus-cli context remove dev

nexus-cli --context dev repo list      # 单次指定
NEXUS_CONTEXT=dev nexus-cli repo list  # 环境变量指定
```

**解析优先级**（高 → 低）：

| 项 | 顺序 |
|---|---|
| context 名 | `--context` > `NEXUS_CONTEXT` > config 的 `current` |
| 地址 | `--url` > `NEXUS_URL` > 该 context 的 `url` > 编译期默认值 |
| 凭据 | `NEXUS_USER` / `NEXUS_PASSWORD` > 该 context 的凭据 |

还有一条便利规则：**显式给了地址且与某个 context 的 `url` 相同，就自动用那个
context 的凭据**。所以：

```bash
NEXUS_URL=http://dev-nexus:8081 nexus-cli repo list   # 自动匹配到 dev 的凭据
```

这解决了一个真实的风险：以前凭据文件不记录主机，`NEXUS_URL=… login` 之后
不带 `NEXUS_URL` 再执行命令，会拿着 A 的凭据打 B。现在 `whoami` 会检测
「context 记录的地址」与「本次目标地址」不一致并报错：

```bash
$ nexus-cli --context prod --url http://other:8081 whoami
⚠ context "prod" 记录的是 http://172.16.41.26:8081，本次目标是 http://other:8081
```

`login` 用 `GET /v1/repositories` 校验，据此区分：
`200` 有效 / `403` 已认证但无权限（凭据本身有效）/ `401` 凭据无效 / `429` 被限流。
**校验通过才落盘**，避免存下错凭据后到处 401（还会触发 Nexus 的认证限流）。

> **从旧版本升级**：如果你之前用过单主机的 `~/.config/nexus-cli/env`，
> 首次运行会自动迁移成 context `default`（旧文件没有主机信息，地址取编译期默认值），
> 原文件重命名为 `env.migrated` 保留备份。然后用
> `nexus-cli login --context default --url <你的地址>` 补正地址。

> **User Token 是 Nexus Pro 专属功能。** Community 版没有该接口（调用会 404），
> 所以本工具不提供「用 admin 自动铸造 token」。若你在 Pro 版或 Web UI 里拿到了
> user token，直接把它当 `--password` 传入即可 —— Nexus 就是用
> 「token name + passcode」替换「用户名 + 密码」做 Basic 认证的。

---

## 命令面

17 个分组，形如 `nexus-cli <分组> <动词>`：

```bash
nexus-cli --help                       # 分组一览
nexus-cli repo --help                  # 某分组的命令

nexus-cli status show        -o json
nexus-cli repo list          -o json
nexus-cli repo get           <name> -o json
nexus-cli component list     --repository <repo> -o json
nexus-cli asset list         --repository <repo> -o json
nexus-cli search query       --maven-groupId com.example -o json
nexus-cli search versions    --repository <repo> -o json
nexus-cli blobstore list     -o json
nexus-cli task list          -o json
nexus-cli security user-list -o json
```

输出 `-o json|yaml|table|csv|ndjson|lines`，可用 `-f` 投影：

```bash
nexus-cli repo list -o json -f '{name,format,type,url}'
```

Restish 自带的通用命令收在 `restish` 命名空间下（避免与 Nexus 的 `config` 分组撞名）：

```bash
nexus-cli restish get https://api.example.com/items
nexus-cli restish config show
```

### 隐藏项

Community 版不可用的 Pro 接口（LDAP、licensing、Firewall/IQ、证书、malicious-risk、
evaluation、recovery-mode、support zip 等）已在 `spec/nexus-overlay.yaml` 里用
`x-cli-ignore` 隐藏，避免调用后拿到 403。若你的实例是 Pro，删掉对应行并
`scripts/gen-tags.sh` + 重新构建即可。

---

## 上传

Nexus 的 components 接口要求 multipart 里用**字面量点号字段名**（`maven2.asset1`）。
Restish 的请求体 shorthand 会把点号当嵌套结构改写成 `maven2[asset1]` 并丢弃二进制
部分，所以这条链路自己发 multipart（Go 标准库 `mime/multipart`）。

```bash
# 单文件
nexus-cli upload --repo <repo> --format maven --file app.jar \
    --group com.example --artifact app --version 1.0.0 [--packaging jar] [--classifier sources]

nexus-cli upload --repo <repo> --format raw --file notes.txt --directory /docs [--filename notes.txt]
nexus-cli upload --repo <repo> --format npm|pypi|nuget|helm|rubygems|r|apt --file pkg.ext
nexus-cli upload --repo <repo> --format yum --file pkg.rpm --directory /x86_64
nexus-cli upload --repo <repo> --format go  --file mod.zip --version v1.2.3

# 任意字段兜底
nexus-cli upload --repo <repo> --format <fmt> --file f --field 'k=v'

# 先看将发送的请求（不需要凭据）
nexus-cli upload ... --dry-run
```

### 批量目录上传

```bash
# raw：保留目录结构（相对路径 → raw.directory）
nexus-cli upload-dir --repo <raw-repo> --dir ./dist --prefix /release/1.0
#   ./dist/docs/a.txt -> raw.directory=/release/1.0/docs, filename=a.txt

# maven：按 <group 路径>/<artifactId>/<version>/<文件> 推断坐标
nexus-cli upload-dir --repo <maven-repo> --format maven --dir ./m2
#   ./m2/com/example/app/1.0.0/app-1.0.0.jar
#     -> groupId=com.example artifactId=app version=1.0.0
#   同目录文件按「主件 → pom → 带 classifier」排序，一次请求最多 3 个 asset，超出自动切分
```

- `--include` / `--exclude` 按 basename 匹配（glob）；`--jobs N` 并发（默认 4）
- maven 模式默认跳过 `maven-metadata*.xml`（Nexus 自己维护）
- 始终跳过 `.DS_Store` / `.git` / `.svn`
- 失败不中断，跑完打印失败清单；有任何失败则退出码 1
- `--dry-run` 打印每个目标的计划与字段，不需要凭据

---

## MCP（给 AI agent）

```bash
nexus-cli mcp                      # 默认：读 + 生产性写
nexus-cli mcp --read-only          # 只读（仅 GET/HEAD）
nexus-cli mcp --allow-destructive  # 额外放开 delete / security 变更 / script run
```

| 档位 | 工具数 | 内容 |
|---|---|---|
| `--read-only` | 129 | 仅读 |
| 默认 | 297 | 读 + 构件上传 + 任务触发/停止 |
| `--allow-destructive` | 338 | + 所有 DELETE、security/configuration/email 变更、`script run` |

**为什么默认不放开破坏性操作**：不可逆操作（删仓库/删构件/删 blobstore、Groovy
任意代码执行）不该让模型随手可用。注意这不是安全边界 —— 若 agent 同时有 shell 权限，
它可以直接用 CLI；这一层主要保护「只有 MCP、没有 shell」的客户端。

`script run`（服务端任意 Groovy 代码）永远在最严那一档。

客户端配置见 `mcp/mcp.json.example`；agent 行为约定见 `agent/AGENTS.md` /
`agent/SKILL.md`。

**上传在 MCP 里也用得通**：`component_upload` 工具由本仓库手工实现（同样是 multipart），
参数与 `nexus-cli upload` 一致。

---

## 架构

```
nexus-cli (单静态二进制)
├── 嵌入 Restish（源码依赖 github.com/rest-sh/restish/v2）
│     └── OpenAPI → 命令生成、-o 输出、-f 过滤、auth、重试
├── 嵌入 OpenAPI spec（go:embed）
│     ├── spec/nexus-<version>.json        vendor 快照
│     ├── spec/nexus-tags.generated.yaml   机械层（脚本生成）
│     └── spec/nexus-overlay.yaml          人工语义层
└── 自有实现（本仓库）
      ├── upload / upload-dir   multipart（Restish 表达不了点号字段名）
      ├── login / whoami / context   多目标 + 凭据
      ├── task wait             异步任务轮询
      └── mcp                   MCP server（进程内，无宿主）
```

三个要点：

1. **为什么内嵌 Restish 而不是调用它**：`main()` 是我们的，可以在 `cli.Run()` 之前
   拦截自有命令（嵌入 API 没有 `AddCommand`）。用 `SetDefaultConfig` 把 base_url 和
   spec 编进二进制，于是用户除了可选的 `~/.config/nexus-cli/config.json`（context）
   之外不需要任何配置。
2. **为什么 spec 落盘**：Restish 的 `spec_files` 只接受文件路径或 URL，而交付目标是
   单文件。落盘到内容哈希命名的缓存目录，升级后自动换目录。
3. **为什么上传自己实现**：见上文。字段映射在 `internal/nexus/fields.go`，
   CLI 与 MCP 两条通道共用同一套。

解析上下文只做一次（`config.Resolve`），结果既用于自有命令，也导出成
`NEXUS_USER`/`NEXUS_PASSWORD` 给嵌入的 Restish。这样避免了「Restish 用的环境变量
反过来污染下一次解析」的问题 —— 否则 `whoami` 会把自己刚写的环境变量误报成来源。

---

## 构建与发布

```bash
make build        # dist/nexus-cli（-trimpath + 版本注入）
make test         # go test ./...
make vet
make verify       # 构建 + 44 项冒烟验证
```

推 `v*` tag 时，`.github/workflows/release.yml` 会用 goreleaser 产出
macOS/Linux/Windows × amd64/arm64 的多平台产物。

> 当前有两个远端：`github`（`github.com:wendaoji/nexus-cli`，Actions 与 release 在这里跑）
> 和 `origin`（gogs 镜像）。推代码/标签时两边都推，保持一致。

---

## 维护

### 升级 spec（Nexus 版本变化）

```bash
scripts/sync-spec.sh        # 拉取 → 重生成机械层 → 校验 overlay → 清理旧 spec
make build && make verify
```

脚本会打印新增/删除的 operation。**删除项若出现在人工层 overlay 里，校验会报错**，
按提示清理即可。

### 改了人工层 overlay

```bash
scripts/gen-tags.sh                    # 机械层（分组名 + DELETE 屏蔽）
python3 scripts/validate-overlay.py    # 拦截「凭空造出的 method」与命名冲突
make build && make verify
```

维护脚本的依赖（仅维护者）：`go`、`jq`、`python3`（+PyYAML）、`curl`。
**运行 CLI 不需要其中任何一个。**

---

## 退出码

| 退出码 | 含义 |
|---|---|
| `0` | 成功 |
| `1` | 命令失败 / `upload-dir` 有目标失败 |
| `2` | 用法错误（Restish 未知命令/flag） |
| `4` | **HTTP 错误状态**（Restish 生成命令） |
| `124` | `task wait` 超时 |

Restish 生成的命令在 HTTP 错误时会把响应体写到 stdout、退出码 4 —— 脚本请用退出码判断。

---

## 已知限制

- **不会自动翻页**。Nexus 用 `continuationToken`，需手动循环（示例见 `agent/AGENTS.md`）。
- **Nexus 会限流失败的认证**。反复用错凭据会得到 `429 Too many authentication attempts`；
  配置里把重试等待压到 3 秒，遇到 429 请停下来等几分钟。
- **`repo update` 不存在**。Nexus 没有通用仓库更新接口，更新是逐格式的
  （`nexus-cli repo update-repositories-maven-hosted` 类长尾命令）。
- **spec 有 3 处 operationId 重复**（`updatePlan`/`deletePlan`/
  `updateEvaluationSettingsWithRepos`），工具名会带 `_2` 后缀。
- **MCP 的工具名是 `<分组>_<动词>`**（如 `repo_list`），与 CLI 的 `repo list` 对应。

## 安全

- 使用**专用只读账号**，不要用 admin 账号。
- 配置文件（含凭据）权限 `600`；仓库内不含任何凭据（见 `.gitignore`）。
- 地址与 context 记录不一致时 `whoami` 会报错，避免拿 A 的凭据打 B。
- MCP 默认屏蔽破坏性操作；`script run` 需显式 `--allow-destructive`。

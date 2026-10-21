# Nexus Repository Manager CLI

你可以通过 `bin/nexus-cli`（或已注册的 `restish nexus-cli`）操作 Nexus Repository Manager。
命令面由 Nexus 的 OpenAPI spec 生成，覆盖全部 409 个 REST 操作。

## 发现命令

不要凭记忆猜命令名。先看：

```bash
nexus-cli groups                 # 列出所有命令分组
restish nexus-cli --help         # 同上，带分组标题
restish nexus-cli <group> --help # 看某分组下的命令（如 repo / component / search）
```

分组：`asset blobstore capability component config datastore email format lifecycle`
`repo routing script search security status system-nodes task`

## 输出：始终加 -o json

```bash
nexus-cli repo list -o json
nexus-cli repo get maven-releases -o json
nexus-cli component list --repository maven-releases -o json
nexus-cli search query --maven-groupId com.example -o json
nexus-cli task list -o json
```

大响应先用 `-f` 投影，避免把整个 JSON 灌进上下文：

```bash
nexus-cli repo list -o json -f '{name,format,type,url}'
nexus-cli component list --repository maven-releases -o json -f 'items[].{name,version}'
```

其它格式：`-o yaml|table|csv|ndjson|lines`。非 TTY 时 Restish 自动用紧凑 JSON。

## 常用操作

```bash
nexus-cli status show -o json                       # 服务版本与状态
nexus-cli repo list -o json                         # 仓库列表
nexus-cli repo get <name> -o json                   # 单个仓库配置
nexus-cli blobstore list -o json
nexus-cli asset list --repository <repo> -o json
nexus-cli search versions --repository <repo> -o json
nexus-cli task list -o json
nexus-cli task wait <taskId>                        # 轮询异步任务到终态（0/1，超时 124）
nexus-cli whoami                                    # 当前凭据来源与目标地址
```

## 上传构件

**不要用 `restish nexus-cli component upload-unsupported`**。
Restish 的 shorthand 会把点号字段名 `maven2.asset1` 改写成 `maven2[asset1]` 并丢弃
二进制部分，Nexus 会拒绝。改用：

```bash
nexus-cli upload --repo <repo> --format maven --file app.jar \
    --group com.example --artifact app --version 1.0.0

nexus-cli upload --repo <repo> --format raw --file notes.txt --directory /docs
nexus-cli upload --repo <repo> --format npm|pypi|nuget|helm|rubygems|r|apt --file pkg.ext
nexus-cli upload --repo <repo> --format yum --file pkg.rpm --directory /x86_64
nexus-cli upload --repo <repo> --format go --file mod.zip --version v1.2.3
```

先 `--dry-run` 看将发送的 curl，再实际执行。

## 批量目录上传

要上传整个目录/一批构件时，别写循环逐个调 `upload` —— 用 `upload-dir`（内部并行）：

```bash
# raw：保留目录结构（相对路径 → raw.directory）
nexus-cli upload-dir --repo <raw-repo> --dir ./dist --prefix /release/1.0

# maven：按 Maven 仓库布局推断坐标（<group 路径>/<artifactId>/<version>/<文件>）
nexus-cli upload-dir --repo <maven-repo> --format maven --dir ./m2
```

- 先 `--dry-run` 确认坐标推断和目录映射，再真跑（dry-run 不需要凭据）。
- `--include` / `--exclude` 按 basename 匹配；`--jobs N` 控制并发（默认 4）。
- maven 模式默认跳过 `maven-metadata*.xml`（Nexus 自己维护的）。
- 失败不中断，最后打印失败清单；有任何失败则退出码 1。**必读失败清单**并报告给用户。
- 同一坐标重复上传会覆盖，没有"跳过已存在"能力。

## 凭据

凭据来自环境变量或 `~/.config/nexus-cli/env`（环境变量优先）。
不确定当前用的是什么时先 `nexus-cli whoami`；想确认能否连通用 `nexus-cli whoami --check`。
**不要读取或打印凭据文件的内容。**

## 安全约定

- 一律使用**可吊销的 user token**（`NEXUS_USER`/`NEXUS_PASSWORD` 指向它），不要用 admin 密码。
- **破坏性操作先与用户确认**再执行：`repo delete`、`component`/`asset delete`、
  `blobstore delete`、`security user-delete`/`role-delete`、`script delete`、
  `repo invalidate-cache`、`configuration set`。这些在 MCP 通道已被屏蔽，
  但在 CLI 仍可调用。
- 不要在仓库里写入任何 token。

## 分页

Nexus 用 `continuationToken`（不是 Link header），Restish 不会自动翻页。
需要全量时自己循环：

```bash
tok=""
while :; do
  q=(); [ -n "$tok" ] && q=(--continuationToken "$tok")
  page=$(nexus-cli component list --repository <repo> -o json "${q[@]}")
  echo "$page" | jq -c '.items[]'
  tok=$(echo "$page" | jq -r '.continuationToken // empty')
  [ -n "$tok" ] || break
done
```

## 失败处理与退出码

Restish 的退出码（实测）：

| 退出码 | 含义 |
|---|---|
| `0` | 成功 |
| `1` | 命令执行错误（如缺必填参数） |
| `2` | 用法错误（未知命令 / 未知 flag） |
| `4` | **HTTP 错误状态**（4xx/5xx） |
| `124` | `nexus-cli task wait` 超时（本仓库 wrapper 定义） |

两个容易踩的点：

- **HTTP 错误时响应体在 stdout，退出码是 4**，不是 1。判断成功请用退出码，别 grep 文本。
- **Nexus 对失败的认证会限流**：反复用错误凭据会拿到 `429 Too Many Requests
  ("Too many authentication attempts")`。所以：凭据要一次配对，别重试式地狂试；
  遇到 429 立刻停下来报告，等待后再试。
- Restish 默认会按 `Retry-After` 重试（实测可停 1 分钟以上）。需要快速失败时加
  `--rsh-retry 0`。配置里已把 `retry_max_wait` 压到 3 秒。
- 401/403：凭据或权限问题，不要重试，直接报告给用户。
- 命令不存在（exit 2）：用 `restish nexus-cli --help` 重新发现，不要臆造命令名。

## 常用环境变量

`RSH_TIMEOUT=30s`、`RSH_RETRY=0`、`RSH_OUTPUT_FORMAT=json`、`RSH_NO_CACHE=1`、
`RSH_INSECURE=1`（自签证书）、`NO_COLOR=1`。

## MCP 通道

若你通过 MCP 接入，工具面是只读的（写操作被隐藏）。
需要写操作时必须显式使用 CLI 并遵守上面的确认约定。
MCP 由 `nexus-cli mcp` 启动（它比 `restish mcp serve nexus-cli` 多一步加载凭据文件）。

# Nexus Repository Manager CLI

你可以用 `nexus-cli` 操作 Nexus Repository Manager。命令面由内嵌的 OpenAPI spec 生成，
覆盖全部 409 个 REST 操作。它是单文件二进制，运行不需要任何其他依赖。

## 先发现，别猜

```bash
nexus-cli --help                 # 分组一览
nexus-cli <group> --help         # 某分组的命令
```

分组：`asset blobstore capability component config datastore email format lifecycle`
`repo routing script search security status system-nodes task`

## 输出：始终加 -o json

```bash
nexus-cli repo list -o json
nexus-cli repo get <name> -o json
nexus-cli component list --repository <repo> -o json
nexus-cli search query --maven-groupId com.example -o json
nexus-cli task list -o json
```

大响应先用 `-f` 投影，避免把整个 JSON 灌进上下文：

```bash
nexus-cli repo list -o json -f '{name,format,type,url}'
```

其它格式：`-o yaml|table|csv|ndjson|lines`。

## 上传构件

**不要用 `nexus-cli component upload-unsupported`** —— 那条命令用 Restish 的请求体
shorthand，会把 Nexus 要求的点号字段名 `maven2.asset1` 改写成 `maven2[asset1]` 并丢弃
二进制部分。改用：

```bash
nexus-cli upload --repo <repo> --format maven --file app.jar \
    --group com.example --artifact app --version 1.0.0
nexus-cli upload --repo <repo> --format raw --file notes.txt --directory /docs
nexus-cli upload --repo <repo> --format npm|pypi|nuget|helm|rubygems|r|apt --file pkg.ext
```

先 `--dry-run` 看将发送的请求，再实际执行。

## 批量上传

要传一个目录或一批构件时用 `upload-dir`（内部并发），不要自己写循环：

```bash
nexus-cli upload-dir --repo <raw-repo> --dir ./dist --prefix /release/1.0
nexus-cli upload-dir --repo <maven-repo> --format maven --dir ./m2
```

- 先 `--dry-run`（不需要凭据）确认坐标推断
- `--include` / `--exclude` 按 basename 匹配；`--jobs N` 控制并发
- 失败不中断，最后有失败清单 —— **必读并报告**

## 凭据

环境变量 `NEXUS_USER` / `NEXUS_PASSWORD` 优先，其次 `~/.config/nexus-cli/env`。
用 `nexus-cli whoami` 查来源，`--check` 顺带验证连通性。
**不要读取或打印凭据文件的内容。**

## 异步任务

```bash
nexus-cli task wait <taskId>     # 轮询到终态；0=成功 1=失败 124=超时
```

## 安全约定

- 一律使用**专用只读账号**，不要用 admin 账号。Nexus 的 user token 是 Pro 专属功能。
- **破坏性操作先与用户确认**再执行：`* delete`、`invalidate-cache`、
  `security` 变更、`configuration set`、`script run`。这些在 MCP 默认档位已屏蔽，
  但 CLI 通道仍可调用。
- 不要在仓库里写入任何凭据。

## 分页

Nexus 用 `continuationToken`（不是 Link header），不会自动翻页。需全量时自己循环：

```bash
tok=""
while :; do
  args=(--repository <repo>)
  [ -n "$tok" ] && args+=(--continuationToken "$tok")
  page=$(nexus-cli component list "${args[@]}" -o json)
  echo "$page" | jq -c '.items[]'
  tok=$(echo "$page" | jq -r '.continuationToken // empty')
  [ -n "$tok" ] || break
done
```

## 失败处理与退出码

| 退出码 | 含义 |
|---|---|
| `0` | 成功 |
| `1` | 命令失败（含 `upload-dir` 有目标失败） |
| `2` | 用法错误 |
| `4` | **HTTP 错误状态**（Restish 生成的命令） |
| `124` | `task wait` 超时 |

- 判断成败用退出码，别 grep 文本。
- **Nexus 会限流失败的认证**：反复用错凭据会返回 `429 Too many authentication attempts`。
  遇到 429 立刻停下，等待后再试。
- 命令不存在（exit 2）：用 `nexus-cli --help` 重新发现，不要臆造命令名。

## MCP 通道

MCP 工具名是 `<分组>_<动词>`（如 `repo_list`、`component_upload`）。
默认档位只读 + 生产性写；`delete` / security 变更 / `script run` 需要
`--allow-destructive` 才会出现。需要执行破坏性操作时，改用 CLI 并遵守上面的确认约定。

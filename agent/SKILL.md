---
name: nexus-cli
description: Operate a Sonatype Nexus Repository Manager (3.x) instance via the `nexus-cli` CLI — list/create repos, search and upload components, manage blobstores, tasks, and security. Use when the user asks about Nexus repositories, artifacts, components, uploads, or Nexus administration.
---

# Nexus Repository Manager CLI

用 `nexus-cli`（包装了 `restish nexus-cli`）操作 Nexus。命令面由 Nexus OpenAPI spec 生成，
覆盖全部 409 个 REST 操作。

## 先发现，别猜

```bash
nexus-cli groups                          # 命令分组
restish nexus-cli <group> --help          # 某分组的命令
```

分组：`asset blobstore capability component config datastore email format lifecycle repo routing script search security status system-nodes task`

## 读操作

```bash
nexus-cli status show -o json
nexus-cli repo list -o json
nexus-cli repo get <name> -o json
nexus-cli component list --repository <repo> -o json
nexus-cli asset list --repository <repo> -o json
nexus-cli search query --maven-groupId com.example -o json
nexus-cli blobstore list -o json
nexus-cli task list -o json
```

**始终加 `-o json`**，并用 `-f` 投影压缩上下文：

```bash
nexus-cli repo list -o json -f '{name,format,type}'
```

## 上传（唯一必须走 wrapper 的操作）

`restish nexus-cli component upload-unsupported` 是**坏的**：Restish shorthand 会把
Nexus 要求的点号字段名 `maven2.asset1` 改写成 `maven2[asset1]`，并丢弃二进制部分。
必须用：

```bash
nexus-cli upload --repo <repo> --format maven --file app.jar \
    --group com.example --artifact app --version 1.0.0
nexus-cli upload --repo <repo> --format raw --file notes.txt --directory /docs
nexus-cli upload --repo <repo> --format npm|pypi|nuget|helm|rubygems|r|apt --file pkg.ext
nexus-cli upload --repo <repo> --format yum --file pkg.rpm --directory /x86_64
nexus-cli upload --repo <repo> --format go --file mod.zip --version v1.2.3
```

不确定参数时先 `--dry-run`（只打印 curl，不发送）。

## 批量上传

要传一个目录或一批构件时用 `upload-dir`（内部并行），不要自己写循环：

```bash
# raw：保留目录结构
nexus-cli upload-dir --repo <raw-repo> --dir ./dist --prefix /release/1.0

# maven：按 <group 路径>/<artifactId>/<version>/<文件> 布局推断坐标
nexus-cli upload-dir --repo <maven-repo> --format maven --dir ./m2
```

先 `--dry-run`（不需要凭据）确认坐标推断；`--include`/`--exclude` 按 basename 匹配；
`--jobs N` 控制并发。失败不中断，最后有失败清单——**必读并报告**。

## 凭据

来自环境变量或 `~/.config/nexus-cli/env`（环境变量优先）。
用 `nexus-cli whoami` 查当前来源，`--check` 顺带验证连通性。
**不要读取或打印凭据文件内容。**

## 异步任务

多数写操作是异步的：

```bash
nexus-cli task wait <taskId>     # 轮询到终态；0=成功 1=失败 124=超时
```

## 失败处理

退出码：`0` 成功 / `1` 执行错误 / `2` 用法错误 / `4` **HTTP 错误状态** / `124` task wait 超时。
HTTP 错误时响应体在 stdout 而退出码是 4——**用退出码判断成败，不要 grep 文本**。

⚠️ **Nexus 会限流失败的认证**：反复用错凭据会返回 `429 Too Many Requests
("Too many authentication attempts")`。遇到 429 立即停止并报告，不要重试。
需要快速失败时用 `--rsh-retry 0`（默认会按 `Retry-After` 等 1 分钟以上）。

## 安全

- 只用**专用只读账号**（`NEXUS_USER`/`NEXUS_PASSWORD`），不用 admin 账号。
  Nexus 的 user token 是 Pro 专属功能，Community 版只能用账号密码。
- 破坏性操作（`* delete`、`invalidate-cache`、`configuration set`、`user-delete`、
  `role-delete`）**执行前必须与用户确认**。MCP 通道已屏蔽这些，CLI 通道没有。
- 不要把凭据写进仓库或日志。

## 分页

Nexus 用 `continuationToken`，不会自动翻页。需全量时用 `--continuationToken` 手动循环。

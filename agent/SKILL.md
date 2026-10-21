---
name: nexus-cli
description: Operate a Sonatype Nexus Repository Manager (3.x) instance via the single-binary `nexus-cli` — list/create repos, search and upload components, manage blobstores, tasks, and security. Use when the user asks about Nexus repositories, artifacts, components, uploads, or Nexus administration.
---

# Nexus Repository Manager CLI

`nexus-cli` 是单文件二进制，命令面由内嵌 OpenAPI spec 生成，覆盖全部 409 个 REST 操作。

## 先发现，别猜

```bash
nexus-cli --help
nexus-cli <group> --help
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
nexus-cli task list -o json
```

**始终加 `-o json`**，并用 `-f` 投影压缩上下文：

```bash
nexus-cli repo list -o json -f '{name,format,type}'
```

## 上传

`component upload-unsupported` 是**坏的**（Restish shorthand 会破坏点号字段名）。必须用：

```bash
nexus-cli upload --repo <repo> --format maven --file app.jar \
    --group com.example --artifact app --version 1.0.0
nexus-cli upload --repo <repo> --format raw --file notes.txt --directory /docs
nexus-cli upload --repo <repo> --format npm|pypi|nuget|helm|rubygems|r|apt --file pkg.ext
```

不确定参数时先 `--dry-run`（不需要凭据）。

## 批量上传

```bash
nexus-cli upload-dir --repo <raw-repo> --dir ./dist --prefix /release/1.0
nexus-cli upload-dir --repo <maven-repo> --format maven --dir ./m2
```

先 `--dry-run` 确认坐标推断；`--include`/`--exclude` 按 basename 匹配；`--jobs N` 并发。
失败不中断，最后有失败清单——**必读并报告**。

## context 与凭据

目标地址 + 凭据按「context」保存（`~/.config/nexus-cli/config.json`）。
`--context <名字>` 可单次指定，`nexus-cli context list/use/show` 管理。
环境变量 `NEXUS_USER`/`NEXUS_PASSWORD` 优先于 context 里的凭据；
显式给了地址且与某 context 相同时会自动匹配。

用 `nexus-cli whoami` 查当前来源，`--check` 顺带验证连通性。
**不要读取或打印凭据文件内容。**

## 异步任务

```bash
nexus-cli task wait <taskId>     # 0=成功 1=失败 124=超时
```

## 失败处理

退出码：`0` 成功 / `1` 失败 / `2` 用法错误 / `4` HTTP 错误状态 / `124` 超时。
用退出码判断成败，不要 grep 文本。

⚠️ Nexus 会限流失败的认证：反复用错凭据会返回 `429 Too Many Requests`。
遇到 429 立即停止并报告，不要重试。

## 安全

- 只用**专用只读账号**，不用 admin 账号。Nexus user token 是 Pro 专属功能。
- 破坏性操作（`* delete`、`invalidate-cache`、`security` 变更、`configuration set`、
  `script run`）**执行前必须与用户确认**。
- 不要把凭据写进仓库或日志。

## 分页

Nexus 用 `continuationToken`，不会自动翻页。需全量时用 `--continuationToken` 手动循环。

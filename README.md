# nexus-cli

Nexus Repository Manager 的命令行工具与 MCP server。

**一个静态二进制，零运行期依赖。** 不需要 restish、jq、curl、python 或 shell ——
Restish 与 Nexus 的 OpenAPI spec 都已编进二进制。

- 命令面由 OpenAPI spec 生成，覆盖 **409 个 REST 操作**
- 另有三类自有能力：构件上传（multipart）、多目标 context、MCP server
- 跨平台：macOS / Linux / Windows，amd64 与 arm64
- 中英双语，按 `LANG` 自动切换

```
nexus-cli repo list -o json
nexus-cli upload --repo maven-releases --file app.jar --group com.example --artifact app --version 1.0.0
nexus-cli mcp
```

## 安装

```bash
# 推荐：go install（模块路径即仓库路径）
go install github.com/wendaoji/nexus-cli/cmd/nexus-cli@latest

# 或从 release 下载单文件后放进 PATH
#   https://github.com/wendaoji/nexus-cli/releases

# 或从源码构建
git clone git@github.com:wendaoji/nexus-cli.git && cd nexus-cli && make build
```

首次运行会把内嵌 spec 解到缓存目录（`~/Library/Caches/nexus-cli` 或
`$XDG_CACHE_HOME/nexus-cli`），不写任何全局配置文件。

## 快速开始

```bash
# 保存目标地址 + 凭据（写入前会校验一次）
nexus-cli login --user <账号>
nexus-cli whoami --check

# 查
nexus-cli status show -o json
nexus-cli repo list -o json
nexus-cli component list --repository <repo> -o json
nexus-cli search query --maven-groupId com.example -o json

# 发
nexus-cli upload --repo <repo> --format maven --file app.jar \
    --group com.example --artifact app --version 1.0.0
nexus-cli upload-dir --repo <repo> --format maven --dir ./m2
```

分组一览：`nexus-cli --help`；某分组：`nexus-cli repo --help`。
输出支持 `-o json|yaml|table|csv|ndjson|lines`，并用 `-f` 投影：

```bash
nexus-cli repo list -o json -f '{name,format,type}'
```

## 多目标（context）

目标地址与凭据按 context 保存在 `$XDG_CONFIG_HOME/nexus-cli/config.json`（0600）。

```bash
nexus-cli login --url http://dev:8081 --user ci --context dev
nexus-cli context list | use prod | show | remove
nexus-cli --context dev repo list          # 单次指定
NEXUS_URL=http://dev:8081 nexus-cli repo list   # 按地址自动匹配凭据
```

优先级：context 名 `--context` > `NEXUS_CONTEXT` > `current`；
地址 `--url` > `NEXUS_URL` > context.url > 编译期默认（`config/default-origin.txt`）；
凭据 `NEXUS_USER`/`NEXUS_PASSWORD` > context。

若本次目标地址与 context 记录的地址不一致，`whoami` 会报错退出——避免拿 A 的凭据打 B。

## 上传

Nexus 的 components 接口要求 multipart 使用**字面量点号字段名**（`maven2.asset1`），
Restish 的请求体 shorthand 会把点号当嵌套结构，因此上传由本工具自己实现。

```bash
nexus-cli upload --repo <repo> --format maven --file app.jar \
    --group com.example --artifact app --version 1.0.0
nexus-cli upload --repo <repo> --format raw --file notes.txt --directory /docs
nexus-cli upload --repo <repo> --format npm|pypi|nuget|helm|rubygems|r|apt --file pkg.ext
nexus-cli upload ... --dry-run        # 只打印请求，不需要凭据
```

批量上传：

```bash
nexus-cli upload-dir --repo <raw-repo> --dir ./dist --prefix /release/1.0
nexus-cli upload-dir --repo <maven-repo> --format maven --dir ./m2
```

- `--include` / `--exclude` 按 basename 匹配，`--jobs N` 控制并发（默认 4）
- maven 模式按 `<group 路径>/<artifactId>/<version>/<文件>` 推断坐标，默认跳过 `maven-metadata*.xml`
- 失败不中断，结束时打印失败清单；有任何失败则退出码 1
- 先 `--dry-run` 确认坐标推断

## MCP

```bash
nexus-cli mcp                      # 默认：读 + 生产性写
nexus-cli mcp --read-only          # 仅 GET/HEAD
nexus-cli mcp --allow-destructive  # 额外放开 delete / security 变更 / script run
```

| 档位 | 工具数 | 内容 |
|---|---|---|
| `--read-only` | 129 | 仅读 |
| 默认 | 297 | 读 + 构件上传 + 任务触发/停止 |
| `--allow-destructive` | 338 | + 17 个 delete、security/configuration/email 变更、`script run` |

客户端配置见 `mcp/mcp.json.example`，agent 约定见 `agent/AGENTS.md` 与 `agent/SKILL.md`。
工具名为 `<分组>_<动词>`（如 `repo_list`），与 CLI 的 `repo list` 对应。

## 语言

默认英文；`LANG`/`LC_ALL`/`LC_MESSAGES` 以 `zh` 开头时切中文：

```bash
LANG=zh_CN.UTF-8 nexus-cli repo list
nexus-cli --lang en repo list        # 单次覆盖（优先级最高）
NEXUS_CLI_LANG=zh nexus-cli whoami
```

## 支持的 Nexus 版本

命令面由**内嵌的 spec** 决定，与目标服务器版本无关。`nexus-cli version` 会显示二者：

```
nexus-cli 0.3.0
spec      3.97.0-13 (nexus-3.97.0-13.json)
go        go1.25.6 darwin/arm64
lang      en
```

| 服务器 | 状态 |
|---|---|
| 3.97.x（Community） | 已实测，内嵌 spec 即来自该版本 |
| 更低版本 | 未验证；较新的接口会 404，其余可用 |
| 更高版本 | 未验证；已有接口可用，新增接口要刷新 spec 后才有 |

Pro 专属接口（user token、LDAP、licensing、Firewall/IQ、证书、content selectors、
malicious-risk 等）在 Community 版不可用，已从命令面隐藏。

升级 spec 与发布流程见 [`docs/DESIGN.md`](docs/DESIGN.md)；变更历史见
[`CHANGELOG.md`](CHANGELOG.md)。

## 开发

```bash
make build      # dist/nexus-cli（-trimpath + 版本注入）
make test       # go test ./...
make vet
make verify     # 构建 + 60 项冒烟验证
```

维护脚本（**运行 CLI 不需要**这些依赖：仅需 go、jq、python3+PyYAML）：

| 脚本 | 用途 |
|---|---|
| `scripts/gen-tags.sh` | 由 vendor spec 生成机械层 overlay（分组名 + DELETE 屏蔽） |
| `scripts/sync-spec.sh` | 拉取新 spec → 重生成机械层 → 校验 overlay → 清理旧 spec |
| `scripts/validate-overlay.py` | 拦住「deep-merge 凭空造出的 method」与命名冲突 |
| `scripts/verify.sh` | 冒烟验证（构建、命令面、上传、context、MCP、i18n） |

推 `v*` tag 时 GitHub Actions 用 goreleaser 产出多平台产物。

## 安全

- 使用**专用只读账号**，不要用 admin 账号
- 配置文件（含凭据）权限 `600`，仓库内不含任何凭据
- MCP 默认屏蔽破坏性操作；`script run` 需显式 `--allow-destructive`
- Nexus 会限流失败的认证（429）：凭据只在校验通过后落盘，避免反复试错

## License

[MIT](LICENSE)

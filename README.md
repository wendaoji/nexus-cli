# nexus-cli

Nexus Repository Manager 的 CLI，**由 OpenAPI spec 生成命令面**，覆盖目标实例
（Nexus 3.97.0-13 Community @ `http://172.16.41.26:8081`）的**全部 409 个 REST 操作**，
同时可作为 **MCP server** 提供给 AI agent。

对上提供两个入口，共用一个认证与配置源：

```
                       ┌────────────────────────────┐
  bin/nexus-cli ──────►│                            │
  bin/nexus-cli mcp ──►│  Restish（API client）     │──── HTTP ──► Nexus
                       │  + 本仓库的 spec 三层叠加   │
                       └────────────────────────────┘
```

- **`bin/nexus-cli`** —— 人 & agent 用的命令入口。通用命令透传给 Restish；
  只有几件 Restish 做不好的事由 wrapper 处理（见「设计决策」）。
- **`nexus-cli mcp`** —— 同一份配置暴露为 MCP 工具，默认只读。
  走 wrapper 而不是直接 `restish mcp serve`，是为了让它也读到凭据文件。

---

## 快速开始

```bash
# 0. 依赖: restish / jq / curl / python3(+PyYAML)
brew install restish jq

# 1. 安装 MCP 插件（需要 MCP 通道时才装）
restish plugin install rest-sh/restish mcp --yes

# 2. 生成机械层 + 注册到 Restish 全局配置
scripts/install.sh

# 3. 写入凭据（写入前会先校验一次；会存到 ~/.config/nexus-cli/env，chmod 600）
nexus-cli login --user <只读账号>            # 交互式输入密码（不回显）
#   nexus-cli login --user <账号> --password-stdin   # 脚本/CI
#   export NEXUS_USER=... NEXUS_PASSWORD=...        # 或直接用环境变量（优先于文件）

# 4. 验证
scripts/verify.sh
nexus-cli whoami --check

# 5. 用
nexus-cli status show -o json
nexus-cli repo list -o json
```

> `scripts/install.sh` 把 API 定义写进 **Restish 全局配置**（`~/.config/restish/restish.json`，
> 绝对路径）。这是刻意的，原因见「设计决策 3」。

---

## 凭据放在哪

**配置文件：`~/.config/nexus-cli/env`**（`chmod 600`，由 `nexus-cli login` 写入）：

```
NEXUS_USER=<账号>
NEXUS_PASSWORD=<密码或 user token passcode>
```

`nexus-cli login` 会**先校验一次凭据**再落盘，避免存下错的凭据后到处 401
（还会触发 Nexus 的认证限流）：

```bash
nexus-cli login --user <账号>              # 交互式输入，不回显
nexus-cli login --user <账号> --password-stdin   # 脚本/CI
```

校验用的是 `GET /v1/repositories`，据此区分三种情况：
`200` 有效 / `403` 已认证但无权限（凭据本身有效）/ `401` 凭据无效 / `429` 被限流。

> **关于 User Token：它是 Nexus Pro 专属功能。** Community 版没有该接口
> （即便是 Pro，路径也是 `/v1/security/users/{userId}/{realm}/user-token`，
> 需要配合 realm，且不是所有版本都开放）。所以本工具**不提供**"用 admin 自动
> 铸造 token"的能力 —— 那个接口在 Community 上必然 404。
> 若你在 Pro 版或 Web UI 里拿到了 user token，**直接把它当密码传给 `--password`** 即可：
> Nexus 的 token 用法就是用「token name + passcode」替换「用户名 + 密码」做 Basic 认证。

优先级：**环境变量 > 凭据文件**。文件只「填补」环境里没设置的变量，
所以 CI/agent 用环境变量、日常用文件，互不打架。查当前生效来源：

```bash
nexus-cli whoami            # 显示来源（环境变量/文件）、用户、目标地址
nexus-cli whoami --check    # 再打一次 /v1/status 确认凭据可用
```

为什么用一个 0600 的文件而不是系统钥匙串：Restish 的 profile 只支持
`env:NAME` / `command:...` 两种密钥来源，没有钥匙串集成。所以约定是
**Restish 配置里只写 `env:NEXUS_USER` 引用，真实值落在 0600 文件或环境变量里** ——
配置本身可以安全地进仓库和分享。

同一台机器要管多个 Nexus 实例时，目前的做法是给不同的 checkout 用不同的
`NEXUS_USER`/`NEXUS_PASSWORD`（或不同的 `XDG_CONFIG_HOME`）；多 context 支持还没做。

---

## 命令面

17 个分组，命令名形如 `nexus-cli <分组> <动词>`：

```bash
nexus-cli groups                                   # 列出分组
restish nexus-cli repo --help                      # 看某分组

nexus-cli status show      -o json                 # 服务状态/版本
nexus-cli repo list        -o json
nexus-cli repo get         <name>   -o json
nexus-cli repo delete      <name>                  # 破坏性操作
nexus-cli component list   --repository <repo> -o json
nexus-cli asset list       --repository <repo> -o json
nexus-cli search query     --maven-groupId com.example -o json
nexus-cli search versions  --repository <repo> -o json
nexus-cli blobstore list   -o json
nexus-cli task list        -o json
nexus-cli security user-list -o json
nexus-cli script list      -o json
```

分组：`asset blobstore capability component config datastore email format lifecycle`
`repo routing script search security status system-nodes task`

输出 `-o json|yaml|table|csv|ndjson|lines|gron`，可用 `-f` 做投影：

```bash
nexus-cli repo list -o json -f '{name,format,type,url}'
```

长尾（约 40 个 `create-repositories-<format>-<kind>`、routing rules、privileges、
realms、capabilities…）保持 spec 自动命名，用 `--help` 发现即可。

---

## 上传构件

**必须用 `nexus-cli upload`**（原因见「设计决策 2」）：

```bash
nexus-cli upload --repo <repo> --format maven --file app.jar \
    --group com.example --artifact app --version 1.0.0 [--packaging jar] [--classifier sources]

nexus-cli upload --repo <repo> --format raw --file notes.txt --directory /docs [--filename notes.txt]
nexus-cli upload --repo <repo> --format npm|pypi|nuget|helm|rubygems|r|apt --file pkg.ext
nexus-cli upload --repo <repo> --format yum --file pkg.rpm --directory /x86_64
nexus-cli upload --repo <repo> --format go  --file mod.zip --version v1.2.3
nexus-cli upload --repo <repo> --format conda --file pkg.tar.bz2 --filename pkg.tar.bz2
nexus-cli upload --repo <repo> --format composer --file pkg.zip --version 1.0.0

# 任意字段兜底（其它格式）
nexus-cli upload --repo <repo> --format <fmt> --file f --field 'k=v' --field 'k2=v2'

# 先看将要执行的 curl
nexus-cli upload ... --dry-run
```

支持的 `--format` 与 Nexus REST 的字段名一一对应（`maven2.groupId`、`raw.asset1`、
`yum.asset`、`go.asset` …）。`docker`/`oci`/`huggingface` 不走 REST 上传，wrapper
会明确报错，请用 `docker push` 等原生客户端。

### 批量目录上传 `upload-dir`

两种模式，都是并行发送（Nexus 没有批量接口，工具在客户端并发）：

```bash
# 模式一：raw —— 保留目录结构，相对路径变成 raw.directory
nexus-cli upload-dir --repo <raw-repo> --dir ./dist --prefix /release/1.0
#   ./dist/top.txt        -> raw.directory=/release/1.0          filename=top.txt
#   ./dist/docs/a.txt     -> raw.directory=/release/1.0/docs     filename=a.txt

# 模式二：maven —— 按 Maven 仓库布局推断坐标
nexus-cli upload-dir --repo <maven-repo> --format maven --dir ./m2
#   ./m2/com/example/app/1.0.0/app-1.0.0.jar
#     -> groupId=com.example  artifactId=app  version=1.0.0
#   同目录下的文件按「主件 → pom → 带 classifier」排序，一次请求最多 3 个 asset：
#     asset1=app-1.0.0.jar  asset2=app-1.0.0.pom  asset3=app-1.0.0-sources.jar(classifier=sources)
#   超过 3 个会自动切分成多次请求。

# 公共选项
#   --include '*.jar'     只传匹配的文件（按 basename 匹配）
#   --exclude '*-sources.jar'
#   --jobs 4              并发数（默认 4）
#   --dry-run             只打印计划的请求，不发送
```

行为细节：

- **maven 模式默认跳过 `maven-metadata*.xml`**（那是 Nexus 自己维护的）。要传就显式
  `--include 'maven-metadata*.xml'`。
- 始终跳过 `.DS_Store` / `.gitignore` / `.git` / `.svn`。
- 失败不中断：跑完所有目标后打印失败清单，并以退出码 1 结束；全成功则 0。
- `--dry-run` 不需要凭据，可以先用它确认坐标推断是否符合预期。

> 不支持增量/跳过已存在（Nexus 同一坐标重复上传会覆盖，行为按仓库策略而定）。
> 需要精确控制时用 `upload` 逐文件传。

---

## MCP（给 AI agent）

```bash
nexus-cli mcp                               # 默认只读：写操作全部隐藏
nexus-cli mcp --allow-write-tools --operations createComponents,createTasksRun
```

> 用 `nexus-cli mcp` 而不是直接 `restish mcp serve nexus-cli`：前者会先加载凭据文件，
> 后者只认环境变量。客户端里若已经显式注入了 `NEXUS_USER`/`NEXUS_PASSWORD`，两者等价。

客户端配置见 `mcp/mcp.json.example`。agent 行为约定见 `agent/AGENTS.md`
（可直接追加到 `~/.claude/CLAUDE.md` 或项目 `AGENTS.md`），
或作为 skill 使用 `agent/SKILL.md`。

实测：默认模式下会隐藏 180 个写操作。

---

## 设计决策（以及踩过的坑）

### 1. 为什么用 Restish 作为基座

它同时具备：OpenAPI → CLI 命令生成、profile/auth（`env:` 引用）、结构化输出与 `-f` 过滤、
以及 **MCP 插件**。候选对比：

| 方案 | 结论 |
|---|---|
| mcp2cli / openapi-cli4ai | 运行时生成 CLI，但**不支持 multipart 文件上传**，Nexus 最核心的发布构件做不了 |
| 手写 409 个命令 | 浪费 |
| oapi-codegen + Cobra 自建 | 控制力最强，但要写真实代码；作为回退路线保留 |
| **Restish** | 唯一同时满足上述所有点 |

### 2. 为什么上传绕过 Restish（用 curl）

Restish 的请求体 shorthand 里，`.` **永远是嵌套语义**（`user.name: x` → `{"user":{"name":"x"}}`），
没有转义方式。实测对 Nexus 上传接口的效果：

```
输入:  maven2.asset1: @app.jar
Restish 实际发送:  name="maven2[asset1]"   ← 字段名被改写
                   且二进制部分被丢弃       ← 文件直接没了
Nexus 要求:        name="maven2.asset1"  + filename="app.jar"
```

所以 `nexus-cli upload` 用 `curl -F` 发送（`curl` 会原样保留点号字段名），
而**认证头仍由 Restish 提供**（`restish api auth get nexus-cli`），
凭据保持单一来源，不产生第二套密钥存储。CLI 里保留了
`component upload-unsupported` 作为显式警告入口。

### 3. 为什么装在 Restish 全局配置里

两个实测结论逼出这个选择：

- **相对 `spec_files` 按「当前工作目录」解析**。放项目级 `.restish.json` 就只有
  `cwd=仓库根` 时能用；在子目录里 Restish 会向上找到配置、但相对路径按子目录解析 →
  `open spec/<spec>.json: no such file or directory`。
- **一旦显式指定配置（`RSH_CONFIG` / `--rsh-config`），Restish 会禁用插件加载**，
  而 MCP 本身是插件 → MCP 失效。

因此：仓库里的 `config/restish.api.json` 是**模板**，`scripts/install.sh` 把其中的
相对路径绝对化后写入全局配置。从此任意目录可用，插件（含 MCP）正常加载。

### 4. 为什么 spec 分三层

```
spec/nexus-<version>.json       vendor 快照，原样，可重新拉取（版本号随实例变化）
spec/nexus-tags.generated.yaml  机械层，scripts/gen-tags.sh 生成（勿手改）
spec/nexus-overlay.yaml         人工语义层，手工维护
```

Restish 按 `spec_files` 顺序 deep-merge（对象递归、标量覆盖、数组覆盖），所以定制
不必改 vendor 文件，git diff 也干净。分层依据是「是否机械可推导」：

- **机械层**（生成）：`/v1/repositories*` 等 221 个 path → 短分组名；
  以及**所有 DELETE** → `x-mcp-ignore`。手写 300 行 YAML 既慢又易错。
- **人工层**（手写）：注入 `securitySchemes`、90 个 `x-cli-name` 命名、
  52 个 Pro/内部接口的 `x-cli-ignore`、危险写操作的额外 `x-mcp-ignore`。

⚠️ **deep-merge 会凭空造出 method**：曾经因为给 `/v1/repositories/{repositoryName}`
写了 `put:`（vendor 里只有 `get`/`delete`）而生成一个缺参数的假接口。
`scripts/validate-overlay.py` 专门拦这类问题（以及分组内命名冲突）。

### 5. 认证为什么需要 overlay 注入

Nexus 的 spec **完全没有 `securitySchemes` / `security`**。Restish 默认只对 spec 声明的
security 发凭据，因此人工层注入：

```yaml
components:
  securitySchemes:
    basicAuth: {type: http, scheme: basic}
security:
  - basicAuth: []
```

凭据本身放在 profile 里，且只用 `env:` 引用，不落仓库：

```json
"auth": {"type": "http-basic", "params": {
  "username": "env:NEXUS_USER", "password": "env:NEXUS_PASSWORD"}}
```

---

## 维护

### 升级 spec（Nexus 版本变化）

```bash
scripts/sync-spec.sh          # 拉取 → 改名/更新模板 → 重生成 → 校验 → 重装 → 同步
```

它会打印新增/删除的 operation。**删除项若出现在人工层 overlay 里，校验会报错**，
按提示清理即可。改完重跑 `scripts/verify.sh`。

### 改了人工层 overlay

```bash
scripts/validate-overlay.py   # 先校验：无凭空 method / 命名冲突
scripts/install.sh            # 再装（会 restish api sync）
```

### 日常排障

```bash
restish doctor api nexus-cli       # 看 x-cli-* 扩展的影响
restish -v nexus-cli repo list     # 看实际请求/响应头
nexus-cli --help-all               # 全部 Restish 选项
```

---

## 退出码与自动化

Restish 的退出码（实测）：

| 退出码 | 含义 |
|---|---|
| `0` | 成功 |
| `1` | 命令执行错误 / `upload`·`upload-dir` 有目标失败 |
| `2` | 用法错误（未知命令 / 未知 flag） |
| `4` | **HTTP 错误状态**（4xx/5xx） |
| `124` | `nexus-cli task wait` 超时（wrapper 定义） |

三个坑：

- **HTTP 错误时响应体写在 stdout，退出码是 4**（不是 1）。脚本/agent 请用退出码判断成败。
- `upload` / `upload-dir` 自己判断 HTTP 状态码：`200/201/204` 视为成功，否则打印响应体并非零退出。
  `upload-dir` 是"跑完所有目标再汇总"，只要有失败就以 1 结束 —— 所以**务必看失败清单**。
- 判断 `upload-dir` 是否成功：退出码，而不是 grep 输出（并发输出顺序不确定）。

常用环境变量：`RSH_TIMEOUT=30s`、`RSH_RETRY=0`、`RSH_OUTPUT_FORMAT=json`、
`RSH_INSECURE=1`（自签证书）、`RSH_NO_CACHE=1`、`NO_COLOR=1`。

---

## 已知限制

- **不会自动翻页**。Nexus 用 `continuationToken`（不是 Link header），Restish 的自动
  分页对它无效。需全量时用 `--continuationToken` 手动循环（示例见 `agent/AGENTS.md`）。
- **Nexus 会限流失败的认证**。反复用错误凭据会收到
  `429 Too Many Requests — "Too many authentication attempts"`。这也是默认重试被压到
  3 秒的原因：避免在错误凭据下长时间静默等待。遇到 429 请停下来，别继续试。
- **Community 版看不到 Pro 接口**。LDAP、licensing、Firewall/IQ、证书、malicious-risk、
  evaluation、recovery-mode、support zip 等已在人工层 `x-cli-ignore` 隐藏，
  避免调用后拿到 403。若你的实例是 Pro，删掉对应 `x-cli-ignore` 再 `scripts/install.sh`。
- **`repo update` 不存在**。Nexus 没有通用仓库更新接口，更新是逐格式的
  （`nexus-cli repo update-repositories-maven-hosted` 类长尾命令）。
- **spec 有 3 处 operationId 重复**（`updatePlan`/`deletePlan`/`updateEvaluationSettingsWithRepos`）。
  Restish 自动加 `-put`/`-delete`/`-patch` 后缀消歧，sync 时会打印提示。
- **`bin/nexus-cli` 需要 `bash`**（用到了数组）。macOS 自带 bash 3.2 也可，建议 4+。

---

## 目录结构

```
nexus-cli/
├── bin/nexus-cli                 主入口（upload / upload-dir / task wait / login / whoami / mcp / 透传）
├── config/restish.api.json       API 定义模板（相对 spec 路径）
├── spec/
│   ├── nexus-<version>.json      vendor 快照（如 nexus-3.97.0-13.json）
│   ├── nexus-tags.generated.yaml 机械层（生成）
│   └── nexus-overlay.yaml        人工语义层
├── scripts/
│   ├── gen-tags.sh               生成机械层
│   ├── validate-overlay.py       拦截凭空 method / 命名冲突
│   ├── install.sh                写入 Restish 全局配置
│   ├── sync-spec.sh              升级 spec 一条命令
│   └── verify.sh                 冒烟验证（65 项）
├── agent/
│   ├── AGENTS.md                 agent 行为约定（可追加进 CLAUDE.md）
│   └── SKILL.md                  可安装的 skill
└── mcp/mcp.json.example          MCP 客户端配置
```

机器上的两个位置（不在仓库里）：

| 路径 | 内容 | 权限 |
|---|---|---|
| `~/.config/restish/restish.json` | API 定义（绝对 spec 路径）、profile | `600` |
| `~/.config/nexus-cli/env` | 凭据（由 `nexus-cli login` 写入） | `600` |

## 安全

- CLI/MCP 一律使用**专用只读账号**，不要用 admin 账号。Nexus 的 user token
  是 Pro 专属功能，Community 版只能用账号密码；若你有 token，按密码用即可。
- 凭据只通过 `env:` 引用；`nexus-cli login` 写入的 `~/.config/nexus-cli/env` 为 `chmod 600`。
- MCP 默认只读；`delete*` 及破坏性写操作已 `x-mcp-ignore`。
- 仓库内不含任何凭据（见 `.gitignore`）。

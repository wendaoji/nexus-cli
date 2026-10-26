package i18n

// catalog 把源码里的英文文案映射到中文。缺失的条目会直接显示英文，
// 所以新增文案时忘了翻译也只是显示英文，不会出现键名。
var catalog = map[string]string{
	// 通用/空值
	"<empty>":   "<空>",
	"<none>":    "<无>",
	"<unset>":   "<未设置>",
	"valid":     "有效",
	"version":   "版本",
	"lang":      "语言",
	"spec":      "spec",
	"upload":    "上传",
	"failures:": "失败清单:",

	// 来源标签（whoami / context show）
	"flag":     "命令行参数",
	"env":      "环境变量",
	"context":  "context",
	"current":  "current",
	"auto-url": "按地址自动匹配",
	"default":  "编译期默认值",

	// 凭据校验
	"Credentials valid (HTTP 200)": "凭据有效（HTTP 200）",
	"Credentials valid, but this account cannot list repositories (HTTP 403) - authenticated":        "凭据有效，但该账号无权列出仓库（HTTP 403）—— 认证已通过",
	"Invalid credentials (HTTP 401): check the username and password/token passcode":                 "凭据无效（HTTP 401）：请检查账号与密码/token passcode",
	"Nexus is rate-limiting authentication (HTTP 429, too many failed attempts): wait a few minutes": "Nexus 认证限流（HTTP 429，短时间多次失败尝试所致）：等几分钟再试",
	"Check failed (HTTP %d)":                     "校验失败（HTTP %d）",
	"network error while validating credentials": "校验凭据时网络错误",
	"connectivity check failed":                  "连通性检查未通过",
	"connectivity: %s":                           "连通性: %s",
	"failed: %s":                                 "失败: %s",

	// 登录 / context
	"Nexus Repository Manager CLI - OpenAPI-powered, single-file distribution.": "Nexus Repository Manager CLI —— OpenAPI 驱动，单文件分发。",
	"Password (or user token passcode) for %s: ":                                                  "%s 的密码（或 user token passcode）: ",
	"login: --user is required":                                                                   "login: 需要 --user",
	"login: empty password, nothing written":                                                      "login: 密码为空，未写入",
	"login: non-interactive input, use --password-stdin (or --password)":                          "login: 非交互环境请用 --password-stdin（或 --password）",
	"login: unexpected argument %q (see --help)":                                                  "login: 多余的参数 %q（--help 看用法）",
	"read stdin":                               "读取 stdin",
	"read password":                            "读取密码",
	"wrote %s (mode 0600)":                     "已写入 %s（chmod 600）",
	"context: %s%s":                            "context: %s%s",
	" (current)":                               "（当前）",
	"switch to it: nexus-cli context use %s":   "切换: nexus-cli context use %s",
	"context %s%s":                             "context %s%s",
	"context %q address changed from %s to %s": "context %q 的地址由 %s 更新为 %s",
	"context %q does not exist":                "context %q 不存在",
	"context %q does not exist (create it with `nexus-cli login --context %s`)": "context %q 不存在（先用 `nexus-cli login --context %s` 创建）",
	"context %q does not exist (existing: %s)":                                  "context %q 不存在（现有: %s）",
	"context %q records %s but the target is %s":                                "context %q 记录的是 %s，本次目标是 %s",
	"context: a subcommand is required":                                         "context: 需要子命令",
	"context: unknown subcommand %q (see --help)":                               "context: 未知子命令 %q（--help 看用法）",
	"contexts: %s (current=%s)":                                                 "所有 context: %s（current=%s）",
	"No contexts yet. Run `nexus-cli login --user <account>` to create one.":    "还没有任何 context。运行 `nexus-cli login --user <账号>` 创建。",
	"switched to context %q (%s)":                                               "已切换到 context %q（%s）",
	"removed context %q":                                                        "已删除 context %q",
	"removed context %q (it was current; current is now %s)":                    "已删除 context %q（原为当前，current 现在是 %s）",
	"no current context; run `nexus-cli login` first":                           "没有当前 context；先运行 `nexus-cli login`",
	"usage: nexus-cli context use <name>":                                       "用法: nexus-cli context use <名字>",
	"usage: nexus-cli context remove <name>":                                    "用法: nexus-cli context remove <名字>",
	"target address does not match the context record (see above)":              "目标地址与 context 记录不一致（见上）",
	"account: %s":       "账号: %s",
	"account: %s%s":     "账号: %s%s",
	" (from %s)":        "（来自%s）",
	"address: %s":       "地址: %s",
	"address: %s%s":     "地址: %s%s",
	"target: %s":        "目标: %s",
	"config file: %s":   "配置文件: %s",
	"password: set":     "密码: 已设置",
	"password: <unset>": "密码: <未设置>",

	// 配置读写 / 迁移
	"read %s":             "读取 %s",
	"parse %s":            "解析 %s",
	"write %s":            "写入 %s",
	"create directory %s": "创建目录 %s",
	"legacy credentials migrated, but the backup failed (%v); handle %s manually":                     "旧凭据文件已迁移，但备份失败（%v）；请手动处理 %s",
	"migrated %s into context %q; the old file is backed up at %s":                                    "已把 %s 迁移为 context %q；旧文件备份在 %s",
	"to pin that context to an address, rerun `nexus-cli login --context default --url <url>`":        "如需指定该 context 的目标地址，重跑 `nexus-cli login --context default --url <地址>`",
	"missing credentials: run `nexus-cli login --user <account>`, or set NEXUS_USER / NEXUS_PASSWORD": "缺少凭据：运行 `nexus-cli login --user <账号>`，或设置 NEXUS_USER / NEXUS_PASSWORD",

	// 内嵌 spec / 缓存
	"read embedded %s":         "读取内嵌 %s",
	"read embedded spec dir":   "读取内嵌 spec 目录",
	"parse servers of %s":      "解析 %s 的 servers",
	"parse info.version of %s": "解析 %s 的 info.version",
	"create cache dir %s":      "创建缓存目录 %s",
	"write cache %s":           "写入缓存 %s",
	"expected exactly one vendor spec (.json), got %d: %v": "期望恰好一个 vendor spec (.json)，实际 %d 个: %v",
	"read vendor spec":    "读取 vendor spec",
	"parse vendor spec":   "解析 vendor spec",
	"build OpenAPI model": "构建 OpenAPI 模型",
	"spec has no paths":   "spec 里没有 paths",
	"read overlay %s":     "读取覆盖层 %s",
	"parse overlay %s":    "解析覆盖层 %s",

	// 上传
	"maven requires group":    "maven 需要 group",
	"maven requires artifact": "maven 需要 artifact",
	"maven requires version":  "maven 需要 version",
	"go requires version":     "go 需要 version",
	"unsupported format %q":   "不支持的格式 %q",
	"%q is not uploaded over REST; use a native client such as docker/helm push": "%q 不走 REST 上传，请用 docker/helm push 等原生客户端",
	"upload: --repo is required":                                          "upload: 需要 --repo",
	"upload: --format is required":                                        "upload: 需要 --format",
	"upload: --file is required":                                          "upload: 需要 --file",
	"upload: not a regular file: %s":                                      "upload: 文件不存在或不是普通文件: %s",
	"upload: --field expects k=v, got %q":                                 "upload: --field 需要 k=v 形式，收到 %q",
	"uploaded (HTTP %d): %s -> %s":                                        "上传成功 (HTTP %d): %s -> %s",
	"upload rejected (HTTP %d): check credentials and write access to %s": "上传被拒（HTTP %d）：检查凭据与对 %s 的写权限",
	"upload failed (HTTP %d): %s":                                         "上传失败（HTTP %d）: %s",
	"request failed":                                                      "请求失败",

	// 批量上传
	"upload-dir: --repo is required":                                     "upload-dir: 需要 --repo",
	"upload-dir: --dir is required":                                      "upload-dir: 需要 --dir",
	"upload-dir: --jobs must be >= 1":                                    "upload-dir: --jobs 需 >= 1",
	"upload-dir: --format supports raw | maven, got %q":                  "upload-dir: --format 目前支持 raw | maven，收到 %q",
	"upload-dir: no such directory: %s":                                  "upload-dir: 目录不存在: %s",
	"upload-dir: no files matched (check --dir / --include / --exclude)": "upload-dir: 没有匹配到文件（检查 --dir / --include / --exclude）",
	"asset: %s":                         "资产: %s",
	"directory: /%s":                    "目录: /%s",
	"planned %d targets (nothing sent)": "计划 %d 个目标（未发送）",
	"summary: %d ok / %d failed":        "汇总: 成功 %d / 失败 %d",
	" / %d skipped":                     " / 跳过 %d",
	" (%d targets)":                     "（共 %d 个目标）",
	"%d targets failed":                 "有 %d 个目标失败",

	// 任务
	"usage: nexus-cli task wait <taskId> [--timeout seconds] [--interval seconds]": "用法: nexus-cli task wait <taskId> [--timeout 秒] [--interval 秒]",
	"read task %s":                                          "读取任务 %s",
	"read task %s failed (HTTP %d): %s":                     "读取任务 %s 失败（HTTP %d）: %s",
	"parse task %s":                                         "解析任务 %s",
	"task %s finished (state=%s, lastRunResult=%s)":         "任务 %s 已完成（state=%s, lastRunResult=%s）",
	"task %s did not succeed (state=%s, lastRunResult=%s)":  "任务 %s 未成功（state=%s, lastRunResult=%s）",
	"task %s did not succeed":                               "任务 %s 未成功",
	"timed out waiting for task %s (%ds), current state %s": "等待任务 %s 超时（%ds），当前状态 %s",

	// 输出选项 / MCP
	"output as JSON":                                                  "以 JSON 输出",
	"output format (currently json)":                                  "输出格式（当前支持 json）",
	"unsupported output format %q (only json)":                        "不支持的输出格式 %q（目前只支持 json）",
	"mcp: --read-only and --allow-destructive are mutually exclusive": "mcp: --read-only 与 --allow-destructive 互斥",
	"nexus-cli mcp: %d tools (read_only=%v, destructive=%v)":          "nexus-cli mcp: %d 个工具（read_only=%v, destructive=%v）",
	"failed to parse JSON: %s":                                        "JSON 解析失败: %s",
	"unsupported method: %s":                                          "不支持的方法: %s",
	"failed to parse arguments: %s":                                   "参数解析失败: %s",
	"unknown tool: %s":                                                "未知工具: %s",
	"failed to resolve context: %s":                                   "解析 context 失败: %s",
	"encode body":                                                     "序列化 body",
	"encode request body":                                             "序列化请求体",
	"request body (JSON)":                                             "请求体（JSON）",
	"  ⚠ destructive operation":                                       "  ⚠ 破坏性操作",

	// 手工 MCP 工具：component_upload
	"target repository name":                       "目标仓库名",
	"absolute path of the file to upload":          "要上传的文件绝对路径",
	"maven groupId":                                "maven groupId",
	"maven artifactId":                             "maven artifactId",
	"maven packaging, default jar":                 "maven packaging，默认 jar",
	"maven classifier":                             "maven classifier",
	"extension, default from the file name":        "扩展名，默认取自文件名",
	"directory prefix for raw/yum":                 "raw/yum 的目录前缀",
	"file name for raw/yum, default from the path": "raw/yum 的文件名，默认取自路径",
	"missing repository":                           "缺少 repository",
	"Upload one component (multipart). The Nexus components endpoint requires literal dotted field names, so this is implemented directly. [POST /v1/components]": "上传单个构件（multipart）。Nexus 的 components 接口要求字面量点号字段名，因此单独实现。[POST /v1/components]",
}

// helpText 保存多行帮助文本的中文版本，键与 i18n.H 的第一个参数一致。
var helpText = map[string]string{
	"help.login": `用法: nexus-cli login --user <账号> [--password <密码> | --password-stdin]
                      [--url <地址>] [--context <名字>]

把目标地址与凭据作为一个 context 保存到
$XDG_CONFIG_HOME/nexus-cli/config.json（权限 0600），保存前会先校验一次。

  --user <账号>          要保存的账号（建议使用专用只读账号）
  --password <密码>      密码或 user token passcode；会出现在 ps 与 shell 历史里
  --password-stdin       从 stdin 读取密码（适合脚本/CI）
  --url <地址>           目标地址；默认取 NEXUS_URL、当前 context 或编译期默认值
  --context <名字>       context 名；默认取按地址匹配到的 context、当前 context 或 default
  -o json, --json        JSON 输出

示例:
  nexus-cli login --user ci-readonly
  nexus-cli login --url http://dev:8081 --user ci --context dev
  nexus-cli context use dev

关于 token:
  Nexus 的 User Token 是 Pro 专属功能（Community 版没有该接口，调用返回 404）。
  若你在 Pro 版或 Web UI 里拿到了 user token，直接把它当 --password 传入：
  Nexus 用「token name + passcode」替代「用户名 + 密码」做 Basic 认证。
`,

	"help.whoami": `用法: nexus-cli whoami [--context <名字>] [--url <地址>] [--check] [-o json]

显示当前生效的 context、目标地址，以及每个值的来源。

  --context <名字>   查看指定 context
  --url <地址>       按指定地址解析
  --check            发一次请求确认凭据可用
`,

	"help.context": `用法: nexus-cli context <子命令>

  context list              列出所有 context（* 为当前）
  context use <名字>        切换当前 context
  context show [名字]       查看某个 context（不填则当前）
  context remove <名字>     删除 context
  -o json, --json           JSON 输出
`,

	"help.upload": `用法: nexus-cli upload --repo <仓库> --format <格式> --file <文件> [选项]

上传单个构件。Nexus 的 components 接口要求 multipart 使用字面量点号字段名
（如 maven2.asset1）；Restish 的请求体 shorthand 会把点号当嵌套结构，
所以本命令自己拼 multipart。

通用选项:
  --repo <仓库>          必填
  --format <格式>        maven | raw | yum | go | conda | composer |
                         npm | pypi | nuget | helm | rubygems | r | apt
  --file <文件>          必填
  --field k=v            追加表单字段（可重复）
  --dry-run              只打印请求，不发送
  -o json, --json        JSON 输出

maven: --group --artifact --version [--packaging jar] [--classifier s] [--extension jar]
raw:   [--directory /path] [--filename name]
yum:   [--directory /x86_64]
go:    --version <v>
conda: [--filename name] [--version v]
composer: [--version v]
`,

	"help.upload-dir": `用法: nexus-cli upload-dir --repo <仓库> --dir <目录> [选项]

批量上传一个目录。Nexus 没有批量接口，这里在客户端并发。

  --repo <仓库>          必填
  --dir <目录>           必填
  --format raw|maven     默认 raw
  --prefix <路径>        raw 模式的目录前缀，例如 /release/1.0
  --include <glob>       只传文件名匹配的文件
  --exclude <glob>       跳过文件名匹配的文件
  --jobs N               并发数（默认 4）
  --dry-run              只打印计划，不发送（不需要凭据）
  -o json, --json        JSON 汇总

raw 模式保留目录结构:
  ./dist/docs/a.txt  ->  raw.directory=<prefix>/docs, filename=a.txt

maven 模式按 <group 路径>/<artifactId>/<version>/<文件> 推断坐标:
  ./m2/com/example/app/1.0.0/app-1.0.0.jar
    -> groupId=com.example artifactId=app version=1.0.0
  同目录文件按「主件 → pom → 带 classifier」排序，一次请求最多 3 个 asset。
  maven 模式默认跳过 maven-metadata*.xml。

失败不会中断，全部跑完后打印失败清单；有任何失败则退出码 1。
`,

	"help.task-wait": `用法: nexus-cli task wait <taskId> [--timeout 秒] [--interval 秒] [-o json]

轮询异步任务直到终态。退出码: 0=成功, 1=失败, 124=超时。

Nexus 的任务定义在空闲时也是 WAITING，所以只有观察到 RUNNING 之后又回到空闲，
才算这一轮跑完，再由 lastRunResult 判断成败。
`,

	"help.mcp": `用法: nexus-cli mcp [--read-only | --allow-destructive]

以 stdio 方式运行 MCP server，供 AI agent 接入。

档位:
  默认                    读操作 + 生产性写（构件上传、触发任务）
  --read-only             仅 GET/HEAD
  --allow-destructive     额外放开删除、security 变更、configuration set、script run
`,

	"help.version": `用法: nexus-cli version [-o json]

打印 CLI 版本、内嵌 OpenAPI spec 的版本，以及构建目标。
命令面由内嵌 spec 决定，它不一定等于你所连接服务器的版本。
`,
}

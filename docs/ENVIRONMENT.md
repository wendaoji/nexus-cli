# 环境变量

本文档是 `nexus-cli` 支持的环境变量清单。未列出的底层依赖环境变量不属于公开接口，
可能在后续版本中被忽略或移除。

## 运行时变量

| 变量 | 用途 | 优先级与说明 |
|---|---|---|
| `NEXUS_URL` | Nexus 服务地址 | 低于 `--url`，高于 context 和编译期默认地址 |
| `NEXUS_USER` | Nexus 用户名 | 与 `NEXUS_PASSWORD` 一起使用；高于 context 中保存的凭据 |
| `NEXUS_PASSWORD` | Nexus 密码或 token | 与 `NEXUS_USER` 一起使用；高于 context 中保存的凭据 |
| `NEXUS_CONTEXT` | 选择命名 context | 低于 `--context`，高于配置中的 `current` |
| `NEXUS_CLI_LANG` | 输出语言 | 支持 `en`、`zh`；低于 `--lang` |
| `LC_ALL` | locale | 未指定 `--lang` 和 `NEXUS_CLI_LANG` 时用于语言检测 |
| `LC_MESSAGES` | 消息 locale | 低于 `LC_ALL` |
| `LANG` | 默认 locale | 低于 `LC_MESSAGES`；值以 `zh` 开头时使用中文，否则使用英文 |
| `XDG_CONFIG_HOME` | 用户配置根目录 | 非 Windows 平台默认回退到 `~/.config` |
| `XDG_CACHE_HOME` | 用户缓存根目录 | 非 Windows 平台默认回退到 `~/.cache` 或平台缓存目录 |
| `HOME` | 用户主目录 | 无 XDG 路径时用于确定配置和缓存位置 |

语言优先级：

```text
--lang > NEXUS_CLI_LANG > LC_ALL > LC_MESSAGES > LANG > en
```

目标与凭据优先级：

```text
context：--context > NEXUS_CONTEXT > config.current
地址：   --url > NEXUS_URL > context.url > 编译期默认地址
凭据：   NEXUS_USER/NEXUS_PASSWORD > context 中保存的凭据
```

示例：

```bash
NEXUS_CONTEXT=dev nexus-cli repo list
NEXUS_URL=http://dev:8081 nexus-cli status show
NEXUS_USER=ci NEXUS_PASSWORD="$TOKEN" nexus-cli repo list
NEXUS_CLI_LANG=zh nexus-cli --help
```

## 配置与缓存位置

用户需要维护的配置只有：

```text
$XDG_CONFIG_HOME/nexus-cli/config.json
```

未设置 `XDG_CONFIG_HOME` 时通常是 `~/.config/nexus-cli/config.json`。文件包含
context、目标地址与凭据，保存权限为 `0600`。

内嵌 spec 和命令元数据写入 `$XDG_CACHE_HOME/nexus-cli`（或平台默认缓存目录）。
缓存可以安全删除，下一次运行会重新生成；它不是用户配置。

## 开发与维护变量

以下变量用于仓库脚本，不属于最终二进制的配置接口：

| 变量 | 使用位置 | 用途 |
|---|---|---|
| `NEXUS_CLI` | `scripts/verify.sh` | 指定要验证的二进制路径 |
| `NEXUS_URL` | `scripts/sync-spec.sh` | 指定从哪个 Nexus 实例抓取维护用 spec |

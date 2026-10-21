#!/usr/bin/env bash
# 把本仓库的 Nexus API 定义安装到 Restish 的「全局配置」里。
#
# 为什么用全局配置而不是项目级 .restish.json：
#   1. Restish 的相对 spec_files 是按「当前工作目录」解析的，放在项目配置里
#      就只有 cwd=仓库根 时能用，在子目录或别处会报 "no such file or directory"。
#   2. 一旦用 RSH_CONFIG / --rsh-config 显式指定配置，Restish 会【禁用插件加载】，
#      而 MCP（restish mcp）本身是个插件 —— MCP 就会失效。
#   所以：模板里的相对路径在这里被绝对化，写入全局配置，之后在任意目录都能用。
#
# 幂等：可重复执行；改完 config/restish.api.json 或升级 spec 后重跑一次即可。
#
# 用法: scripts/install.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEMPLATE="$ROOT/config/restish.api.json"
GLOBAL="${RSH_GLOBAL_CONFIG:-${XDG_CONFIG_HOME:-$HOME/.config}/restish/restish.json}"

command -v jq >/dev/null || { echo "需要 jq" >&2; exit 1; }
[ -f "$TEMPLATE" ] || { echo "找不到模板: $TEMPLATE" >&2; exit 1; }

# 模板里的 spec 文件必须都已生成
while IFS= read -r f; do
  abs="$ROOT/${f#./}"
  [ -f "$abs" ] || { echo "缺少 spec 文件: ${abs}（先跑 scripts/gen-tags.sh）" >&2; exit 1; }
done < <(jq -r '.apis["nexus-cli"].spec_files[]' "$TEMPLATE")

mkdir -p "$(dirname "$GLOBAL")"
[ -f "$GLOBAL" ] || printf '{}\n' > "$GLOBAL"

# 把 ./spec/... 绝对化为 $ROOT/spec/...
ENTRY="$(jq --arg root "$ROOT" '
  .apis["nexus-cli"]
  | .spec_files = [ .spec_files[] | if startswith("./") then ($root + "/" + .[2:]) else . end ]
' "$TEMPLATE")"

jq --argjson entry "$ENTRY" '.apis["nexus-cli"] = $entry' "$GLOBAL" > "$GLOBAL.tmp"
mv "$GLOBAL.tmp" "$GLOBAL"
chmod 600 "$GLOBAL"

echo "已写入全局配置: $GLOBAL"
jq -r '.apis["nexus-cli"]
  | "  base_url  : \(.base_url)",
    "  spec_files:",
    (.spec_files[] | "    - " + .)' "$GLOBAL"

echo
echo "同步 spec 到 Restish 缓存 ..."
restish api sync nexus-cli

echo
echo "完成。验证:"
echo "  export NEXUS_USER=... NEXUS_PASSWORD=..."
echo "  restish nexus-cli status show -o json"
echo "  nexus-cli repo list -o json"

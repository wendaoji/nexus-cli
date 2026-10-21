#!/usr/bin/env bash
# 生成「机械性」的 tag 映射 overlay。
#
# 为什么需要：Restish 的命令分组来自 operation 的 tags[0]，而 tags 只能逐 operation
# 设置。Nexus spec 里 /v1/repositories* 下有 ~130 个 path，手写不现实。
# 这个脚本把它们机械地映射成短分组名，输出到一个 *生成* 文件（不要手改）。
#
# 输出覆盖两层机械规则：
#   1. path 前缀 -> 短分组名（tags），供 command_layout: tags 使用
#   2. 所有 DELETE 操作 -> x-mcp-ignore: true（对 MCP 屏蔽破坏性操作，CLI 仍可用）
# 人工语义（x-cli-name / x-cli-ignore / 额外 x-mcp-ignore / securitySchemes）
# 放在 spec/nexus-overlay.yaml，Restish 按 spec_files 顺序 deep-merge，后者覆盖前者。
#
# 用法: scripts/gen-tags.sh [spec.json] [out.yaml]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# 默认从 config/restish.api.json 的 spec_files[0] 解析当前 vendor spec，避免硬编码版本号
SPEC="${1:-}"
if [ -z "$SPEC" ]; then
  SPEC="$(jq -r '.apis["nexus-cli"].spec_files[0] // empty' "$ROOT/config/restish.api.json" 2>/dev/null || true)"
  case "$SPEC" in
    ./*) SPEC="$ROOT/${SPEC#./}";;
    "")  SPEC="";;
  esac
fi
# 兜底：取仓库内版本号最高的 spec/nexus-*.json（不硬编码版本，sync-spec.sh 会改文件名）
[ -n "$SPEC" ] && [ -f "$SPEC" ] || SPEC="$(ls -1 "$ROOT"/spec/nexus-*.json 2>/dev/null | sort -V | tail -1)"

OUT="${2:-$ROOT/spec/nexus-tags.generated.yaml}"

command -v jq >/dev/null || { echo "需要 jq" >&2; exit 1; }
[ -f "$SPEC" ] || { echo "找不到 spec: $SPEC" >&2; exit 1; }

VERSION="$(jq -r '.info.version' "$SPEC")"

# path 前缀 -> 分组名。顺序敏感：更具体的前缀写在前面。
# 注意：不匹配时返回 null（不要用 empty，否则 if 会把该条目整个丢掉）。
jq -r --arg v "$VERSION" '
  def group($p):
    # 非 /v1 前缀的 path（/beta、/internal）也要归位，否则会与 /v1 的同名分组撞出
    # 一个 "xxx-2" 的重复分组。
    if   $p | startswith("/beta/status")            then "status"
    elif $p | startswith("/beta/data-store")        then "datastore"
    elif $p | startswith("/v1/repository-dashboard") then "repo"
    elif $p | startswith("/v1/repositorySettings")  then "repo"
    elif $p | startswith("/v1/repositories")        then "repo"
    elif $p | startswith("/v1/components")          then "component"
    elif $p | startswith("/v1/assets")              then "asset"
    elif $p | startswith("/v1/search")              then "search"
    elif $p | startswith("/v1/blobstores")          then "blobstore"
    elif $p | startswith("/v1/azureblobstore")      then "blobstore"
    elif $p | startswith("/v1/tasks")               then "task"
    elif $p | startswith("/v1/script")              then "script"
    elif $p | startswith("/v1/security")            then "security"
    elif $p | startswith("/v1/status")              then "status"
    elif $p | startswith("/v1/capabilities")        then "capability"
    elif $p | startswith("/v1/routing-rules")       then "routing"
    elif $p | startswith("/v1/formats")             then "format"
    elif $p | startswith("/v1/email")               then "email"
    elif $p | startswith("/v1/lifecycle")           then "lifecycle"
    elif $p | startswith("/v1/configuration")       then "config"
    elif $p | startswith("/v1/plan")                then "datastore"
    else null end;

  # 单个 operation 的机械补丁：分组 + DELETE 屏蔽
  def op_patch($g; $m):
    (if $g == null then {} else {tags: [$g]} end)
    + (if $m == "delete" then {"x-mcp-ignore": true} else {} end);

  {
    openapi: "3.0.1",
    info: {title: "Nexus Repository Manager REST API", version: $v},
    paths: (
      .paths
      | with_entries(
          .key as $p
          | (group($p)) as $g
          | .value |= with_entries(
              select(.key | test("^(get|post|put|delete|patch)$"))
              | .value = op_patch($g; .key))
        )
    )
  }
' "$SPEC" > "$OUT"

echo "已生成 $OUT"
echo "覆盖 path 数: $(jq '.paths|length' "$OUT")"
echo "DELETE 已屏蔽 (x-mcp-ignore) 数: $(jq '[.paths[][] | select(."x-mcp-ignore")] | length' "$OUT")"
echo "分组分布:"
jq -r '.paths[] | to_entries[] | .value.tags[0] // "（保留原分组）"' "$OUT" | sort | uniq -c | sort -rn

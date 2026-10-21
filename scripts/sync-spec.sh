#!/usr/bin/env bash
# 从 Nexus 拉取最新的 OpenAPI spec，重新生成机械层 overlay，校验一致性，并重装到 Restish。
#
# Nexus 升级会改动 spec（新增/删除 operation）。这个脚本把「升级」变成一条可重复的
# 命令，并明确指出人工层 overlay 是否需要跟着改。
#
# 用法:
#   scripts/sync-spec.sh                     # 用 config/restish.api.json 里的 base_url
#   NEXUS_URL=http://host:8081 scripts/sync-spec.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEMPLATE="$ROOT/config/restish.api.json"

command -v jq >/dev/null || { echo "需要 jq" >&2; exit 1; }

BASE="${NEXUS_URL:-$(jq -r '.apis["nexus-cli"].base_url' "$TEMPLATE")}"
OLD_REL="$(jq -r '.apis["nexus-cli"].spec_files[0]' "$TEMPLATE")"
OLD_SPEC="$ROOT/${OLD_REL#./}"

echo "拉取 $BASE/service/rest/swagger.json ..."
TMP="$(mktemp)"
curl -fsS -m 30 "$BASE/service/rest/swagger.json" -o "$TMP"

NEW_VER="$(jq -r '.info.version' "$TMP")"
OLD_VER="$(jq -r '.info.version' "$OLD_SPEC" 2>/dev/null || echo '<无>')"
NEW_REL="./spec/nexus-${NEW_VER}.json"
NEW_SPEC="$ROOT/spec/nexus-${NEW_VER}.json"

mkdir -p "$ROOT/spec"
mv "$TMP" "$NEW_SPEC"
echo "版本: $OLD_VER -> $NEW_VER"

if [ "$OLD_REL" != "$NEW_REL" ]; then
  echo "spec 文件名变化，更新模板 spec_files[0]: $OLD_REL -> $NEW_REL"
  jq --arg old "$OLD_REL" --arg new "$NEW_REL" \
    '.apis["nexus-cli"].spec_files = [ .apis["nexus-cli"].spec_files[] | if . == $old then $new else . end ]' \
    "$TEMPLATE" > "$TEMPLATE.tmp" && mv "$TEMPLATE.tmp" "$TEMPLATE"
  [ "$OLD_SPEC" != "$NEW_SPEC" ] && echo "提示: $OLD_REL 已不再被引用，确认后可删除。"
fi

# operationId 差异：新增通常无害；删除会让人工层 overlay 报错（下面的校验会拦住）
if [ -f "$OLD_SPEC" ] && [ "$OLD_SPEC" != "$NEW_SPEC" ]; then
  mkdir -p "$ROOT/.cache"
  jq -r '.paths[][] | .operationId // empty' "$OLD_SPEC" | sort -u > "$ROOT/.cache/ops-old.txt"
  jq -r '.paths[][] | .operationId // empty' "$NEW_SPEC" | sort -u > "$ROOT/.cache/ops-new.txt"
  echo "新增 operation: $(comm -13 "$ROOT/.cache/ops-old.txt" "$ROOT/.cache/ops-new.txt" | tr '\n' ' ')"
  echo "删除 operation: $(comm -23 "$ROOT/.cache/ops-old.txt" "$ROOT/.cache/ops-new.txt" | tr '\n' ' ')"
fi

echo
echo "== 重新生成机械层 =="
bash "$ROOT/scripts/gen-tags.sh" "$NEW_SPEC"

echo
echo "== 校验 overlay =="
python3 "$ROOT/scripts/validate-overlay.py" "$NEW_SPEC"

echo
echo "== 安装到 Restish 全局配置 =="
bash "$ROOT/scripts/install.sh"

echo
echo "完成。建议再跑: scripts/verify.sh"

#!/usr/bin/env bash
# 从 Nexus 拉取最新的 OpenAPI spec，重新生成机械层 overlay，并校验一致性。
#
# Nexus 升级会改动 spec（新增/删除 operation）。这个脚本把「升级」变成一条可重复的
# 命令，并明确指出人工层 overlay 是否需要跟着改。
#
# 用法:
#   scripts/sync-spec.sh                       # 用 config/default-origin.txt 里的地址
#   NEXUS_URL=http://host:8081 scripts/sync-spec.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ORIGIN_FILE="$ROOT/config/default-origin.txt"

command -v jq >/dev/null || { echo "需要 jq" >&2; exit 1; }

BASE="${NEXUS_URL:-$(tr -d '[:space:]' < "$ORIGIN_FILE")}"
OLD_SPEC="$(ls -1 "$ROOT"/spec/nexus-*.json 2>/dev/null | sort -V | tail -1 || true)"

echo "拉取 $BASE/service/rest/swagger.json ..."
TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT
curl -fsS -m 30 "$BASE/service/rest/swagger.json" -o "$TMP"

NEW_VER="$(jq -r '.info.version' "$TMP")"
OLD_VER="$(jq -r '.info.version' "${OLD_SPEC:-/dev/null}" 2>/dev/null || echo '<无>')"
NEW_SPEC="$ROOT/spec/nexus-${NEW_VER}.json"

mkdir -p "$ROOT/spec"
mv "$TMP" "$NEW_SPEC"
trap - EXIT
echo "版本: $OLD_VER -> $NEW_VER"

# operationId 差异：新增通常无害；删除会让人工层 overlay 报错（下面的校验会拦住）
if [ -n "$OLD_SPEC" ] && [ -f "$OLD_SPEC" ] && [ "$OLD_SPEC" != "$NEW_SPEC" ]; then
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

# 二进制里 go:embed 只接受恰好一个 vendor .json，所以升级成功后清掉旧版本
if [ -n "$OLD_SPEC" ] && [ -f "$OLD_SPEC" ] && [ "$OLD_SPEC" != "$NEW_SPEC" ]; then
  echo
  echo "== 清理旧 vendor spec =="
  rm -f "$OLD_SPEC"
  echo "已删除 $(basename "$OLD_SPEC")"
fi

echo
echo "完成。下一步: make build && scripts/verify.sh"

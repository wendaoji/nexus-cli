#!/usr/bin/env bash
# 冒烟验证：仓库结构、命令面、隐藏项、MCP 只读、上传参数整形。
# 需要凭据的检查在缺少 NEXUS_USER/NEXUS_PASSWORD 时自动跳过（不算失败）。
#
# 用法: scripts/verify.sh
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NEXUS="$ROOT/bin/nexus-cli"
PASS=0; FAIL=0; SKIP=0

ok()   { printf '  \033[32m✓\033[0m %s\n' "$*"; PASS=$((PASS+1)); }
bad()  { printf '  \033[31m✗\033[0m %s\n' "$*"; FAIL=$((FAIL+1)); }
skip() { printf '  \033[33m-\033[0m %s\n' "$*"; SKIP=$((SKIP+1)); }
head_() { printf '\n\033[1m%s\033[0m\n' "$*"; }

have_creds() { [ -n "${NEXUS_USER:-}" ] && [ -n "${NEXUS_PASSWORD:-}" ]; }

# ── 1. 依赖 ────────────────────────────────────────────────────────────────
head_ "1. 依赖"
for c in restish jq curl python3; do
  if command -v "$c" >/dev/null 2>&1; then ok "$c"; else bad "$c 缺失"; fi
done

# ── 2. spec 与 overlay ─────────────────────────────────────────────────────
head_ "2. spec / overlay"
# spec 文件名不硬编码（sync-spec.sh 会随版本改名），从模板解析
while IFS= read -r f; do
  rel="${f#./}"
  [ -f "$ROOT/$rel" ] && ok "$rel" || bad "$rel 缺失"
done < <(jq -r '.apis["nexus-cli"].spec_files[]' "$ROOT/config/restish.api.json")
for f in config/restish.api.json; do
  [ -f "$ROOT/$f" ] && ok "$f" || bad "$f 缺失"
done
if python3 "$ROOT/scripts/validate-overlay.py" >/dev/null 2>&1; then
  ok "overlay 校验通过（无凭空 method / 命名冲突）"
else
  bad "overlay 校验失败（运行 scripts/validate-overlay.py 看详情）"
fi

# ── 3. 已注册到 Restish ────────────────────────────────────────────────────
head_ "3. Restish 注册"
if restish api list 2>/dev/null | grep -q '^nexus-cli[[:space:]]'; then
  ok "API nexus-cli 已注册"
  base="$(restish config show -o json 2>/dev/null | jq -r '.apis["nexus-cli"].base_url')"
  ok "base_url = $base"
else
  bad "API nexus-cli 未注册，请运行 scripts/install.sh"
fi

# ── 4. 命令面 ──────────────────────────────────────────────────────────────
head_ "4. 命令面"
CMDS=(
  "repo list" "repo get" "component list" "component upload-unsupported"
  "asset list" "asset get" "search query" "search assets" "search versions"
  "blobstore list" "task list" "task get" "task run" "script list" "script run"
  "security user-list" "security role-list" "security privilege-list"
  "status show" "status writable" "capability list" "routing list"
)
for c in "${CMDS[@]}"; do
  if restish nexus-cli $c --help >/dev/null 2>&1; then ok "nexus-cli $c"; else bad "nexus-cli $c 缺失"; fi
done

# ── 5. 分组整洁性 ──────────────────────────────────────────────────────────
head_ "5. 分组"
groups_out="$(restish nexus-cli --help 2>&1)"
ngroups="$(sed -n '/Available Commands:/,/^$/p' <<<"$groups_out" | grep -cE '^  [a-z]')"
[ "$ngroups" -ge 10 ] && ok "分组数 $ngroups" || bad "分组数异常: $ngroups"
if grep -qE '^  [a-z-]+-2 ' <<<"$groups_out"; then
  bad "存在重复分组（-2 后缀），说明有 tag 撞名"
else
  ok "无重复分组"
fi

# ── 6. 隐藏项确实不可见 ────────────────────────────────────────────────────
head_ "6. 隐藏（Pro / 内部）"
for kw in ldap license malicious-risk hosted-repository-evaluation recovery-mode; do
  if restish nexus-cli --help 2>&1 | grep -qi -- "$kw"; then
    bad "'$kw' 仍出现在命令面"
  else
    ok "'$kw' 已隐藏"
  fi
done

# ── 7. wrapper ─────────────────────────────────────────────────────────────
head_ "7. bin/nexus-cli wrapper"
if bash -n "$NEXUS" 2>/dev/null; then ok "语法正确"; else bad "语法错误"; fi
if "$NEXUS" --help >/dev/null 2>&1; then ok "--help 可用"; else bad "--help 失败"; fi

# ── 8. upload 参数整形（dry-run，不发送）───────────────────────────────────
head_ "8. nexus-cli upload (dry-run)"
testjar="$(mktemp -t nexusverify.XXXXXX.jar)"; echo x > "$testjar"
out="$("$NEXUS" upload --dry-run --repo R --format maven --file "$testjar" \
        --group com.example --artifact app --version 1.0.0 2>&1)"
for field in maven2.groupId maven2.artifactId maven2.version maven2.asset1 maven2.asset1.extension; do
  if grep -q -- "-F $field" <<<"$out"; then ok "字段 $field"; else bad "字段 $field 未生成"; fi
done
if [ -n "$out" ] && ! grep -qE 'name=" ' <<<"$out"; then
  ok "字段名无前导空格"
elif [ -z "$out" ]; then
  bad "dry-run 无输出（应为空即视为失败）"
else
  bad "字段名含前导空格"
fi
# 子命令自身的 --help 必须可用（不要落到"把 --help 当参数"的坑里）
for sub in "upload --help" "upload-dir --help" "task wait --help" "login --help" "whoami"; do
  "$NEXUS" $sub >/dev/null 2>&1 && ok "nexus-cli $sub" || bad "nexus-cli $sub 不可用"
done
if "$NEXUS" upload --repo R --format maven --file "$testjar" >/dev/null 2>&1; then
  bad "缺 --group 时应当报错"
else
  ok "缺必填参数时正确报错"
fi
rm -f "$testjar"

# ── 8b. upload-dir（dry-run，不发送）───────────────────────────────────────
head_ "8b. nexus-cli upload-dir (dry-run)"
ud="$(mktemp -d -t nexusud.XXXXXX)"
mkdir -p "$ud/m2/com/example/app/1.0.0" "$ud/dist/docs" "$ud/dist/img"
for f in app-1.0.0.jar app-1.0.0.pom app-1.0.0-sources.jar maven-metadata.xml; do
  echo x > "$ud/m2/com/example/app/1.0.0/$f"
done
echo a > "$ud/dist/docs/a.txt"; echo b > "$ud/dist/img/b.png"; echo c > "$ud/dist/.DS_Store"

out="$("$NEXUS" upload-dir --repo R --format maven --dir "$ud/m2" --dry-run 2>&1)"
grep -q 'maven2.groupId=com.example' <<<"$out" && ok "maven 坐标推断（group 路径→groupId）" \
  || bad "maven 坐标推断失败"
grep -q 'maven2.asset1=@.*app-1.0.0.jar' <<<"$out" && ok "asset1 = 主件" || bad "asset1 不是主件"
grep -q 'maven2.asset3.classifier=sources' <<<"$out" && ok "sources 带 classifier" \
  || bad "sources classifier 缺失"
grep -q 'maven-metadata.xml' <<<"$out" && bad "maven-metadata.xml 未被跳过" \
  || ok "maven-metadata.xml 已跳过"

out="$("$NEXUS" upload-dir --repo R --dir "$ud/dist" --prefix /rel --dry-run 2>&1)"
grep -q 'raw.directory=/rel/docs' <<<"$out" && ok "raw 保留目录结构于 --prefix 下" \
  || bad "raw.directory 不符预期"
grep -q '.DS_Store' <<<"$out" && bad ".DS_Store 未被跳过" || ok ".DS_Store 已跳过"

out="$("$NEXUS" upload-dir --repo R --dir "$ud/dist" --exclude '*.png' --dry-run 2>&1)"
grep -q 'b.png' <<<"$out" && bad "--exclude '*.png' 未生效" || ok "--exclude 生效"

if "$NEXUS" upload-dir --repo R --dir "$ud/nonexistent" >/dev/null 2>&1; then
  bad "目录不存在时应当报错"
else
  ok "目录不存在时正确报错"
fi
rm -rf "$ud"

# ── 8c. 凭据文件 ───────────────────────────────────────────────────────────
head_ "8c. 凭据文件"
who="$(env -u NEXUS_USER -u NEXUS_PASSWORD "$NEXUS" whoami 2>&1)"
grep -q '凭据文件' <<<"$who" && ok "whoami 显示凭据文件路径" || bad "whoami 输出异常"
who2="$(NEXUS_USER=envuser NEXUS_PASSWORD=envpass "$NEXUS" whoami 2>&1)"
grep -q '凭据来源: 环境变量' <<<"$who2" && ok "环境变量优先于文件" || bad "凭据优先级不符预期"

# ── 8d. login 本地行为 ─────────────────────────────────────────────────────
head_ "8d. login 本地行为"
if "$NEXUS" login --help >/dev/null 2>&1; then ok "login --help"; else bad "login --help 失败"; fi
if "$NEXUS" login >/dev/null 2>&1; then
  bad "缺 --user 时应当报错"
else
  ok "缺 --user 时正确报错"
fi
# 非交互且未给 --password-stdin：应明确报错，且不应该去联网
out="$(env -u NEXUS_USER -u NEXUS_PASSWORD "$NEXUS" login --user x </dev/null 2>&1)"
if grep -q 'password-stdin' <<<"$out"; then ok "非交互时提示用 --password-stdin"; else bad "非交互报错信息不明确: $out"; fi

# ── 8e. 脚本 lint：$VAR 后紧跟非 ASCII 会 unbound variable ──────────────────
# bash 会把多字节字符并进变量名（变量名后紧跟全角括号/中文等就会中招），
# 必须写 ${VAR}。这个坑在开发中踩过三次，固化成检查。
# 只检查代码：跳过注释行（注释里出现该写法不会真的展开变量）。
head_ "8e. 脚本 lint"
lint_hits="$(grep -rnE '\$[A-Za-z_][A-Za-z0-9_]*[^ -~]' "$ROOT/bin" "$ROOT/scripts" 2>/dev/null \
  | grep -vE ':[0-9]+:[[:space:]]*#' || true)"
if [ -n "$lint_hits" ]; then
  bad "\$VAR 后紧跟非 ASCII（需改成 \${VAR}）："
  sed 's/^/      /' <<<"$lint_hits"
else
  ok "无 \$VAR + 非 ASCII 写法"
fi

# ── 9. MCP 只读 ────────────────────────────────────────────────────────────
head_ "9. MCP 只读"
mcp_err="$(mktemp -t nexusmcp.XXXXXX)"
{ printf '%s\n' '{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"v","version":"0"}}}'; sleep 4; } \
  | restish mcp serve nexus-cli >/dev/null 2>"$mcp_err" &
mpid=$!; wait $mpid 2>/dev/null || true
if grep -q 'hid .* write operation' "$mcp_err"; then
  ok "写操作已对 MCP 隐藏（$(grep -o 'hid [0-9]* write operation' "$mcp_err" | head -1)）"
elif grep -qi 'unknown command' "$mcp_err"; then
  bad "restish mcp 不可用（插件未安装？restish plugin install rest-sh/restish mcp）"
else
  skip "无法确认 MCP 行为，stderr: $(head -c 120 "$mcp_err")"
fi
rm -f "$mcp_err"

# ── 10. 真实连通（需要凭据）────────────────────────────────────────────────
head_ "10. 真实连通（需要 NEXUS_USER / NEXUS_PASSWORD）"
if have_creds; then
  for probe in "status show" "repo list"; do
    # --rsh-retry 0：Restish 默认会按 Retry-After 重试（实测可达 1 分钟以上），
    # 验证场景要快速失败。
    # 注意：HTTP 错误时 Restish 的退出码是 4（不是 1），且响应体打到 stdout。
    out="$(restish --rsh-retry 0 nexus-cli $probe -o json 2>&1)"
    rc=$?
    if [ $rc -eq 0 ]; then
      ok "$probe"
    elif grep -qiE '42[0-9]|40[0-9]|too many auth|unauthor|forbidden' <<<"$out"; then
      # 注意：变量后面紧跟非 ASCII 字符时必须写成 ${probe}，
      # 否则 bash 会把多字节字符并进变量名 → "unbound variable"。
      skip "${probe}：凭据无效或受限（Nexus 对失败认证会限流 429）"
    else
      bad "${probe} 失败 (exit=$rc)：$(tr -d '\n' <<<"$out" | head -c 160)"
    fi
  done
else
  skip "未设置凭据，跳过网络检查"
fi

# ── 汇总 ───────────────────────────────────────────────────────────────────
printf '\n\033[1m汇总\033[0m: %d 通过, %d 失败, %d 跳过\n' "$PASS" "$FAIL" "$SKIP"
[ "$FAIL" -eq 0 ] || exit 1

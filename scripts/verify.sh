#!/usr/bin/env bash
# 冒烟验证：构建产物、命令面、上传参数整形、凭据、MCP 档位。
#
# 依赖（仅维护者需要）：go、jq。运行时不需要任何外部依赖。
# 需要真实凭据的检查在缺少 NEXUS_USER/NEXUS_PASSWORD 时自动跳过。
#
# 用法: scripts/verify.sh
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="${NEXUS_CLI:-$ROOT/dist/nexus-cli}"
PASS=0; FAIL=0; SKIP=0

ok()   { printf '  \033[32m✓\033[0m %s\n' "$*"; PASS=$((PASS+1)); }
bad()  { printf '  \033[31m✗\033[0m %s\n' "$*"; FAIL=$((FAIL+1)); }
skip() { printf '  \033[33m-\033[0m %s\n' "$*"; SKIP=$((SKIP+1)); }
head_() { printf '\n\033[1m%s\033[0m\n' "$*"; }

have_creds() { [ -n "${NEXUS_USER:-}" ] && [ -n "${NEXUS_PASSWORD:-}" ]; }

# ── 0. 构建 ────────────────────────────────────────────────────────────────
head_ "0. 构建"
if command -v go >/dev/null 2>&1; then
  if (cd "$ROOT" && go build -o "$BIN" ./cmd/nexus-cli) 2>/tmp/nx-build.err; then
    ok "go build"
  else
    bad "go build 失败：$(head -3 /tmp/nx-build.err)"
  fi
else
  skip "没有 go 工具链，跳过构建（假设 $BIN 已存在）"
fi
[ -x "$BIN" ] || { printf '\n\033[31m没有可执行文件 %s\033[0m\n' "$BIN"; exit 1; }

# 隔离 HOME：证明不需要任何外部配置文件
FH="$(mktemp -d)"
trap 'rm -rf "$FH"' EXIT
run() { HOME="$FH" "$BIN" "$@"; }

# ── 1. 基本命令面 ──────────────────────────────────────────────────────────
head_ "1. 基本命令"
run version >/dev/null 2>&1 && ok "version" || bad "version"
run --help >/dev/null 2>&1 && ok "--help" || bad "--help"
run restish config path >/dev/null 2>&1 && ok "内置命令收在 restish 命名空间" || bad "restish 命名空间缺失"

# ── 2. 生成命令（无外部配置）──────────────────────────────────────────────
head_ "2. 生成的命令"
for c in "repo list" "component list" "asset list" "search query" "task list" "status show"; do
  run $c --help >/dev/null 2>&1 && ok "nexus-cli $c" || bad "nexus-cli $c 缺失"
done
groups_out="$(run --help 2>&1)"
ngroups="$(sed -n '/Additional Commands:/,/^$/p' <<<"$groups_out" | grep -cE '^  [a-z]')"
[ "$ngroups" -ge 10 ] && ok "分组数 $ngroups" || bad "分组数异常: $ngroups"

# ── 3. 隐藏项 ──────────────────────────────────────────────────────────────
head_ "3. 隐藏（Pro / 内部）"
for kw in ldap license malicious-risk recovery-mode; do
  if run --help 2>&1 | grep -qi -- "$kw"; then bad "'$kw' 仍出现在命令面"; else ok "'$kw' 已隐藏"; fi
done

# ── 4. upload 参数整形 ─────────────────────────────────────────────────────
head_ "4. upload (dry-run)"
jar="$(mktemp -t nxv.XXXXXX.jar)"; echo x > "$jar"
out="$(HOME="$FH" env -u NEXUS_USER -u NEXUS_PASSWORD "$BIN" upload --dry-run \
        --repo R --format maven --file "$jar" --group com.example --artifact app --version 1.0.0 2>&1)"
for f in maven2.groupId maven2.artifactId maven2.version maven2.asset1 maven2.asset1.extension; do
  grep -q -- "-F $f" <<<"$out" && ok "字段 $f" || bad "字段 $f 未生成"
done
grep -qE 'name=" ' <<<"$out" && bad "字段名含前导空格" || ok "字段名无前导空格"
# dry-run 不应要求凭据（上面已用 env -u 验证）
HOME="$FH" env -u NEXUS_USER -u NEXUS_PASSWORD "$BIN" upload --repo R --format raw --file "$jar" >/dev/null 2>&1 \
  && bad "真正发送时缺凭据应当报错" || ok "缺凭据时正确报错"
rm -f "$jar"

# ── 5. upload-dir ─────────────────────────────────────────────────────────
head_ "5. upload-dir (dry-run)"
ud="$(mktemp -d)"
mkdir -p "$ud/m2/com/example/app/1.0.0" "$ud/dist/docs"
for f in app-1.0.0.jar app-1.0.0.pom app-1.0.0-sources.jar maven-metadata.xml; do
  echo x > "$ud/m2/com/example/app/1.0.0/$f"
done
echo a > "$ud/dist/docs/a.txt"; echo c > "$ud/dist/.DS_Store"

out="$(run upload-dir --repo R --format maven --dir "$ud/m2" --dry-run 2>&1)"
grep -q 'com.example:app:1.0.0' <<<"$out" && ok "maven 坐标推断" || bad "maven 坐标推断失败"
grep -qE 'asset1=app-1\.0\.0\.jar' <<<"$out" && ok "asset1 = 主件" || bad "asset1 不是主件"
grep -q 'classifier=sources' <<<"$out" && ok "sources 带 classifier" || bad "classifier 缺失"
grep -q 'maven-metadata.xml' <<<"$out" && bad "maven-metadata.xml 未被跳过" || ok "maven-metadata.xml 已跳过"

out="$(run upload-dir --repo R --dir "$ud/dist" --prefix /rel --dry-run 2>&1)"
grep -q 'directory: /rel/docs' <<<"$out" && ok "raw 保留目录结构" || bad "raw.directory 不符预期"
grep -q '.DS_Store' <<<"$out" && bad ".DS_Store 未被跳过" || ok ".DS_Store 已跳过"

out="$(run upload-dir --repo R --dir "$ud/dist" --exclude '*.txt' --dry-run 2>&1)"
grep -q 'a.txt' <<<"$out" && bad "--exclude 未生效" || ok "--exclude 生效"
rm -rf "$ud"

# ── 6. 凭据 ────────────────────────────────────────────────────────────────
head_ "6. 凭据"
who="$(HOME="$FH" env -u NEXUS_USER -u NEXUS_PASSWORD "$BIN" whoami 2>&1)"
grep -q '凭据文件' <<<"$who" && ok "whoami 显示凭据文件" || bad "whoami 输出异常"
who2="$(HOME="$FH" NEXUS_USER=envuser NEXUS_PASSWORD=envpass "$BIN" whoami 2>&1)"
grep -q '环境变量' <<<"$who2" && ok "环境变量优先于文件" || bad "凭据优先级不符预期"

run login --help >/dev/null 2>&1 && ok "login --help" || bad "login --help"
HOME="$FH" "$BIN" login >/dev/null 2>&1 && bad "缺 --user 应当报错" || ok "缺 --user 正确报错"
out="$(HOME="$FH" env -u NEXUS_USER -u NEXUS_PASSWORD "$BIN" login --user x </dev/null 2>&1)"
grep -q 'password-stdin' <<<"$out" && ok "非交互时提示 --password-stdin" || bad "非交互报错不明确"

# ── 7. MCP 档位 ────────────────────────────────────────────────────────────
head_ "7. MCP 档位"
mcp_tools() {
  { printf '%s\n' '{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"v","version":"0"}}}'
    printf '%s\n' '{"jsonrpc":"2.0","method":"notifications/initialized"}'
    printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
  } | HOME="$FH" timeout 40 "$BIN" mcp "$@" 2>/dev/null | jq -r 'select(.id==1)|.result.tools[].name'
}
ro="$(mcp_tools --read-only)"; def="$(mcp_tools)"; des="$(mcp_tools --allow-destructive)"
n_ro=$(wc -l <<<"$ro" | tr -d ' ')
n_def=$(wc -l <<<"$def" | tr -d ' ')
n_des=$(wc -l <<<"$des" | tr -d ' ')
[ "$n_ro" -gt 50 ] && ok "--read-only 工具数 ${n_ro}" || bad "--read-only 工具数异常: ${n_ro}"
[ "$n_def" -gt "$n_ro" ] && ok "默认档位含写操作（${n_def} > ${n_ro}）" || bad "默认档位没有增加写操作"
[ "$n_des" -gt "$n_def" ] && ok "--allow-destructive 进一步放开（${n_des}）" || bad "destructive 档位未放开"
grep -q 'delete' <<<"$def" && bad "默认档位暴露了 delete" || ok "默认档位无 delete"
grep -q 'delete' <<<"$des" && ok "destructive 档位含 delete" || bad "destructive 档位没有 delete"
grep -qx 'task_run' <<<"$def" && ok "默认档位含 task_run" || bad "默认档位缺 task_run"
grep -qx 'component_upload' <<<"$def" && ok "默认档位含 component_upload" || bad "默认档位缺 component_upload"
grep -qx 'script_run' <<<"$des" && ok "destructive 含 script_run" || bad "destructive 缺 script_run"
grep -qx 'script_run' <<<"$def" && bad "默认档位不该有 script_run" || ok "默认档位屏蔽 script_run"

# ── 8. 真实连通（可选）────────────────────────────────────────────────────
head_ "8. 真实连通（需要 NEXUS_USER / NEXUS_PASSWORD）"
if have_creds; then
  out="$(HOME="$FH" "$BIN" whoami --check 2>&1)"; rc=$?
  if [ $rc -eq 0 ]; then
    ok "凭据与连通性正常"
  elif grep -qiE '429|401|403|限流|无效' <<<"$out"; then
    skip "凭据无效或受限：$(tr -d '\n' <<<"$out" | head -c 120)"
  else
    bad "连通性检查失败：$(tr -d '\n' <<<"$out" | head -c 160)"
  fi
else
  skip "未设置凭据，跳过"
fi

# ── 9. 脚本 lint ───────────────────────────────────────────────────────────
# bash 会把多字节字符并进变量名：`$n）` 中的全角括号会成为变量名的一部分，
# 触发 unbound variable。必须写 ${n}。开发中踩过多次，固化成检查。
# 用 grep -E（macOS 的 BSD grep 不支持 -P，用 -P 会静默失效）；跳过注释行。
head_ "9. 脚本 lint"
lint_hits="$(grep -rnE '\$[A-Za-z_][A-Za-z0-9_]*[^ -~]' "$ROOT/scripts" 2>/dev/null \
  | grep -vE ':[0-9]+:[[:space:]]*#' || true)"
if [ -n "$lint_hits" ]; then
  bad "\$VAR 后紧跟非 ASCII（需写成 \${VAR}）："
  sed 's/^/      /' <<<"$lint_hits"
else
  ok "无 \$VAR + 非 ASCII 写法"
fi

printf '\n\033[1m汇总\033[0m: %d 通过, %d 失败, %d 跳过\n' "$PASS" "$FAIL" "$SKIP"
[ "$FAIL" -eq 0 ] || exit 1

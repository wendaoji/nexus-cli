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
# bad 在 GitHub Actions 里额外发一条 ::error 注解，便于不下载日志也能定位失败项
bad()  {
  printf '  \033[31m✗\033[0m %s\n' "$*"
  if [ -n "${GITHUB_ACTIONS:-}" ]; then
    printf '::error title=verify::%s\n' "$*"
  fi
  FAIL=$((FAIL+1))
  return 0
}
skip() { printf '  \033[33m-\033[0m %s\n' "$*"; SKIP=$((SKIP+1)); }
head_() { printf '\n\033[1m%s\033[0m\n' "$*"; }

have_creds() { [ -n "${NEXUS_USER:-}" ] && [ -n "${NEXUS_PASSWORD:-}" ]; }

# ── 0. 构建 ────────────────────────────────────────────────────────────────
head_ "0. 构建"
if command -v go >/dev/null 2>&1; then
  BUILD_VERSION="${VERSION:-$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)}"
  if (cd "$ROOT" && go build -trimpath -ldflags "-s -w -X main.version=$BUILD_VERSION" \
      -o "$BIN" ./cmd/nexus-cli) 2>/tmp/nx-build.err; then
    ok "go build（版本 ${BUILD_VERSION}）"
  else
    bad "go build 失败：$(head -3 /tmp/nx-build.err)"
  fi
else
  skip "没有 go 工具链，跳过构建（假设 $BIN 已存在）"
fi
[ -x "$BIN" ] || { printf '\n\033[31m没有可执行文件 %s\033[0m\n' "$BIN"; exit 1; }

# 隔离 HOME 与 XDG 目录：证明不需要任何外部配置文件。
# 注意 XDG_CONFIG_HOME 必须一起隔离 —— GitHub Actions 等环境会设置它，
# 而 config.Dir() 优先用它，只覆盖 HOME 会让测试读写不同的目录。
FH="$(mktemp -d)"
export HOME="$FH"
export XDG_CONFIG_HOME="$FH/.config"
export XDG_CACHE_HOME="$FH/.cache"
# 断言基于中文输出；语言切换单独在第 9 节验证
export NEXUS_CLI_LANG=zh
mkdir -p "$XDG_CONFIG_HOME" "$XDG_CACHE_HOME"
trap 'rm -rf "$FH"' EXIT
run() { "$BIN" "$@"; }

# ── 1. 基本命令面 ──────────────────────────────────────────────────────────
head_ "1. 基本命令"
ver_out="$(run version 2>&1)"
if grep -q "spec" <<<"$ver_out"; then ok "version 含内嵌 spec 版本"; else bad "version 未输出版本信息"; fi
run --help >/dev/null 2>&1 && ok "--help" || bad "--help"
public_help="$(run --help 2>&1)"
if grep -qiE 'restish|--rsh-' <<<"$public_help"; then
  bad "帮助中仍暴露底层命令引擎"
else
  ok "帮助未暴露底层命令引擎"
fi

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
  if grep -qi -- "$kw" <<<"$groups_out"; then bad "'$kw' 仍出现在命令面"; else ok "'$kw' 已隐藏"; fi
done

# ── 4. upload 参数整形 ─────────────────────────────────────────────────────
head_ "4. upload (dry-run)"
# 注意：不要用 `mktemp -t foo.XXXXXX.jar` —— GNU coreutils 要求模板以 X 结尾，
# macOS 的 BSD mktemp 则允许后缀。用临时目录 + 固定文件名才可移植。
tmpdir="$(mktemp -d)"
jar="$tmpdir/app.jar"; echo x > "$jar"
out="$(HOME="$FH" env -u NEXUS_USER -u NEXUS_PASSWORD "$BIN" upload --dry-run \
        --repo R --format maven --file "$jar" --group com.example --artifact app --version 1.0.0 2>&1)"
for f in maven2.groupId maven2.artifactId maven2.version maven2.asset1 maven2.asset1.extension; do
  grep -q -- "-F $f" <<<"$out" && ok "字段 $f" || bad "字段 $f 未生成"
done
grep -qE 'name=" ' <<<"$out" && bad "字段名含前导空格" || ok "字段名无前导空格"
# dry-run 不应要求凭据（上面已用 env -u 验证）
HOME="$FH" env -u NEXUS_USER -u NEXUS_PASSWORD "$BIN" upload --repo R --format raw --file "$jar" >/dev/null 2>&1 \
  && bad "真正发送时缺凭据应当报错" || ok "缺凭据时正确报错"
rm -rf "$tmpdir"

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
grep -q '/rel/docs' <<<"$out" && ok "raw 保留目录结构" || bad "raw.directory 不符预期: $out"
grep -q '.DS_Store' <<<"$out" && bad ".DS_Store 未被跳过" || ok ".DS_Store 已跳过"

out="$(run upload-dir --repo R --dir "$ud/dist" --exclude '*.txt' --dry-run 2>&1)"
grep -q 'a.txt' <<<"$out" && bad "--exclude 未生效" || ok "--exclude 生效"
rm -rf "$ud"

# ── 6. context 与凭据 ──────────────────────────────────────────────────────
head_ "6. context 与凭据"
cfgdir="$XDG_CONFIG_HOME/nexus-cli"
mkdir -p "$cfgdir"
cat > "$cfgdir/config.json" <<'JSON'
{
  "current": "prod",
  "contexts": {
    "prod": { "url": "http://prod.test:8081", "user": "produser", "password": "prodpass" },
    "dev":  { "url": "http://dev.test:8081",  "user": "devuser",  "password": "devpass"  }
  }
}
JSON
chmod 600 "$cfgdir/config.json"

out="$(run context list 2>&1)"
grep -q 'prod' <<<"$out" && grep -q 'dev' <<<"$out" && ok "context list 列出全部" || bad "context list 异常: $out"
grep -q '^\* prod' <<<"$out" && ok "context list 标记 current" || bad "current 标记缺失"

out="$(run whoami 2>&1)"
grep -q 'context: prod' <<<"$out" && ok "whoami 用 current" || bad "whoami 未用 current: $out"
grep -q '账号: prod…' <<<"$out" && ok "凭据来自 context" || bad "凭据来源不符: $out"

out="$(run --context dev whoami 2>&1)"
grep -q 'context: dev' <<<"$out" && grep -q 'devu…' <<<"$out" && ok "--context 全局参数生效" \
  || bad "--context 未生效: $out"

# 核心：给定地址时按 URL 自动匹配 context，避免拿 A 的凭据打 B
out="$(run --url http://dev.test:8081 whoami 2>&1)"
grep -q '按地址自动匹配' <<<"$out" && grep -q 'devu…' <<<"$out" && ok "按地址自动匹配 context" \
  || bad "自动匹配失败: $out"

# 地址与 context 记录不一致时必须报警并失败
run --context prod --url http://other.test:8081 whoami >/dev/null 2>&1 \
  && bad "地址不一致时应当失败" || ok "地址与 context 不一致时报错"

run context use dev >/dev/null 2>&1 && ok "context use" || bad "context use"
out="$(run whoami 2>&1)"
grep -q 'context: dev' <<<"$out" && ok "切换后生效" || bad "切换未生效: $out"
run context remove dev >/dev/null 2>&1 && ok "context remove" || bad "context remove"

run --context nope whoami >/dev/null 2>&1 \
  && bad "未知 context 应当报错" || ok "未知 context 报错"
# 指向本机未监听端口，避免这个用例去连真实 Nexus
out="$(run --context nope --url http://127.0.0.1:1 login --user x --password y 2>&1 || true)"
grep -q '不存在' <<<"$out" && bad "login 拒绝创建新 context" || ok "login 允许创建新 context"

# login 的本地行为
run login --help >/dev/null 2>&1 && ok "login --help" || bad "login --help"
run login >/dev/null 2>&1 && bad "缺 --user 应当报错" || ok "缺 --user 正确报错"
out="$(env -u NEXUS_USER -u NEXUS_PASSWORD "$BIN" login --user x </dev/null 2>&1 || true)"
grep -q 'password-stdin' <<<"$out" && ok "非交互时提示 --password-stdin" || bad "非交互报错不明确"

# 环境变量仍优先于 context
out="$(NEXUS_USER=envuser NEXUS_PASSWORD=envpass run whoami 2>&1)"
grep -q '来自环境变量' <<<"$out" && ok "环境变量优先于 context" || bad "环境变量优先级不符: $out"

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

# ── 9. 语言 ────────────────────────────────────────────────────────────────
head_ "9. 语言"
out="$(NEXUS_CLI_LANG=en "$BIN" whoami 2>&1)"
grep -q "account:" <<<"$out" && grep -q "address:" <<<"$out" && ok "英文输出" || bad "英文输出异常: $out"

out="$(LANG=zh_CN.UTF-8 env -u NEXUS_CLI_LANG -u LC_ALL -u LC_MESSAGES "$BIN" whoami 2>&1)"
grep -q "账号:" <<<"$out" && grep -q "地址:" <<<"$out" && ok "LANG=zh_CN.UTF-8 切到中文" || bad "中文输出异常: $out"

out="$(LC_ALL=zh_TW.UTF-8 env -u NEXUS_CLI_LANG "$BIN" whoami 2>&1)"
grep -q "账号:" <<<"$out" && ok "LC_ALL 优先于 LANG" || bad "LC_ALL 未生效"

out="$(NEXUS_CLI_LANG=en LANG=zh_CN.UTF-8 "$BIN" --lang zh whoami 2>&1)"
grep -q "账号:" <<<"$out" && ok "--lang 优先级最高" || bad "--lang 未生效"

out="$(NEXUS_CLI_LANG=en "$BIN" upload --repo R 2>&1)"
grep -qi "required" <<<"$out" && ok "错误信息也会翻译" || bad "错误信息未翻译: $out"

# ── 10. 脚本 lint ───────────────────────────────────────────────────────────
# bash 会把多字节字符并进变量名：`$n）` 中的全角括号会成为变量名的一部分，
# 触发 unbound variable。必须写 ${n}。开发中踩过多次，固化成检查。
# 用 grep -E（macOS 的 BSD grep 不支持 -P，用 -P 会静默失效）；跳过注释行。
head_ "10. 脚本 lint"
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

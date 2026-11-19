#!/usr/bin/env bash
# In-VM smoke test for one agent backend on Unikraft Cloud.
# Plan: docs/how-to/smoke-test-agent-backends-on-ukc.md
#
# Runs as the instance cmd. Emits one JSON line per step on stdout, so the
# operator reads results with `unikraft instances logs <name>`.
#
# Required env: SMOKE_BACKEND (claude|opencode|grok|codex|gemini)
# Optional env: SMOKE_WORK (writable dir, default /work), SMOKE_STEP_TIMEOUT (s, default 300)
# Secret env (by backend, never printed): CLAUDE_CODE_OAUTH_TOKEN, ANTHROPIC_API_KEY,
#   OPENCODE_GO_API_KEY, XAI_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY
#
# Never add `set -x` or dump the environment: secrets are in it.
# shellcheck disable=SC2317  # functions below are invoked indirectly via step()
set -uo pipefail

BACKEND="${SMOKE_BACKEND:?SMOKE_BACKEND is required}"
WORK="${SMOKE_WORK:-/work}"
STEP_TIMEOUT="${SMOKE_STEP_TIMEOUT:-300}"
SECRET_VARS=(CLAUDE_CODE_OAUTH_TOKEN ANTHROPIC_API_KEY OPENCODE_GO_API_KEY XAI_API_KEY OPENAI_API_KEY GEMINI_API_KEY)
CLONE_URL="https://github.com/octocat/Hello-World.git"
FAILS=0

export HOME="${WORK}/home"
mkdir -p "${HOME}" "${WORK}/tmp" || exit 90
export TMPDIR="${WORK}/tmp"
export GIT_TERMINAL_PROMPT=0
git config --global user.name "drover-smoke" 2>/dev/null
git config --global user.email "smoke@drover.invalid" 2>/dev/null
git config --global init.defaultBranch main 2>/dev/null

# redact: replace every secret value, then any token-shaped string.
redact() {
  local s="$1" v name
  for name in "${SECRET_VARS[@]}"; do
    v="${!name:-}"
    if [[ -n "$v" ]]; then
      s="${s//"$v"/[REDACTED:${name}]}"
    fi
  done
  printf '%s' "$s" | sed -E \
    -e 's/sk-ant-[A-Za-z0-9_-]{8,}/[REDACTED:sk-ant]/g' \
    -e 's/sk-[A-Za-z0-9_-]{16,}/[REDACTED:sk]/g' \
    -e 's/xai-[A-Za-z0-9_-]{16,}/[REDACTED:xai]/g' \
    -e 's/AIza[A-Za-z0-9_-]{20,}/[REDACTED:google]/g' \
    -e 's/(Bearer|bearer) [A-Za-z0-9._~+\/=-]{12,}/\1 [REDACTED]/g' \
    -e 's/"(access_token|refresh_token|id_token|api_key|apiKey)" *: *"[^"]*"/"\1":"[REDACTED]"/g'
}

json_str() { printf '%s' "$1" | jq -Rs .; }

# step NAME CMD... : run with timeout, emit one JSON line, keep the last 2 KiB of output.
step() {
  local name="$1"; shift
  local out err rc t0 t1
  out="$(mktemp)"; err="$(mktemp)"
  t0="$(date +%s%3N)"
  # bash -c so exported shell functions (agent_prompt, version_cmd) work under timeout.
  timeout "${STEP_TIMEOUT}" bash -c '"$@"' _ "$@" >"$out" 2>"$err"
  rc=$?
  t1="$(date +%s%3N)"
  [[ $rc -ne 0 ]] && FAILS=$((FAILS + 1))
  printf '{"backend":%s,"step":%s,"rc":%d,"ms":%d,"stdout":%s,"stderr":%s}\n' \
    "$(json_str "$BACKEND")" "$(json_str "$name")" "$rc" "$((t1 - t0))" \
    "$(json_str "$(redact "$(tail -c 2048 "$out")")")" \
    "$(json_str "$(redact "$(tail -c 2048 "$err")")")"
  cp "$out" "${WORK}/last-${name}.out" 2>/dev/null
  rm -f "$out" "$err"
  return $rc
}

note() { printf '{"backend":%s,"note":%s,"value":%s}\n' "$(json_str "$BACKEND")" "$(json_str "$1")" "$(json_str "$(redact "$2")")"; }

hosts_for() {
  case "$1" in
    claude)   echo "api.anthropic.com claude.ai platform.claude.com" ;;
    opencode) echo "opencode.ai" ;;
    grok)     echo "api.x.ai auth.x.ai cli-chat-proxy.grok.com" ;;
    codex)    echo "api.openai.com chatgpt.com" ;;
    gemini)   echo "generativelanguage.googleapis.com" ;;
  esac
}

PROMPT_SPAWN='Use your shell tool to run exactly: uname -a && git --version . Reply with the raw command output only.'
PROMPT_E2E="In the current git repository, append the line 'smoke-ok ${BACKEND}' to SMOKE.md, then run: git add SMOKE.md && git commit -m 'smoke: ${BACKEND}' . Do not push. Reply DONE when the commit exists."

agent_prompt() { # agent_prompt PROMPT -> runs the backend headless in $PWD
  case "$BACKEND" in
    claude)   claude -p "$1" --output-format json --allowedTools "Bash" "Edit" "Write" "Read" ;;
    opencode) opencode run --model "${OPENCODE_MODEL:-opencode-go/kimi-k3}" "$1" ;;
    grok)     grok -p "$1" --output-format json --always-approve ;;
    codex)    codex exec --json --skip-git-repo-check --dangerously-bypass-approvals-and-sandbox "$1" ;;
    gemini)   gemini -p "$1" --output-format json --yolo ;;
  esac
}

version_cmd() {
  case "$BACKEND" in
    claude) claude --version ;; opencode) opencode --version ;; grok) grok --version ;;
    codex) codex --version ;; gemini) gemini --version ;;
  esac
}

export BACKEND
export -f agent_prompt version_cmd

# ---- 0. environment facts -------------------------------------------------
note boot_uptime "$(cut -d' ' -f1 /proc/uptime 2>/dev/null || echo n/a)"
note uname "$(uname -a 2>&1)"
note vm_epoch "$(date -u +%s)"
note resolv_conf "$(tr '\n' ';' </etc/resolv.conf 2>&1)"
# HTTP (not HTTPS) Date header, so a bad clock cannot hide behind a TLS failure.
http_date="$(curl -sI --max-time 10 http://example.com/ 2>/dev/null | tr -d '\r' | sed -n 's/^[Dd]ate: //p')"
note http_date "${http_date:-unavailable}"
if [[ -n "$http_date" ]]; then
  note clock_delta_s "$(( $(date -u +%s) - $(date -u -d "$http_date" +%s) ))"
fi
for h in $(hosts_for "$BACKEND") github.com; do
  note "dns_${h}" "$(getent hosts "$h" 2>&1 | head -2 | tr '\n' ';')"
done

# ---- 1. binary ------------------------------------------------------------
step version version_cmd

# ---- 2. spawn ---------------------------------------------------------------
step spawn_sh sh -c 'echo ok'
step spawn_nested sh -c 'sh -c "bash -c \"echo nested-ok\""'
step spawn_pipeline bash -c 'printf "a\nb\n" | sort -r | head -1'
step spawn_bg bash -c 'sleep 1 & wait $! && echo bg-ok'

# ---- 3. git -----------------------------------------------------------------
rm -rf "${WORK}/git-local" "${WORK}/git-clone" "${WORK}/e2e"
step git_local bash -c "git init -q '${WORK}/git-local' && cd '${WORK}/git-local' && echo hi > a.txt && git add a.txt && git commit -qm init && git log --oneline | wc -l"
step git_clone_https git clone -q --depth 1 "$CLONE_URL" "${WORK}/git-clone"

# ---- 4. TLS -------------------------------------------------------------------
for h in $(hosts_for "$BACKEND") github.com; do
  step "tls_openssl_${h}" bash -c "o=\$(openssl s_client -connect '${h}:443' -servername '${h}' -verify_return_error </dev/null 2>&1); printf '%s\n' \"\$o\" | grep -E 'Verify return code|issuer=' | head -3; printf '%s\n' \"\$o\" | grep -q 'Verify return code: 0 (ok)'"
  step "tls_curl_${h}" curl -sS -o /dev/null --max-time 15 -w '%{http_code} verify=%{ssl_verify_result}\n' "https://${h}/"
done
# CA-bundle behaviour: system bundle only, then an empty bundle (expected to fail).
step tls_ca_system env SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt curl -sS -o /dev/null -w 'verify=%{ssl_verify_result}\n' https://github.com/
# shellcheck disable=SC2016  # TMPDIR expands in the child shell
step tls_ca_empty_expect_fail bash -c ': > "${TMPDIR}/empty.pem"; ! curl -sS --cacert "${TMPDIR}/empty.pem" -o /dev/null https://github.com/'
note node_extra_ca_certs "${NODE_EXTRA_CA_CERTS:-unset}"
note ssl_cert_file "${SSL_CERT_FILE:-unset}"

# ---- 4b. backend auth bootstrap (API key paths only; nothing is printed) -------
if [[ "$BACKEND" == codex && -n "${OPENAI_API_KEY:-}" && ! -s "${CODEX_HOME:-$HOME/.codex}/auth.json" ]]; then
  mkdir -p "${CODEX_HOME:-$HOME/.codex}"
  # shellcheck disable=SC2016  # expands in the child shell
  step codex_login_api_key bash -c 'printenv OPENAI_API_KEY | codex login --with-api-key'
fi

# ---- 5. agent spawn via its own shell tool ---------------------------------------
mkdir -p "${WORK}/spawn" && cd "${WORK}/spawn" || exit 91
step agent_spawn agent_prompt "$PROMPT_SPAWN"

# ---- 6. headless end-to-end: edit + commit --------------------------------------
git init -q "${WORK}/e2e" && cd "${WORK}/e2e" && echo "# smoke" > SMOKE.md && git add SMOKE.md && git commit -qm base
step agent_e2e agent_prompt "$PROMPT_E2E"
step e2e_verify bash -c "test \"\$(git -C '${WORK}/e2e' log -1 --format=%s)\" = 'smoke: ${BACKEND}' && grep -qx 'smoke-ok ${BACKEND}' '${WORK}/e2e/SMOKE.md' && git -C '${WORK}/e2e' rev-list --count HEAD"

note summary_fails "$FAILS"
echo "SMOKE_DONE backend=${BACKEND} fails=${FAILS}"
# Keep the instance up briefly so logs can be read, then exit.
sleep "${SMOKE_LINGER_S:-30}"
exit 0

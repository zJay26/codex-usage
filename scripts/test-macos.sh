#!/usr/bin/env bash
set -euo pipefail

# Native CI smoke test. The runner is disposable; all accounting files are
# synthetic and isolated from the default user state.
test_root="$(mktemp -d)"
export CODEX_USAGE_HOME="$test_root/state"
export CODEX_HOME="$test_root/codex"
export SSH_CONNECTION=ci-smoke-test
mkdir -p "$CODEX_USAGE_HOME" "$CODEX_HOME/sessions"
binary="$test_root/codex-usage"
go build -o "$binary" ./cmd/codex-usage
cleanup() { "$binary" uninstall >/dev/null 2>&1 || true; }
trap cleanup EXIT
printf '%s\n' '{"port":43279,"scan_interval_seconds":600}' > "$CODEX_USAGE_HOME/config.json"
printf '%s\n' '{"auto_check":false}' > "$CODEX_USAGE_HOME/.codex-usage-updates.json"
"$binary" install --skip-scan
curl --fail --silent http://127.0.0.1:43279/healthz
/bin/launchctl print "gui/$(id -u)/com.zjay.codex-usage" >/dev/null
test -s "$CODEX_USAGE_HOME/usage.sqlite"
"$binary" uninstall
test -s "$CODEX_USAGE_HOME/usage.sqlite"
if curl --fail --silent http://127.0.0.1:43279/healthz; then exit 1; fi
"$binary" install --skip-scan
curl --fail --silent http://127.0.0.1:43279/healthz
"$binary" uninstall
test -s "$CODEX_USAGE_HOME/usage.sqlite"
trap - EXIT

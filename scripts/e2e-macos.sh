#!/usr/bin/env bash
# macOS End-to-End Test Suite for rho
# Run: bash scripts/e2e-macos.sh
set -euo pipefail

PASS=0
FAIL=0

pass() { PASS=$((PASS+1)); echo "  PASS: $1"; }
fail() { FAIL=$((FAIL+1)); echo "  FAIL: $1"; }

# Ensure binary exists
if [ ! -f ./rho ]; then
    echo "Building rho..."
    go build -o rho ./cmd/rho
fi

echo "=== macOS E2E Tests ==="
echo

# 1. Version check
echo "--- version ---"
./rho version 2>&1 | grep -q "[0-9]\+\.[0-9]\+\.[0-9]\+" && pass "version output" || fail "version output"

# 2. Doctor / preflight
echo "--- doctor ---"
./rho doctor 2>&1 | head -20 | grep -qi "rho\|ok\|check" && pass "doctor runs" || fail "doctor failed"

# 3. Preflight
echo "--- preflight ---"
./rho preflight 2>&1 | head -20 && pass "preflight runs" || fail "preflight failed"

# 4. No API keys in provider.json
echo "--- provider.json has no plaintext API keys ---"
if [ -f ~/.rho/provider.json ]; then
    if grep -qi '"api_key"' ~/.rho/provider.json 2>/dev/null; then
        fail "provider.json contains api_key field"
    else
        pass "provider.json has no plaintext API keys"
    fi
else
    pass "provider.json not present (no setup done)"
fi

# 5. Credential store (macOS Keychain)
echo "--- credentials list ---"
./rho credentials list 2>&1 | head -10 && pass "credentials list runs" || fail "credentials list failed"

# 6. Shell completions generate
echo "--- shell completions ---"
./rho completion bash 2>&1 | head -5 | grep -q "complete\|completion" && pass "bash completions generated" || fail "bash completions"
./rho completion zsh 2>&1 | head -5 | grep -q "compdef\|completion" && pass "zsh completions generated" || fail "zsh completions"

# 7. Help output
echo "--- help ---"
./rho --help 2>&1 | head -5 | grep -qi "rho\|usage\|flag" && pass "help output" || fail "help output"

# 9. Config subcommands
echo "--- config commands ---"
./rho config --help 2>&1 | head -5 | grep -qi "manage\|config\|edit" && pass "config help" || fail "config help"

# 10. Session list (no-op test)
echo "--- sessions list ---"
./rho sessions list 2>&1 | head -5 && pass "sessions list runs" || fail "sessions list"

echo
echo "=== Results: $PASS passed, $FAIL failed ==="
if [ "$FAIL" -gt 0 ]; then
    exit 1
fi

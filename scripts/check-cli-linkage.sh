#!/usr/bin/env bash
# Guard: orole-cli is a thin IPC client and must not link the daemon or its heavy
# dependencies (sqlite, sftp/ssh, MCP SDK). See issue #152.
set -euo pipefail
cd "$(dirname "$0")/.."
bad=$(go list -deps ./cmd/orole-cli | grep -E 'modernc\.org/sqlite|github\.com/pkg/sftp|golang\.org/x/crypto/ssh|modelcontextprotocol/go-sdk|orole-os-desktop/internal/daemon$' || true)
if [ -n "$bad" ]; then
  echo "orole-cli links packages it must not:" >&2
  echo "$bad" >&2
  exit 1
fi
echo "orole-cli linkage ok"

## What and why

Closes #

## Checklist

- [ ] `go vet ./... && go test -race ./...` pass
- [ ] `bun run --cwd frontend check` passes
- [ ] Bound service or event changed: `go tool mygo generate` run and `frontend/src/mygo.ts` committed
- [ ] New migration is forward-only and numbered (`migrations/NNNN_name.sql`)
- [ ] No secrets in SQLite, logs or the frontend (credentials go through `internal/credentials`)
- [ ] Permissions for sensitive actions are decided in Go, not only in the UI
- [ ] No mock data in production paths; unconfigured integrations show a setup state
- [ ] Docs updated (and `docs/research/upstream-attribution.md` if upstream code was copied)

## Evidence

<!-- Command output, screenshots, or "not run: reason". -->

# Requires GNU make (e.g. via Git for Windows / scoop). Every target is a thin wrapper over the
# documented go / bun commands in docs/development/setup.md; nothing here is required.
GO ?= go

.PHONY: dev web daemon test test-go test-web lint build installer clean-dev
dev:        ## launch the desktop app with HMR (starts Vite itself)
	$(GO) tool mygo dev
web:        ## frontend only, in a browser (preview mode, no daemon)
	bun run --cwd frontend dev
daemon:     ## run the daemon in the foreground with stderr logging
	$(GO) run ./cmd/daemon -stderr -v
test: test-go test-web
test-go:
	$(GO) fmt ./... && $(GO) vet ./... && $(GO) test -race ./...
test-web:
	bun run --cwd frontend check
lint:
	bun run --cwd frontend lint
build:
	$(GO) tool mygo build
installer: build
clean-dev:  ## deletes ONLY the isolated dev data dir .orole-dev (never %LOCALAPPDATA%\Orole)
	rm -rf .orole-dev

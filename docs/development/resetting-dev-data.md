# Resetting development data

**Never delete `%LOCALAPPDATA%\Orole` to reset development.** That is the real per-user data directory
(database, logs). Develop against an isolated directory instead:

```powershell
$env:OROLE_HOME = "$PWD\.orole-dev"     # git-ignored
go tool mygo dev
```

Everything (database, token, logs) then lives in `.orole-dev`, and the daemon's pipe name differs from the real
instance's (it embeds a hash of the data dir), so a dev daemon and your installed daemon can run side by side.

Reset:

```powershell
build\orole.exe daemon stop           # or go run ./cmd/orole-cli daemon stop, with the same OROLE_HOME
Remove-Item -Recurse -Force .orole-dev
```

`make clean-dev` does the same removal. Stop the daemon first: it holds the SQLite files open and removing them
under a running daemon fails or corrupts the run.

Credentials are **not** in the data dir. Entries created by development (for example `Orole OS/github/token`)
stay in Windows Credential Manager (Control Panel > Credential Manager > Windows Credentials) until removed there.
Settings > Diagnostics uses only a throwaway `diagnostics/probe` entry that it deletes.

A removed Task Scheduler registration: `orole daemon uninstall` (or `schtasks /Delete /TN "Orole OS Daemon" /F`).

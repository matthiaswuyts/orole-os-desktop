# Resetting development data

**Never delete `%LOCALAPPDATA%\Orole` to reset development.** That is the real per-user data directory
(database, logs). Develop against an isolated directory instead:

```powershell
.\start.ps1                              # data in profiles\dev (git-ignored)
.\start.ps1 -DataDir alt                 # a second profile in profiles\alt
```

A relative `-DataDir` always lands under the repository's `profiles\` folder, whichever directory the shell started
in; an absolute path is used as is. The manual equivalent:

```powershell
$env:OROLE_HOME = "$PWD\profiles\dev"
go tool mygo dev
```

Everything (database, token, logs) then lives in that profile, and the daemon's pipe name differs from the real
instance's (it embeds a hash of the data dir), so a dev daemon and your installed daemon can run side by side.

Reset:

```powershell
build\orole.exe daemon stop           # or go run ./cmd/orole-cli daemon stop, with the same OROLE_HOME
Remove-Item -Recurse -Force profiles\dev
```

`make clean-dev` does the same removal. Stop the daemon first: it holds the SQLite files open and removing them
under a running daemon fails or corrupts the run.

Credentials are **not** in the data dir. Entries created by development (for example `Orole OS/github/token`)
stay in Windows Credential Manager (Control Panel > Credential Manager > Windows Credentials) until removed there.
Settings > Diagnostics uses only a throwaway `diagnostics/probe` entry that it deletes.

A removed Task Scheduler registration: `orole daemon uninstall` (or `schtasks /Delete /TN "Orole OS Daemon" /F`).

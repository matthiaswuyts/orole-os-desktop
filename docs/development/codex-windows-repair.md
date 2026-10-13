# Windows Codex installation repair

Settings and the Codex agent adapter now resolve through the same CLI runtime manager.
Windows managed installations use the official `@openai/codex` platform package
(`VERSION-win32-x64` or `VERSION-win32-arm64`), verified with the registry's SHA-512
integrity hash. Preserve the vendor layout: `bin/codex.exe`, its adjacent
`codex-code-mode-host.exe`, and the sibling `codex-resources` and `codex-path` trees.
The standalone GitHub executable archive does not provide this complete runtime.

An older executable-only installation is incomplete. Download Codex again from
Settings to install the complete package, including when its version is unchanged.
The user's CLI authentication is preserved; this does not read or copy credentials.

Verify sign-in and terminal execution separately from Windows sandbox setup.
Successful `--version` or `login status` does not prove local tools can run.
On the inspected machine, restored helpers allowed a harmless terminal smoke command
to succeed with unrestricted execution, while read-only sandbox initialization still
reported `helper_unknown_error: setup refresh had errors`. Do not disable sandbox
protections as an installation repair.

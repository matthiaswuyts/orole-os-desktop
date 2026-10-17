; Wraps the installer MyGo builds so that it can run while Orole OS is open.
; MyGo's own installer cannot replace "Orole OS.exe" while it runs ("Error opening file for writing").
; This stub closes the app and its daemon that run from the install folder, then runs the real
; installer with the same arguments (/S, /D=<dir>). Built by scripts/wrap-installer.ps1:
;   makensis /DINNER=<mygo installer> /DOUT=<result> /DCLOSE=<scripts\close-orole.ps1> installer-wrapper.nsi
Unicode true
SilentInstall silent
RequestExecutionLevel user
SetCompress off
Name "Orole OS Setup"
OutFile "${OUT}"
InstallDir "$LOCALAPPDATA\Programs\Orole OS"
!include "FileFunc.nsh"
!insertmacro GetParameters

Section
  InitPluginsDir
  ${GetParameters} $R0

  ; NSIS itself consumes /D=<dir> (the last argument, unquoted) into $INSTDIR and removes it from the command
  ; line, so it is passed on explicitly. $INSTDIR is the folder being replaced, the default one without /D.
  StrCpy $R2 ""
  StrCmp $INSTDIR "$LOCALAPPDATA\Programs\Orole OS" +2
  StrCpy $R2 " /D=$INSTDIR"

  File "/oname=$PLUGINSDIR\close-orole.ps1" "${CLOSE}"
  nsExec::Exec 'powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\close-orole.ps1" -Dir "$INSTDIR"'
  Pop $0

  File "/oname=$PLUGINSDIR\setup.exe" "${INNER}"
  ExecWait '"$PLUGINSDIR\setup.exe" $R0$R2' $0
  SetErrorLevel $0
SectionEnd

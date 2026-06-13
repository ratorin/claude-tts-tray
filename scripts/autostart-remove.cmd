@echo off
rem Claude TTS Tray + VOICEVOX engine - disable autostart at logon.
reg delete "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v ClaudeTTSTray /f
reg delete "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v VoicevoxEngine /f
echo.
echo Autostart removed (both). Running processes are not closed; quit the tray from its menu,
echo and stop the VOICEVOX engine via Task Manager (run.exe) if needed.
pause

@echo off
rem Claude TTS Tray + VOICEVOX engine - enable autostart at logon (HKCU Run) and launch now.
set "EXE=C:\xampp\Project\claude-tts-tray\claude-tts-tray.exe"
set "VVVBS=C:\xampp\Project\voicevox-engine\start-voicevox-hidden.vbs"

rem 1) VOICEVOX engine (hidden console via wscript)
if exist "%VVVBS%" (
  reg add "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v VoicevoxEngine /t REG_SZ /d "wscript \"%VVVBS%\"" /f
  echo Launching VOICEVOX engine now...
  wscript "%VVVBS%"
) else (
  echo [skip] VOICEVOX engine launcher not found: %VVVBS%
)

rem 2) Claude TTS Tray
reg add "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v ClaudeTTSTray /t REG_SZ /d "\"%EXE%\"" /f
echo Launching ClaudeTTSTray now...
start "" "%EXE%"

echo.
echo Autostart enabled (VOICEVOX engine + Claude TTS Tray).
pause

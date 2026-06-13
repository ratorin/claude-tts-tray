#!/usr/bin/env sh
# claude-tts-tray (Linux) を削除する。
pkill -f "$HOME/.local/bin/claude-tts-tray" 2>/dev/null || true
rm -f "$HOME/.config/autostart/claude-tts-tray.desktop"
rm -f "$HOME/.local/bin/claude-tts-tray"
echo "削除しました(自動起動・バイナリ)。"

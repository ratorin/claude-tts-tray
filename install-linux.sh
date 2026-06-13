#!/usr/bin/env sh
# claude-tts-tray を Linux デスクトップにインストールする。
# - バイナリを ~/.local/bin に配置
# - XDG autostart (~/.config/autostart) に登録(ログイン時に自動起動)
set -e
HERE="$(cd "$(dirname "$0")" && pwd)"

BIN="$HERE/dist/claude-tts-tray-linux-amd64"
[ -f "$BIN" ] || BIN="$HERE/claude-tts-tray"
if [ ! -f "$BIN" ]; then
	echo "バイナリが見つかりません。先に ./build-linux.sh を実行するか、バイナリを置いてください。" >&2
	exit 1
fi

mkdir -p "$HOME/.local/bin" "$HOME/.config/autostart"
install -m 0755 "$BIN" "$HOME/.local/bin/claude-tts-tray"

cat > "$HOME/.config/autostart/claude-tts-tray.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=Claude TTS Tray
Comment=Claude Code の応答を読み上げる常駐トレイ
Exec=$HOME/.local/bin/claude-tts-tray
X-GNOME-Autostart-enabled=true
NoDisplay=true
EOF

echo "インストール完了:"
echo "  バイナリ : $HOME/.local/bin/claude-tts-tray"
echo "  自動起動 : $HOME/.config/autostart/claude-tts-tray.desktop"
echo
echo "[次の手順]"
echo "1) 音声プレイヤーを用意 (いずれか): paplay / ffplay / aplay"
echo "     例: sudo apt install pulseaudio-utils   (paplay)"
echo "         sudo apt install ffmpeg             (ffplay)"
echo "2) 合成サーバーを設定: ~/.claude/tts-config.json の \"server\" を"
echo "   到達可能な VOICEVOX(:50021) / AivisSpeech(:10101) に。"
echo "3) Claude Code のフック(~/.claude/settings.json)に追加:"
echo "     Stop         : \"$HOME/.local/bin/claude-tts-tray hook stop\""
echo "     Notification : \"$HOME/.local/bin/claude-tts-tray hook notify\""
echo "4) 今すぐ起動: \"$HOME/.local/bin/claude-tts-tray\" &"
echo "   (デスクトップにトレイ=StatusNotifierItem ホストが必要。GNOMEは拡張が要る場合あり)"

#!/usr/bin/env sh
# claude-tts-tray を Linux デスクトップにインストールする(バイナリを ~/.local/bin へ配置)。
# 自動起動はインストール後、トレイメニューの「ログイン時に自動起動」で登録する。
set -e
HERE="$(cd "$(dirname "$0")" && pwd)"

BIN="$HERE/../dist/claude-tts-tray-linux-amd64"
[ -f "$BIN" ] || BIN="$HERE/../claude-tts-tray"
if [ ! -f "$BIN" ]; then
	echo "バイナリが見つかりません。先に ./scripts/build-linux.sh を実行するか、バイナリを置いてください。" >&2
	exit 1
fi

mkdir -p "$HOME/.local/bin"
install -m 0755 "$BIN" "$HOME/.local/bin/claude-tts-tray"
echo "インストール完了: $HOME/.local/bin/claude-tts-tray"
echo
echo "[次の手順]"
echo "1) 音声プレイヤーを用意 (いずれか): paplay / ffplay / aplay"
echo "     例: sudo apt install pulseaudio-utils   (paplay)"
echo "         sudo apt install ffmpeg             (ffplay)"
echo "2) 今すぐ起動: \"$HOME/.local/bin/claude-tts-tray\" &"
echo "   (デスクトップにトレイ=StatusNotifierItem ホストが必要。GNOMEは拡張が要る場合あり)"
echo "3) 自動起動: トレイメニュー「ログイン時に自動起動」をオン (XDG autostart に登録)"
echo "4) 読み上げ(音声)にする場合: トレイ「サーバーを追加・編集…」で VOICEVOX/AivisSpeech を接続"
echo "   Claude Code のフック(~/.claude/settings.json)に以下を追加:"
echo "     Stop         : \"$HOME/.local/bin/claude-tts-tray hook stop\""
echo "     Notification : \"$HOME/.local/bin/claude-tts-tray hook notify\""

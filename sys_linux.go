//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// --- 外部プレイヤーによる WAV 再生 (Linux/Unix) --------------------------
// winmm の代わりに paplay / aplay / ffplay へシェルアウトする。
// 再生中プロセスを保持し、stopSound() で kill して即停止する。
var (
	playProcMu sync.Mutex
	playProc   *exec.Cmd
)

// audioPlayer は利用可能な再生コマンドと固定引数を返す。
func audioPlayer() (string, []string) {
	if p, err := exec.LookPath("paplay"); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("ffplay"); err == nil {
		return p, []string{"-nodisp", "-autoexit", "-loglevel", "quiet"}
	}
	if p, err := exec.LookPath("aplay"); err == nil {
		return p, []string{"-q"}
	}
	return "", nil
}

// playSoundFile は音声ファイルを同期再生する(再生終了までブロック)。
func playSoundFile(path string) {
	player, args := audioPlayer()
	if player == "" {
		logLine("no audio player found (paplay/ffplay/aplay)")
		return
	}
	cmd := exec.Command(player, append(append([]string{}, args...), path)...)
	playProcMu.Lock()
	playProc = cmd
	playProcMu.Unlock()
	_ = cmd.Run() // 終了 or stopSound() による kill までブロック
}

// playSystemAlias は確認チャイムとしてシステム効果音を鳴らす(alias引数はLinuxでは無視)。
func playSystemAlias(alias string) {
	for _, f := range []string{
		"/usr/share/sounds/freedesktop/stereo/complete.oga",
		"/usr/share/sounds/freedesktop/stereo/bell.oga",
		"/usr/share/sounds/freedesktop/stereo/message.oga",
	} {
		if _, err := os.Stat(f); err == nil {
			playSoundFile(f)
			return
		}
	}
	logLine("no system sound found for chime")
}

// stopSound は再生中のプロセスを kill して即停止する。
func stopSound() {
	playProcMu.Lock()
	defer playProcMu.Unlock()
	if playProc != nil && playProc.Process != nil {
		_ = playProc.Process.Kill()
	}
}

// openBrowser は既定のブラウザでURLを開く。
func openBrowser(url string) {
	if err := exec.Command("xdg-open", url).Start(); err != nil {
		logLine("openBrowser failed: " + err.Error())
	}
}

// --- スタートアップ(ログイン時自動起動): XDG autostart 方式 ---------------
// ~/.config/autostart/claude-tts-tray.desktop を置く/消すだけ。

func autostartDesktopPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".config", "autostart", "claude-tts-tray.desktop")
}

// autostartEnabled は autostart の .desktop があるか。
func autostartEnabled() bool {
	_, err := os.Stat(autostartDesktopPath())
	return err == nil
}

// setAutostart は autostart の .desktop を作成/削除する。
func setAutostart(on bool) error {
	p := autostartDesktopPath()
	if !on {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	content := "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=Claude TTS Tray\n" +
		"Exec=" + exe + "\n" +
		"X-GNOME-Autostart-enabled=true\n" +
		"NoDisplay=true\n"
	return os.WriteFile(p, []byte(content), 0o644)
}

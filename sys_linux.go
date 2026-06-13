//go:build !windows

package main

import (
	"os"
	"os/exec"
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

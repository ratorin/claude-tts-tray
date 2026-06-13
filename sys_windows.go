//go:build windows

package main

import (
	"os/exec"
	"syscall"
	"unsafe"
)

// --- winmm.dll による WAV 再生 (Windows) ---------------------------------
var (
	winmm         = syscall.NewLazyDLL("winmm.dll")
	procPlaySound = winmm.NewProc("PlaySoundW")
)

const (
	sndSync      = 0x0000
	sndNodefault = 0x0002
	sndAlias     = 0x00010000
	sndFilename  = 0x00020000
)

// playSoundFile はWAVファイルを同期再生する(再生終了までブロック)。
func playSoundFile(path string) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	procPlaySound.Call(uintptr(unsafe.Pointer(p)), 0, uintptr(sndFilename|sndSync|sndNodefault))
}

// playSystemAlias はWindowsのシステム音(例 SystemAsterisk)を再生する。
func playSystemAlias(alias string) {
	p, err := syscall.UTF16PtrFromString(alias)
	if err != nil {
		return
	}
	procPlaySound.Call(uintptr(unsafe.Pointer(p)), 0, uintptr(sndAlias|sndSync|sndNodefault))
}

// stopSound は再生中の音を即座に停止する(PlaySound(NULL))。
func stopSound() {
	procPlaySound.Call(0, 0, 0)
}

// openBrowser は既定のブラウザでURLを開く。
func openBrowser(url string) {
	if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start(); err != nil {
		logLine("openBrowser failed: " + err.Error())
	}
}

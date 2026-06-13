//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
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

// --- スタートアップ(ログイン時自動起動): .lnk ショートカット方式 ----------
// レジストリを使わず、スタートアップフォルダに .lnk を置く/消すだけ。

func startupLnkPath() string {
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		home, _ := os.UserHomeDir()
		appdata = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(appdata, "Microsoft", "Windows", "Start Menu", "Programs", "Startup", "Claude TTS Tray.lnk")
}

// autostartEnabled はスタートアップにショートカットがあるか。
func autostartEnabled() bool {
	_, err := os.Stat(startupLnkPath())
	return err == nil
}

// setAutostart はスタートアップのショートカットを作成/削除する。
func setAutostart(on bool) error {
	lnk := startupLnkPath()
	if !on {
		if err := os.Remove(lnk); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(lnk), 0o755); err != nil {
		return err
	}
	return createShortcut(lnk, exe, filepath.Dir(exe))
}

var (
	modOle32             = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = modOle32.NewProc("CoCreateInstance")
)

// comMethod は COM インターフェイスの vtable から index 番目のメソッドアドレスを返す。
// obj は COM オブジェクトへの unsafe.Pointer。先頭が vtable ポインタ。
func comMethod(obj unsafe.Pointer, index int) uintptr {
	vtable := *(**[64]uintptr)(obj)
	return vtable[index]
}

// createShortcut は IShellLinkW(COM)で .lnk を作成する。
func createShortcut(lnkPath, target, workdir string) error {
	runtime.LockOSThread() // COMはスレッド親和性があるので固定
	defer runtime.UnlockOSThread()

	clsidShellLink, err := windows.GUIDFromString("{00021401-0000-0000-C000-000000000046}")
	if err != nil {
		return err
	}
	iidShellLinkW, err := windows.GUIDFromString("{000214F9-0000-0000-C000-000000000046}")
	if err != nil {
		return err
	}
	iidPersistFile, err := windows.GUIDFromString("{0000010B-0000-0000-C000-000000000046}")
	if err != nil {
		return err
	}

	if cierr := windows.CoInitializeEx(0, 2 /* APARTMENTTHREADED */); cierr == nil {
		defer windows.CoUninitialize()
	}

	var psl unsafe.Pointer
	r0, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidShellLink)), 0,
		uintptr(windows.CLSCTX_INPROC_SERVER),
		uintptr(unsafe.Pointer(&iidShellLinkW)),
		uintptr(unsafe.Pointer(&psl)),
	)
	if r0 != 0 || psl == nil {
		return syscall.Errno(r0)
	}
	defer syscall.SyscallN(comMethod(psl, 2), uintptr(psl)) // IUnknown::Release

	tp, _ := windows.UTF16PtrFromString(target)
	if r, _, _ := syscall.SyscallN(comMethod(psl, 20), uintptr(psl), uintptr(unsafe.Pointer(tp))); r != 0 { // SetPath
		return syscall.Errno(r)
	}
	if workdir != "" {
		if wp, e := windows.UTF16PtrFromString(workdir); e == nil {
			syscall.SyscallN(comMethod(psl, 9), uintptr(psl), uintptr(unsafe.Pointer(wp))) // SetWorkingDirectory
		}
	}

	var ppf unsafe.Pointer
	if r, _, _ := syscall.SyscallN(comMethod(psl, 0), uintptr(psl), uintptr(unsafe.Pointer(&iidPersistFile)), uintptr(unsafe.Pointer(&ppf))); r != 0 || ppf == nil { // QueryInterface
		return syscall.Errno(r)
	}
	defer syscall.SyscallN(comMethod(ppf, 2), uintptr(ppf)) // Release

	lp, _ := windows.UTF16PtrFromString(lnkPath)
	if r, _, _ := syscall.SyscallN(comMethod(ppf, 6), uintptr(ppf), uintptr(unsafe.Pointer(lp)), 1); r != 0 { // IPersistFile::Save(path, TRUE)
		return syscall.Errno(r)
	}
	return nil
}

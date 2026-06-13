package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

var logMu sync.Mutex

// logPath はログファイルの場所。
func logPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".claude", "tts-tray.log")
}

// logLine は1行ログを追記する(失敗しても無視)。
func logLine(msg string) {
	logMu.Lock()
	defer logMu.Unlock()
	f, err := os.OpenFile(logPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s [tts-tray] %s\n", time.Now().Format("2006-01-02T15:04:05"), msg)
}

// itoa は int を10進文字列に変換する。
func itoa(n int) string {
	return strconv.Itoa(n)
}

// boolStr は bool を "true"/"false" にする。
func boolStr(b bool) string {
	return strconv.FormatBool(b)
}

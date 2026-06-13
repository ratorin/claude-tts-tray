//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// createShortcut が有効な .lnk を作れるか検証する。
func TestCreateShortcut(t *testing.T) {
	dir := t.TempDir()
	lnk := filepath.Join(dir, "test.lnk")
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("Executable: %v", err)
	}
	if err := createShortcut(lnk, exe, filepath.Dir(exe)); err != nil {
		t.Fatalf("createShortcut: %v", err)
	}
	b, err := os.ReadFile(lnk)
	if err != nil {
		t.Fatalf("read lnk: %v", err)
	}
	// ShellLinkHeader: HeaderSize = 0x0000004C
	if len(b) < 4 || b[0] != 0x4C || b[1] != 0x00 || b[2] != 0x00 || b[3] != 0x00 {
		t.Fatalf("invalid .lnk header: % x", b[:minInt(8, len(b))])
	}
	t.Logf(".lnk OK: %d bytes, target=%s", len(b), exe)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// setAutostart の作成→確認→削除の往復(実スタートアップフォルダ)。
func TestSetAutostartRoundtrip(t *testing.T) {
	if autostartEnabled() {
		t.Skip("既に自動起動が有効。テストをスキップ(消さないため)")
	}
	if err := setAutostart(true); err != nil {
		t.Fatalf("setAutostart(true): %v", err)
	}
	if !autostartEnabled() {
		t.Fatalf("有効化後に enabled=false")
	}
	t.Logf("作成OK: %s", startupLnkPath())
	if err := setAutostart(false); err != nil {
		t.Fatalf("setAutostart(false): %v", err)
	}
	if autostartEnabled() {
		t.Fatalf("無効化後も enabled=true (残骸)")
	}
	t.Log("削除OK(往復成功)")
}

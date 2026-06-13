package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

// runHook は極薄のフッククライアント。標準入力(フックのJSONペイロード)を
// 常駐デーモンへ転送するだけ。シェル非依存・高速起動・常に exit 0。
//
// 使い方(settings.json のフック):
//   claude-tts-tray.exe hook stop     ... Stop時
//   claude-tts-tray.exe hook notify   ... Notification(確認)時
func runHook(mode string) {
	switch mode {
	case "stop", "notify", "speak":
	default:
		os.Exit(0)
	}

	// ポートは設定ファイルから取得(無ければ既定 7331)
	port := 7331
	if data, err := os.ReadFile(configPath()); err == nil {
		var c struct {
			Port int `json:"port"`
		}
		if json.Unmarshal(data, &c) == nil && c.Port > 0 {
			port = c.Port
		}
	}

	body, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	// 接続失敗(デーモン未起動)は素早く諦め、全体も短時間で打ち切る
	client := &http.Client{
		Timeout: 1500 * time.Millisecond,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{Timeout: 500 * time.Millisecond}).DialContext,
		},
	}
	resp, err := client.Post(
		"http://127.0.0.1:"+itoa(port)+"/"+mode,
		"application/json",
		bytes.NewReader(body),
	)
	if err == nil {
		resp.Body.Close()
	}
	// デーモン未起動でも黙って成功扱い(セッションを壊さない)
	os.Exit(0)
}

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// activePlays は現在進行中の発話(playLoop)の数。0より大きければ発話中。
var activePlays atomic.Int32

// isSpeaking は現在発話中かどうかを返す(トレイアイコン切替に使用)。
func isSpeaking() bool { return activePlays.Load() > 0 }

// 音声再生(playSoundFile / playSystemAlias / stopSound)と openBrowser は
// OS依存のため sys_windows.go / sys_linux.go に分離。

// --- 合成 (AivisSpeech / VOICEVOX 互換API) -------------------------------
var httpClient = &http.Client{Timeout: 30 * time.Second}

// synthesize は1チャンクのテキストをWAVバイト列に変換する。失敗時はnil。
// ctx をキャンセルすると進行中のHTTPリクエストを即座に打ち切る。
func synthesize(ctx context.Context, server string, speaker int, text string) []byte {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	server = strings.TrimRight(server, "/")
	spk := url.Values{"speaker": {itoa(speaker)}}

	// 1) audio_query
	q := url.Values{"text": {text}, "speaker": {itoa(speaker)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server+"/audio_query?"+q.Encode(), nil)
	if err != nil {
		return nil
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		logLine("synth audio_query failed: " + err.Error())
		return nil
	}
	query, _ := readAllClose(resp)
	if resp.StatusCode != 200 {
		logLine("synth audio_query status " + itoa(resp.StatusCode))
		return nil
	}

	// 2) synthesis
	req2, err := http.NewRequestWithContext(ctx, http.MethodPost, server+"/synthesis?"+spk.Encode(), bytes.NewReader(query))
	if err != nil {
		return nil
	}
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := httpClient.Do(req2)
	if err != nil {
		logLine("synth synthesis failed: " + err.Error())
		return nil
	}
	wav, _ := readAllClose(resp2)
	if resp2.StatusCode != 200 {
		logLine("synth synthesis status " + itoa(resp2.StatusCode))
		return nil
	}
	return wav
}

// fetchSpeakers はサーバーの話者一覧を取得する。
type apiStyle struct {
	Name string `json:"name"`
	ID   int    `json:"id"`
}
type apiSpeaker struct {
	Name   string     `json:"name"`
	Styles []apiStyle `json:"styles"`
}

func fetchSpeakers(server string) ([]apiSpeaker, error) {
	server = strings.TrimRight(server, "/")
	resp, err := httpClient.Get(server + "/speakers")
	if err != nil {
		return nil, err
	}
	body, _ := readAllClose(resp)
	if resp.StatusCode != 200 {
		return nil, errors.New("speakers status " + itoa(resp.StatusCode))
	}
	var out []apiSpeaker
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ensureValidSpeaker は cur がその話者一覧に存在すればそのまま、無ければ先頭の話者IDを返す。
func ensureValidSpeaker(speakers []apiSpeaker, cur int) int {
	first := -1
	for _, s := range speakers {
		for _, st := range s.Styles {
			if first < 0 {
				first = st.ID
			}
			if st.ID == cur {
				return cur
			}
		}
	}
	if first >= 0 {
		return first
	}
	return cur
}

// --- 発話パイプライン(世代カウンタでキャンセル) --------------------------
var (
	playMu     sync.Mutex
	playCancel context.CancelFunc
)

// cancelCurrent は進行中の発話を確実に停止する。
// playCancel(ctx) で合成/再生ループを終了させ、stopSound() で再生中の音を即停止する。
// speak() を経由しない停止(無効化・終了・チャイム)はすべてこれを使う。
func cancelCurrent() {
	playMu.Lock()
	if playCancel != nil {
		playCancel()
		playCancel = nil
	}
	playMu.Unlock()
	stopSound()
}

// speak はテキストを指定話者で読み上げる。新しい発話が来たら前の発話を中断する。
func speak(text string, speaker int) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	playMu.Lock()
	if playCancel != nil {
		playCancel() // 前の発話をキャンセル
	}
	ctx, cancel := context.WithCancel(context.Background())
	playCancel = cancel
	playMu.Unlock()

	stopSound() // 再生中の音を即停止

	go playLoop(ctx, text, speaker)
}

// chime は確認用のシステム音を鳴らす。発話中ならそれを止めてから鳴らす。
func chime() {
	cancelCurrent()
	go playSystemAlias("SystemAsterisk")
}

// --- 確認音のキャッシュ ---------------------------------------------------
// 確認(notify)の文言は毎回同じなので、一度だけ合成してWAVに保存し、
// 以降は合成(約2.6秒)を省いてファイルを即再生する。

func cacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".claude", "tts-cache")
}

// notifyCachePath は (サーバー, 話者, 文言) からキャッシュWAVのパスを決める。
// いずれかが変われば別ファイルになる。
func notifyCachePath(server string, speaker int, text string) string {
	h := sha256.Sum256([]byte(server + "|" + itoa(speaker) + "|" + text))
	name := "notify-" + itoa(speaker) + "-" + hex.EncodeToString(h[:6]) + ".wav"
	return filepath.Join(cacheDir(), name)
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// notifyCacheMu は ensureNotifyCache を直列化し、二重合成・一時ファイル競合を防ぐ。
var notifyCacheMu sync.Mutex

// ensureNotifyCache は現在の確認音WAVが無ければ合成して保存し、そのパスを返す。
// 合成できなければ空文字を返す。同時に呼ばれても合成は1回だけ(mutexで直列化)。
func ensureNotifyCache() string {
	c := getCfg()
	if c.NotifyText == "" || strings.TrimSpace(c.Server) == "" {
		return "" // サーバー未設定時は合成しない(既定は効果音)
	}
	path := notifyCachePath(c.Server, c.NotifySpeaker, c.NotifyText)

	notifyCacheMu.Lock()
	defer notifyCacheMu.Unlock()
	if fileExists(path) { // 先行の呼び出しが既に作っていれば再合成しない
		return path
	}
	wav := synthesize(context.Background(), c.Server, c.NotifySpeaker, c.NotifyText)
	if len(wav) == 0 {
		logLine("notify cache: synth failed")
		return ""
	}
	if err := os.MkdirAll(cacheDir(), 0o755); err != nil {
		return ""
	}
	f, err := os.CreateTemp(cacheDir(), "notify-*.tmp") // 一意な一時名
	if err != nil {
		return ""
	}
	tmp := f.Name()
	if _, err := f.Write(wav); err != nil {
		f.Close()
		os.Remove(tmp)
		return ""
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return ""
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		logLine("notify cache rename failed: " + err.Error())
		return ""
	}
	logLine("notify cache built: " + path)
	return path
}

// playNotify は確認音を鳴らす。キャッシュがあれば即再生。
// 無ければ1回だけ合成して保存し、そのファイルを再生する(二重合成しない)。
func playNotify() {
	c := getCfg()
	path := notifyCachePath(c.Server, c.NotifySpeaker, c.NotifyText)
	if fileExists(path) {
		playFile(path)
		return
	}
	go func() {
		if p := ensureNotifyCache(); p != "" {
			playFile(p)
		} else {
			speak(c.NotifyText, c.NotifySpeaker) // 合成失敗時のフォールバック
		}
	}()
}

// playFile は既成WAVファイルを発話と同じキャンセル機構で再生する。
func playFile(path string) {
	playMu.Lock()
	if playCancel != nil {
		playCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	playCancel = cancel
	playMu.Unlock()
	stopSound()

	go func() {
		activePlays.Add(1)
		refreshIcon()
		defer func() {
			if activePlays.Add(-1) == 0 {
				refreshIcon()
			}
		}()
		if ctx.Err() != nil {
			return
		}
		playSoundFile(path)
	}()
}

// playSoundBytes は埋め込みWAVバイト列を一時ファイルに書いて再生する(既定効果音用)。
func playSoundBytes(wav []byte) {
	if len(wav) == 0 {
		return
	}
	playMu.Lock()
	if playCancel != nil {
		playCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	playCancel = cancel
	playMu.Unlock()
	stopSound()

	go func() {
		activePlays.Add(1)
		refreshIcon()
		defer func() {
			if activePlays.Add(-1) == 0 {
				refreshIcon()
			}
		}()
		f, err := os.CreateTemp("", "cc_snd_*.wav")
		if err != nil {
			return
		}
		path := f.Name()
		_, _ = f.Write(wav)
		_ = f.Close()
		defer os.Remove(path)
		if ctx.Err() != nil {
			return
		}
		playSoundFile(path)
	}()
}

// playLoop はチャンクごとに「次を合成しつつ現在を再生」する。
func playLoop(ctx context.Context, text string, speaker int) {
	activePlays.Add(1)
	refreshIcon() // 発話中アイコン(停止マーク)へ
	defer func() {
		if activePlays.Add(-1) == 0 {
			refreshIcon() // 通常アイコンへ戻す
		}
	}()

	c := getCfg()
	chunks := splitChunks(text, 50)
	if len(chunks) == 0 {
		return
	}
	logLine("speak " + itoa(len(chunks)) + " chunk(s)")

	ch := make(chan []byte, 2)
	// producer: 合成
	go func() {
		defer close(ch)
		for _, ck := range chunks {
			if ctx.Err() != nil {
				return
			}
			wav := synthesize(ctx, c.Server, speaker, ck)
			select {
			case ch <- wav:
			case <-ctx.Done():
				return
			}
		}
	}()
	// consumer: 再生
	for {
		select {
		case <-ctx.Done():
			return
		case wav, ok := <-ch:
			if !ok {
				return
			}
			if len(wav) == 0 {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			playBytes(ctx, wav)
		}
	}
}

// playBytes はWAVバイト列を一時ファイルに書いて同期再生する。
func playBytes(ctx context.Context, wav []byte) {
	f, err := os.CreateTemp("", "cc_tts_*.wav")
	if err != nil {
		return
	}
	path := f.Name()
	_, _ = f.Write(wav)
	_ = f.Close()
	defer os.Remove(path)

	if ctx.Err() != nil {
		return
	}
	playSoundFile(path) // 再生終了までブロック。次のspeakでstopSound()され即復帰
}

// --- テキスト整形 ---------------------------------------------------------
var (
	reCodeBlock = regexp.MustCompile("(?s)```.*?```")
	reInline    = regexp.MustCompile("`[^`]*`")
	reImage     = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	reLink      = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	reURL       = regexp.MustCompile(`https?://\S+`)
	// 裸のURL: www.〜 や ドメイン+TLD(+パス)。http(s)無しでも除去する。
	reBareURL  = regexp.MustCompile(`(?i)(?:www\.[^\s、。）)」』]+|[a-z0-9][a-z0-9-]*(?:\.[a-z0-9-]+)*\.(?:com|net|org|io|dev|jp|co|ai|app|gg|sh|me|info|biz|tv|cloud|page|xyz|tokyo|gov|edu)(?:/[^\s、。）)」』]*)?)`)
	reWinPath   = regexp.MustCompile(`[A-Za-z]:[\\/][^\s]+`)
	reUnixPath  = regexp.MustCompile(`/[\w.\-]+(?:/[\w.\-]+)+`) // /usr/local/bin 等(2段以上)
	reHeading   = regexp.MustCompile(`(?m)^[ \t]*#{1,6}[ \t]*`)
	reBullet    = regexp.MustCompile(`(?m)^[ \t]*[-*+>][ \t]+`)
	reNumbered  = regexp.MustCompile(`(?m)^[ \t]*\d+\.[ \t]+`)
	reSpaces    = regexp.MustCompile(`[ \t]+`)
	reNewlines  = regexp.MustCompile(`\n{2,}`)
)

// cleanForSpeech はコード/マークダウンを除去し、読み上げ向けの地の文に整える。
func cleanForSpeech(text string, maxChars int) string {
	if text == "" {
		return ""
	}
	t := text
	t = reCodeBlock.ReplaceAllString(t, "")
	t = reInline.ReplaceAllString(t, "")
	t = reImage.ReplaceAllString(t, "")
	t = reLink.ReplaceAllString(t, "$1")
	t = reURL.ReplaceAllString(t, "")
	t = reBareURL.ReplaceAllString(t, "")
	t = reWinPath.ReplaceAllString(t, "")
	t = reUnixPath.ReplaceAllString(t, "")
	t = reHeading.ReplaceAllString(t, "")
	t = reBullet.ReplaceAllString(t, "")
	t = reNumbered.ReplaceAllString(t, "")
	t = strings.ReplaceAll(t, "**", "")
	t = strings.ReplaceAll(t, "__", "")
	t = strings.ReplaceAll(t, "*", "")
	t = reSpaces.ReplaceAllString(t, " ")
	t = reNewlines.ReplaceAllString(t, "\n")
	t = strings.TrimSpace(t)

	r := []rune(t)
	if maxChars > 0 && len(r) > maxChars {
		t = string(r[:maxChars]) + " 以下省略"
	}
	return t
}

// splitChunks は文末/改行で区切りつつ、各チャンクを最大 maxRunes 文字に収める。
func splitChunks(text string, maxRunes int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	terminators := "。．！？!?\n"
	var pieces []string
	var cur []rune
	for _, ch := range text {
		cur = append(cur, ch)
		if strings.ContainsRune(terminators, ch) {
			pieces = append(pieces, strings.TrimSpace(string(cur)))
			cur = nil
		}
	}
	if len(cur) > 0 {
		pieces = append(pieces, strings.TrimSpace(string(cur)))
	}

	var chunks []string
	var buf []rune
	flush := func() {
		if len(buf) > 0 {
			chunks = append(chunks, string(buf))
			buf = nil
		}
	}
	for _, p := range pieces {
		if p == "" {
			continue
		}
		pr := []rune(p)
		if len(buf)+len(pr) <= maxRunes {
			buf = append(buf, pr...)
			continue
		}
		flush()
		for len(pr) > maxRunes {
			chunks = append(chunks, string(pr[:maxRunes]))
			pr = pr[maxRunes:]
		}
		buf = append(buf, pr...)
	}
	flush()
	return chunks
}

// --- トランスクリプト解析 -------------------------------------------------
// lastAssistantText はJSONLトランスクリプトから最後のassistantテキストを抽出する。
func lastAssistantText(transcriptPath string) string {
	if transcriptPath == "" {
		return ""
	}
	f, err := os.Open(transcriptPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	last := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024) // 長い行に対応
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev struct {
			Type    string `json:"type"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Type != "assistant" {
			continue
		}
		txt := extractText(ev.Message.Content)
		if txt != "" {
			last = txt
		}
	}
	return last
}

// extractText は content (文字列 or ブロック配列) からテキストだけを連結する。
func extractText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	}
	return ""
}

// --- 小物 -----------------------------------------------------------------
func readAllClose(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	var b bytes.Buffer
	_, err := b.ReadFrom(resp.Body)
	return b.Bytes(), err
}


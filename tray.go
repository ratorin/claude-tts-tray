package main

import (
	_ "embed"
	"os"
	"os/exec"
	"sort"
	"sync/atomic"

	"fyne.io/systray"
)

//go:embed icon_on.ico
var iconOn []byte

//go:embed icon_off.ico
var iconOff []byte

//go:embed icon_speaking.ico
var iconSpeaking []byte

// trayReady は systray の準備完了フラグ(onReady完了後にtrue)。
var trayReady atomic.Bool

func main() {
	// フックモード: `claude-tts-tray.exe hook <stop|notify|speak>`
	if len(os.Args) >= 3 && os.Args[1] == "hook" {
		runHook(os.Args[2])
		return
	}

	// 常駐モード(トレイ)
	loadConfig()
	c := getCfg()
	if err := startServer(c.Port); err != nil {
		logLine("server start failed: " + err.Error())
	}
	go ensureNotifyCache() // 確認音を先に用意して初回から即時に
	systray.Run(onReady, func() {})
}

func onReady() {
	trayReady.Store(true)
	refreshIcon()
	systray.SetTitle("")
	updateTooltip()

	// トレイアイコン左クリック=今すぐ停止 / 右クリック=メニュー。
	// コールバックは systray のメッセージスレッドで走るため、goroutine に逃がして
	// メッセージポンプをブロックしない。
	systray.SetOnTapped(func() { go cancelCurrent() })

	// ON/OFF トグル
	mEnabled := systray.AddMenuItemCheckbox("読み上げ 有効", "読み上げの ON/OFF", getCfg().Enabled)
	go func() {
		for range mEnabled.ClickedCh {
			updateCfg(func(c *Config) { c.Enabled = !c.Enabled })
			if getCfg().Enabled {
				mEnabled.Check()
			} else {
				mEnabled.Uncheck()
				cancelCurrent() // 進行中の発話を確実に止める(バッファ済みチャンクも含む)
			}
			refreshIcon()
			updateTooltip()
		}
	}()

	// 今すぐ停止(再生中の音声を止める)
	mStop := systray.AddMenuItem("今すぐ停止", "再生中の音声を止める")
	go func() {
		for range mStop.ClickedCh {
			cancelCurrent()
		}
	}()

	systray.AddSeparator()

	// サーバー選択
	mServer := systray.AddMenuItem("サーバー", "音声合成サーバー")
	buildServerMenu(mServer)
	mServerEdit := systray.AddMenuItem("　サーバーを追加・編集…", "ブラウザで設定ページを開く")
	go func() {
		for range mServerEdit.ClickedCh {
			openBrowser("http://127.0.0.1:" + itoa(getCfg().Port) + "/")
		}
	}()

	// 音声選択(読み上げ用と確認用を別々に)。話者一覧は一度だけ取得して共有。
	speakers, sErr := fetchSpeakers(getCfg().Server)
	mVoiceRead := systray.AddMenuItem("音声（読み上げ）", "返答読み上げの話者")
	buildVoiceMenu(mVoiceRead, speakers, sErr, "read")
	mVoiceNotify := systray.AddMenuItem("音声（確認）", "確認通知の話者")
	buildVoiceMenu(mVoiceNotify, speakers, sErr, "notify")

	// 確認音
	mNotify := systray.AddMenuItem("確認音", "確認(permission等)の通知音")
	buildNotifyMenu(mNotify)

	systray.AddSeparator()

	mTest := systray.AddMenuItem("テスト発話（読み上げ）", "読み上げ話者で試聴")
	go func() {
		for range mTest.ClickedCh {
			c := getCfg()
			speak("これは読み上げのテストです。", c.Speaker)
		}
	}()

	mTestNotify := systray.AddMenuItem("テスト発話（確認）", "確認話者で試聴")
	go func() {
		for range mTestNotify.ClickedCh {
			c := getCfg()
			speak(c.NotifyText, c.NotifySpeaker)
		}
	}()

	mReload := systray.AddMenuItem("再起動して反映", "サーバー変更後の音声一覧を更新")
	go func() {
		for range mReload.ClickedCh {
			restartSelf()
		}
	}()

	mQuit := systray.AddMenuItem("終了", "常駐を終了")
	go func() {
		<-mQuit.ClickedCh
		cancelCurrent()
		systray.Quit()
	}()
}

// buildServerMenu はサーバー選択肢(ラジオ)を構築する。
func buildServerMenu(parent *systray.MenuItem) {
	c := getCfg()
	names := make([]string, 0, len(c.Servers))
	for name := range c.Servers {
		names = append(names, name)
	}
	sort.Strings(names) // メニューの並び順を毎回一定にする
	var items []*systray.MenuItem
	var urls []string
	for _, name := range names {
		u := c.Servers[name]
		it := parent.AddSubMenuItemCheckbox(name, u, u == c.Server)
		items = append(items, it)
		urls = append(urls, u)
	}
	for i := range items {
		idx := i
		go func() {
			for range items[idx].ClickedCh {
				selectServer(urls[idx])
				for j, it := range items {
					if j == idx {
						it.Check()
					} else {
						it.Uncheck()
					}
				}
			}
		}()
	}
}

// selectServer はサーバーを切り替え、新サーバーで有効な話者(読み上げ・確認)を選び直す。
func selectServer(u string) {
	speakers, err := fetchSpeakers(u)
	updateCfg(func(c *Config) {
		c.Server = u
		if err == nil && len(speakers) > 0 {
			c.Speaker = ensureValidSpeaker(speakers, c.Speaker)
			c.NotifySpeaker = ensureValidSpeaker(speakers, c.NotifySpeaker)
		}
	})
	updateTooltip()
	go ensureNotifyCache() // 新サーバー用の確認音キャッシュを用意
	logLine("server switched to " + u)
}

// buildVoiceMenu は話者一覧(ラジオ)を構築する。
// which="read" は読み上げ話者、"notify" は確認話者を設定する。
func buildVoiceMenu(parent *systray.MenuItem, speakers []apiSpeaker, err error, which string) {
	if err != nil {
		parent.AddSubMenuItem("(取得失敗: サーバー未起動?)", err.Error()).Disable()
		return
	}
	c := getCfg()
	current := c.Speaker
	if which == "notify" {
		current = c.NotifySpeaker
	}
	var items []*systray.MenuItem
	var ids []int
	for _, s := range speakers {
		for _, st := range s.Styles {
			title := s.Name + " / " + st.Name
			it := parent.AddSubMenuItemCheckbox(title, itoa(st.ID), st.ID == current)
			items = append(items, it)
			ids = append(ids, st.ID)
		}
	}
	for i := range items {
		idx := i
		go func() {
			for range items[idx].ClickedCh {
				updateCfg(func(c *Config) {
					if which == "notify" {
						c.NotifySpeaker = ids[idx]
					} else {
						c.Speaker = ids[idx]
					}
				})
				for j, it := range items {
					if j == idx {
						it.Check()
					} else {
						it.Uncheck()
					}
				}
				updateTooltip()
				if which == "notify" {
					go ensureNotifyCache() // 確認話者が変わったらキャッシュ作り直し
				}
			}
		}()
	}
}

// openBrowser は既定のブラウザでURLを開く。
func openBrowser(url string) {
	if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start(); err != nil {
		logLine("openBrowser failed: " + err.Error())
	}
}

// buildNotifyMenu は確認音の種類(ラジオ)を構築する。
func buildNotifyMenu(parent *systray.MenuItem) {
	c := getCfg()
	modes := []struct {
		key, label string
	}{
		{"speak", "発話「" + c.NotifyText + "」"},
		{"chime", "チャイム音"},
		{"none", "なし"},
	}
	var items []*systray.MenuItem
	for _, m := range modes {
		it := parent.AddSubMenuItemCheckbox(m.label, "", m.key == c.NotifyMode)
		items = append(items, it)
	}
	for i := range items {
		idx := i
		key := modes[idx].key
		go func() {
			for range items[idx].ClickedCh {
				updateCfg(func(c *Config) { c.NotifyMode = key })
				for j, it := range items {
					if j == idx {
						it.Check()
					} else {
						it.Uncheck()
					}
				}
			}
		}()
	}
}

// refreshIcon は現在状態に応じてトレイアイコンを切り替える。
// 発話中=停止マーク(オレンジ)、有効=緑、無効=灰。
func refreshIcon() {
	if !trayReady.Load() {
		return
	}
	switch {
	case isSpeaking():
		systray.SetIcon(iconSpeaking)
	case getCfg().Enabled:
		systray.SetIcon(iconOn)
	default:
		systray.SetIcon(iconOff)
	}
}

// updateTooltip はトレイのツールチップを現在状態に更新する。
func updateTooltip() {
	c := getCfg()
	state := "OFF"
	if c.Enabled {
		state = "ON"
	}
	systray.SetTooltip("Claude TTS [" + state + "] " + nameOfServer(c.Server) + " | 左:停止 右:メニュー")
}

// nameOfServer はURLに対応する表示名を返す(無ければURL)。
func nameOfServer(u string) string {
	for name, su := range getCfg().Servers {
		if su == u {
			return name
		}
	}
	return u
}

// restartSelf は自身を再起動する(音声一覧の再読み込み用)。
func restartSelf() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	closeServer() // ポートを解放してから新プロセスを起動(再バインド競合を回避)
	cmd := exec.Command(exe)
	if err := cmd.Start(); err != nil {
		logLine("restart failed: " + err.Error())
		return
	}
	cancelCurrent()
	systray.Quit()
}

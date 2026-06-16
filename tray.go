package main

import (
	"os"
	"os/exec"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"

	"fyne.io/systray"
)

// アイコン(iconOn/iconOff/iconSpeaking)は OS別に icons_windows.go(.ico) /
// icons_linux.go(.png) で埋め込む。

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
	applyMenuTheme()       // メニュー文字色対策(Win)。プロセスのメニューテーマを先に確定
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

	// 音声選択(読み上げ/確認)。各メニューは「効果音（合成なし）」＋「人 ▸ 種類」の2段。
	// 話者一覧は一度だけ取得して共有。
	speakers, sErr := fetchSpeakers(getCfg().Server)
	mVoiceRead := systray.AddMenuItem("音声（読み上げ）", "返答読み上げ: 効果音 or 話者")
	buildVoiceMenu(mVoiceRead, speakers, sErr, "read")
	mVoiceNotify := systray.AddMenuItem("音声（確認）", "確認通知: 効果音 or 話者")
	buildVoiceMenu(mVoiceNotify, speakers, sErr, "notify")

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

	// ログイン時に自動起動(Win=スタートアップの.lnk / Linux=XDG autostart)
	mAuto := systray.AddMenuItemCheckbox("ログイン時に自動起動", "スタートアップ登録/解除", autostartEnabled())
	go func() {
		for range mAuto.ClickedCh {
			if err := setAutostart(!autostartEnabled()); err != nil {
				logLine("autostart toggle failed: " + err.Error())
			}
			if autostartEnabled() {
				mAuto.Check()
			} else {
				mAuto.Uncheck()
			}
		}
	}()

	mQuit := systray.AddMenuItem("終了", "常駐を終了")
	go func() {
		<-mQuit.ClickedCh
		cancelCurrent()
		systray.Quit()
	}()

	// メニュー構築後にもテーマを再フラッシュ(文字色のゆらぎ対策・Winのみ実体)
	applyMenuTheme()
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

// buildVoiceMenu は「効果音（合成なし）＋ 人 ▸ 種類」の2段メニューを構築する。
// 効果音と話者は1つのラジオ(排他)として扱う。
// which="read"  は読み上げ(ReadMode/Speaker)、"notify" は確認(NotifyMode/NotifySpeaker)を設定する。
func buildVoiceMenu(parent *systray.MenuItem, speakers []apiSpeaker, err error, which string) {
	// Linux(Cinnamon等)は深い入れ子サブメニューが不安定で3階層目が空になる。
	// トレイは「音声/チャイム/OFF」の浅いモード選択のみにし、話者選択は設定ページ(Web)で行う。
	if runtime.GOOS == "linux" {
		buildVoiceModeMenu(parent, which)
		return
	}
	// --- 以下 Windows: 効果音 + 人▸種類 の2段 ---
	// 効果音の選択肢(用途別)と「音声」を表すモード値
	effects := []struct{ key, label string }{
		{"chime", "チャイム"},
		{"none", "なし"},
	}
	voiceMode := "speak" // 確認で「発話」を表す値
	if which == "read" {
		effects = []struct{ key, label string }{
			{"done", "完了音"},
			{"chime", "チャイム"},
			{"none", "なし"},
		}
		voiceMode = "voice"
	}

	c := getCfg()
	curMode, curSpeaker := c.NotifyMode, c.NotifySpeaker
	if which == "read" {
		curMode, curSpeaker = c.ReadMode, c.Speaker
	}

	// (1) 効果音サブメニュー
	const effTitle = "効果音（合成なし）"
	var effItems []*systray.MenuItem
	var effKeys []string
	mEff := parent.AddSubMenuItem(effTitle, "音声合成せず効果音を鳴らす")
	for _, e := range effects {
		it := mEff.AddSubMenuItemCheckbox(e.label, e.key, curMode == e.key)
		effItems = append(effItems, it)
		effKeys = append(effKeys, e.key)
	}

	// (2) 話者: 人 ▸ 種類
	var styleItems []*systray.MenuItem
	var styleIDs []int
	var personItems []*systray.MenuItem // 1段目(人)の項目。選択マーク用に保持
	var personNames []string            // 1段目の元タイトル
	var personIDsets [][]int            // 各人の配下スタイルID群
	if err != nil {
		parent.AddSubMenuItem("（音声一覧の取得失敗: サーバー未起動?）", err.Error()).Disable()
	} else {
		for _, s := range speakers {
			mPerson := parent.AddSubMenuItem(s.Name, s.Name)
			personItems = append(personItems, mPerson)
			personNames = append(personNames, s.Name)
			var ids []int
			for _, st := range s.Styles {
				checked := curMode == voiceMode && st.ID == curSpeaker
				it := mPerson.AddSubMenuItemCheckbox(st.Name, itoa(st.ID), checked)
				styleItems = append(styleItems, it)
				styleIDs = append(styleIDs, st.ID)
				ids = append(ids, st.ID)
			}
			personIDsets = append(personIDsets, ids)
		}
	}

	// 【重要・Linux DBus対策】選択のたびに全項目を更新すると systray(DBus) が詰まり
	// メニューがフリーズして見える。そこで現在チェック中の index を保持し、
	// 変わった項目だけ Check/Uncheck・SetTitle する。
	// 初期チェック状態は AddSubMenuItemCheckbox 済みなので、それに合わせて初期化する。
	checkedStyle, checkedEff := -1, -1
	if curMode != voiceMode {
		for j, k := range effKeys {
			if k == curMode {
				checkedEff = j
				break
			}
		}
	} else {
		for j, id := range styleIDs {
			if id == curSpeaker {
				checkedStyle = j
				break
			}
		}
	}
	markedPerson := -1
	effMarked := false
	var selMu sync.Mutex // 選択状態(checked*/marked*)の更新を直列化

	// relabel は1段目(効果音/人)の「●」マークを、変化があった項目だけ付け替える。
	// 2段目(種類)のチェックだけでは、人を開かないと現在のモデルが分からないため。
	relabel := func() {
		c := getCfg()
		mode, spk := c.NotifyMode, c.NotifySpeaker
		if which == "read" {
			mode, spk = c.ReadMode, c.Speaker
		}
		effActive := mode != voiceMode // voice/speak 以外なら効果音が選択中
		activePerson := -1
		if !effActive {
			for i, ids := range personIDsets {
				for _, id := range ids {
					if id == spk {
						activePerson = i
						break
					}
				}
				if activePerson >= 0 {
					break
				}
			}
		}
		selMu.Lock()
		defer selMu.Unlock()
		if effActive != effMarked {
			if effActive {
				mEff.SetTitle("● " + effTitle)
			} else {
				mEff.SetTitle(effTitle)
			}
			effMarked = effActive
		}
		if activePerson != markedPerson {
			if markedPerson >= 0 {
				personItems[markedPerson].SetTitle(personNames[markedPerson])
			}
			if activePerson >= 0 {
				personItems[activePerson].SetTitle("● " + personNames[activePerson])
			}
			markedPerson = activePerson
		}
	}
	relabel() // 初期表示にマークを反映

	// setChecks は効果音/話者のチェックを、変わった項目だけ付け替える(排他)。非該当は -1。
	setChecks := func(styleIdx, effIdx int) {
		selMu.Lock()
		defer selMu.Unlock()
		if checkedStyle != styleIdx {
			if checkedStyle >= 0 {
				styleItems[checkedStyle].Uncheck()
			}
			if styleIdx >= 0 {
				styleItems[styleIdx].Check()
			}
			checkedStyle = styleIdx
		}
		if checkedEff != effIdx {
			if checkedEff >= 0 {
				effItems[checkedEff].Uncheck()
			}
			if effIdx >= 0 {
				effItems[effIdx].Check()
			}
			checkedEff = effIdx
		}
	}

	// 効果音クリック
	for i := range effItems {
		idx := i
		go func() {
			for range effItems[idx].ClickedCh {
				key := effKeys[idx]
				updateCfg(func(c *Config) {
					if which == "read" {
						c.ReadMode = key
					} else {
						c.NotifyMode = key
					}
				})
				setChecks(-1, idx)
				relabel()
				updateTooltip()
				if which == "notify" {
					go ensureNotifyCache()
				}
			}
		}()
	}

	// 話者(種類)クリック → 音声モードに切替＋話者IDを設定
	for i := range styleItems {
		idx := i
		go func() {
			for range styleItems[idx].ClickedCh {
				id := styleIDs[idx]
				updateCfg(func(c *Config) {
					if which == "read" {
						c.ReadMode = "voice"
						c.Speaker = id
					} else {
						c.NotifyMode = "speak"
						c.NotifySpeaker = id
					}
				})
				setChecks(idx, -1)
				relabel()
				updateTooltip()
				if which == "notify" {
					go ensureNotifyCache() // 確認話者が変わったらキャッシュ作り直し
				}
			}
		}()
	}
}

// buildVoiceModeMenu は Linux 用の簡易モード選択(音声/チャイム/OFF)を作る。
// 具体的な話者選択は設定ページ(ブラウザ)で行う(深い入れ子を避けるため)。
func buildVoiceModeMenu(parent *systray.MenuItem, which string) {
	// read は完了音(done)も選べる。notify(確認)は done が無いので 音声/チャイム/OFF。
	opts := []struct{ key, label string }{
		{"speak", "音声"},
		{"chime", "チャイム"},
		{"none", "OFF"},
	}
	if which == "read" {
		opts = []struct{ key, label string }{
			{"voice", "音声"},
			{"done", "完了音"},
			{"chime", "チャイム"},
			{"none", "OFF"},
		}
	}
	cur := getCfg().ReadMode
	if which == "notify" {
		cur = getCfg().NotifyMode
	}
	var items []*systray.MenuItem
	var keys []string
	for _, o := range opts {
		it := parent.AddSubMenuItemCheckbox(o.label, o.key, o.key == cur)
		items = append(items, it)
		keys = append(keys, o.key)
	}
	for i := range items {
		idx := i
		go func() {
			for range items[idx].ClickedCh {
				key := keys[idx]
				updateCfg(func(c *Config) {
					if which == "read" {
						c.ReadMode = key
					} else {
						c.NotifyMode = key
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
					go ensureNotifyCache()
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

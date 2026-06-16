package main

import (
	"encoding/json"
	"html/template"
	"io"
	"net"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	httpSrv *http.Server
	httpLn  net.Listener
)

// startServer は 127.0.0.1 限定でローカルHTTPサーバーを起動する。
// フック(curl)からの /stop /notify を受けて読み上げる。
// 即座に200を返し、合成・再生はバックグラウンドで行う(フックをブロックしない)。
// 自己再起動直後はポート解放待ちで bind に失敗しうるため数回リトライする。
func startServer(port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", handlePing)
	mux.HandleFunc("/stop", handleStop)
	mux.HandleFunc("/notify", handleNotify)
	mux.HandleFunc("/speak", handleSpeak)
	mux.HandleFunc("/", handleUI)
	mux.HandleFunc("/servers/add", handleServerAdd)
	mux.HandleFunc("/servers/delete", handleServerDelete)
	mux.HandleFunc("/servers/use", handleServerUse)
	mux.HandleFunc("/voice/set", handleVoiceSet)

	// 127.0.0.1 のみにバインド(外部公開しない)
	var ln net.Listener
	var err error
	for i := 0; i < 10; i++ {
		ln, err = net.Listen("tcp", "127.0.0.1:"+itoa(port))
		if err == nil {
			break
		}
		time.Sleep(150 * time.Millisecond) // 旧プロセスのポート解放待ち
	}
	if err != nil {
		return err
	}
	httpLn = ln
	httpSrv = &http.Server{Handler: localOnly(mux)}
	go func() {
		logLine("server listening on 127.0.0.1:" + itoa(port))
		_ = httpSrv.Serve(ln)
	}()
	return nil
}

// closeServer はリスナーを閉じてポートを解放する(再起動前に呼ぶ)。
func closeServer() {
	if httpSrv != nil {
		_ = httpSrv.Close()
	}
}

// localOnly はループバック以外からのアクセスを拒否する保険。
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden) // 不明なアドレスは拒否(フェイルクローズ)
			return
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// blockedCrossOrigin はブラウザからのクロスオリジンPOSTを弾く(localhostへのCSRF対策)。
// curl/フックは Origin を送らないため通過する。設定ページ(同一オリジン)も許可。
// 戻り値 true なら拒否済み(呼び出し側は return する)。
func blockedCrossOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false // 非ブラウザ(curl/フック)
	}
	port := itoa(getCfg().Port)
	if origin == "http://127.0.0.1:"+port || origin == "http://localhost:"+port {
		return false // 設定ページ自身
	}
	http.Error(w, "forbidden", http.StatusForbidden)
	return true
}

func handlePing(w http.ResponseWriter, r *http.Request) {
	c := getCfg()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"enabled": c.Enabled,
		"server":  c.Server,
		"speaker": c.Speaker,
	})
}

// handleStop はフックのStopペイロード(JSON)を受け、最後の返答を読み上げる。
func handleStop(w http.ResponseWriter, r *http.Request) {
	if blockedCrossOrigin(w, r) {
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	w.WriteHeader(http.StatusOK) // 先に返してフックを解放
	io.WriteString(w, "ok")

	c := getCfg()
	if !c.Enabled {
		return
	}
	// 読み上げモード: 効果音/なし は合成せずここで完結
	switch c.ReadMode {
	case "none":
		return
	case "done":
		if recentNotify(1500 * time.Millisecond) { // 確認チャイム直後は完了音を抑止(チャイム優先)
			logLine("stop: recent notify -> skip done")
			return
		}
		playSoundBytes(soundDone)
		return
	case "chime":
		if recentNotify(1500 * time.Millisecond) {
			logLine("stop: recent notify -> skip chime")
			return
		}
		playSoundBytes(soundNotify)
		return
	}
	// "voice"(既定): サーバーがあれば読み上げ、無ければ完了音にフォールバック
	if strings.TrimSpace(c.Server) == "" {
		logLine("stop: no server -> done sound")
		playSoundBytes(soundDone)
		return
	}
	var payload struct {
		TranscriptPath string `json:"transcript_path"`
	}
	_ = json.Unmarshal(body, &payload)
	text := cleanForSpeech(lastAssistantText(payload.TranscriptPath), c.MaxChars)
	logLine("stop text len=" + itoa(len([]rune(text))))
	if text != "" {
		speak(text, c.Speaker) // 読み上げの話者
	}
}

// handleNotify は確認(permission等)の通知音を鳴らす。
func handleNotify(w http.ResponseWriter, r *http.Request) {
	if blockedCrossOrigin(w, r) {
		return
	}
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, "ok")

	c := getCfg()
	logLine("notify received (mode=" + c.NotifyMode + " enabled=" + boolStr(c.Enabled) + ")")
	if !c.Enabled {
		return
	}
	switch {
	case c.NotifyMode == "none":
		return
	case c.NotifyMode == "speak" && strings.TrimSpace(c.Server) != "":
		markNotify()
		playNotify() // 発話(サーバーで合成、キャッシュ即再生)
	default: // "chime" または サーバー未設定 → 埋め込み効果音
		markNotify()
		playSoundBytes(soundNotify)
	}
}

// handleSpeak は任意テキストをそのまま読み上げる(テスト/手動用)。
func handleSpeak(w http.ResponseWriter, r *http.Request) {
	if blockedCrossOrigin(w, r) {
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, "ok")

	c := getCfg()
	if !c.Enabled {
		return
	}
	speak(string(body), c.Speaker)
}

// --- 設定ページ(サーバーの追加/削除/選択) --------------------------------
type uiServer struct {
	Name    string
	URL     string
	Current bool
}
type uiOption struct {
	Value, Label string
	Selected     bool
}
type uiGroup struct {
	Label   string
	Options []uiOption
}
type uiData struct {
	Servers   []uiServer
	Port      int
	ShowVoice bool        // Linux のみ話者選択UIを表示
	HasServer bool        // 話者一覧が取得できたか
	VoiceData template.JS // 話者データ JSON: [{n,s:[{id,t}]}]
	ReadCur   int         // 読み上げの現在話者ID
	NotifyCur int         // 確認の現在話者ID
}

var uiTmpl = template.Must(template.New("ui").Parse(`<!doctype html>
<html lang="ja"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Claude TTS サーバー設定</title>
<style>
 body{font-family:"Segoe UI",sans-serif;max-width:680px;margin:24px auto;padding:0 16px;color:#222}
 h1{font-size:20px} h2{font-size:16px;margin-top:28px}
 table{border-collapse:collapse;width:100%;margin-top:8px}
 th,td{border:1px solid #ccc;padding:6px 8px;text-align:left;font-size:14px}
 th{background:#f4f4f4}
 .cur{color:#1a8a4a;font-weight:bold}
 input[type=text]{padding:6px;width:100%;box-sizing:border-box}
 button{padding:6px 12px;cursor:pointer}
 form.inline{display:inline;margin:0}
 .note{color:#888;font-size:12px;margin-top:16px;line-height:1.6}
</style></head><body>
<h1>Claude TTS サーバー設定</h1>
<table>
<tr><th>表示名</th><th>URL</th><th>状態</th><th>操作</th></tr>
{{range .Servers}}
<tr>
 <td>{{.Name}}</td>
 <td>{{.URL}}</td>
 <td>{{if .Current}}<span class="cur">使用中</span>{{end}}</td>
 <td>
  <form class="inline" method="post" action="/servers/use">
   <input type="hidden" name="url" value="{{.URL}}">
   <button type="submit"{{if .Current}} disabled{{end}}>使う</button>
  </form>
  <form class="inline" method="post" action="/servers/delete" onsubmit="return confirm('削除しますか?')">
   <input type="hidden" name="name" value="{{.Name}}">
   <button type="submit"{{if .Current}} disabled{{end}}>削除</button>
  </form>
 </td>
</tr>
{{end}}
</table>
{{if .ShowVoice}}
<h2>音声（話者）</h2>
{{if .HasServer}}
<table>
<tr><th>用途</th><th>人</th><th>種類</th></tr>
<tr><td>読み上げ</td><td><select id="read_person" onchange="vPerson('read')"></select></td><td><div id="read_styles"></div></td></tr>
<tr><td>確認</td><td><select id="notify_person" onchange="vPerson('notify')"></select></td><td><div id="notify_styles"></div></td></tr>
</table>
<p class="note">※ 人を選ぶと種類がボタンで出ます。種類を選ぶと即保存。音声/チャイム/OFF の切替はトレイメニューで。</p>
<script>
var SPK={{.VoiceData}};
var CUR={read:{{.ReadCur}},notify:{{.NotifyCur}}};
function vFillP(w){var p=document.getElementById(w+'_person');p.innerHTML='';for(var i=0;i<SPK.length;i++){var o=document.createElement('option');o.value=i;o.text=SPK[i].n;p.appendChild(o);}}
function vFillS(w){var pi=document.getElementById(w+'_person').value;var box=document.getElementById(w+'_styles');box.innerHTML='';var st=SPK[pi].s;for(var j=0;j<st.length;j++){var lb=document.createElement('label');lb.style.marginRight='14px';var r=document.createElement('input');r.type='radio';r.name='st_'+w;r.value=st[j].id;if(st[j].id==CUR[w])r.checked=true;r.addEventListener('change',function(){vSave(w);});lb.appendChild(r);lb.appendChild(document.createTextNode(' '+st[j].t));box.appendChild(lb);}}
function vPerson(w){vFillS(w);}
function vSave(w){var r=document.querySelector('input[name="st_'+w+'"]:checked');if(!r)return;var f=document.createElement('form');f.method='post';f.action='/voice/set';f.innerHTML='<input name="which" value="'+w+'"><input name="spk" value="'+r.value+'">';document.body.appendChild(f);f.submit();}
function vInit(w){vFillP(w);var pi=0;for(var i=0;i<SPK.length;i++)for(var j=0;j<SPK[i].s.length;j++)if(SPK[i].s[j].id==CUR[w])pi=i;document.getElementById(w+'_person').value=pi;vFillS(w);}
window.addEventListener('load',function(){vInit('read');vInit('notify');});
</script>
{{else}}
<p class="note">※ 話者を選ぶにはサーバー（VOICEVOX/AivisSpeech）を接続してください。</p>
{{end}}
{{end}}
<h2>サーバーを追加</h2>
<form method="post" action="/servers/add">
 <p>表示名: <input type="text" name="name" placeholder="自宅サーバー" required></p>
 <p>URL: <input type="text" name="url" placeholder="http://192.168.1.10:10101" required></p>
 <button type="submit">追加</button>
</form>
<p class="note">
 ※ 追加・削除・選択はその場で保存されます。<br>
 ※ トレイの「サーバー」「音声」メニューの表示を更新するには、トレイの「再起動して反映」を実行してください
 （サーバー選択自体は即時反映されます）。
</p>
</body></html>`))

func handleUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	c := getCfg()
	names := make([]string, 0, len(c.Servers))
	for name := range c.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	data := uiData{Port: c.Port}
	for _, name := range names {
		u := c.Servers[name]
		data.Servers = append(data.Servers, uiServer{Name: name, URL: u, Current: u == c.Server})
	}
	// Linux はトレイの深い入れ子が不安定なため、話者選択をこのページで行う。
	if runtime.GOOS == "linux" {
		data.ShowVoice = true
		speakers, sErr := fetchSpeakers(c.Server)
		data.HasServer = sErr == nil && len(speakers) > 0
		data.VoiceData = buildVoiceData(speakers)
		data.ReadCur = c.Speaker
		data.NotifyCur = c.NotifySpeaker
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = uiTmpl.Execute(w, data)
}

// buildVoiceData は話者一覧を JS 用 JSON([{n,s:[{id,t}]}]) に整形する(人ドロップ＋種類ラジオ用)。
func buildVoiceData(speakers []apiSpeaker) template.JS {
	type stJSON struct {
		ID int    `json:"id"`
		T  string `json:"t"`
	}
	type spkJSON struct {
		N string   `json:"n"`
		S []stJSON `json:"s"`
	}
	out := make([]spkJSON, 0, len(speakers))
	for _, s := range speakers {
		sj := spkJSON{N: s.Name}
		for _, st := range s.Styles {
			sj.S = append(sj.S, stJSON{ID: st.ID, T: st.Name})
		}
		out = append(out, sj)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return template.JS("[]")
	}
	return template.JS(b)
}

// handleVoiceSet は設定ページからの話者選択を受け、読み上げ/確認の話者IDを更新する。
// モード(音声/チャイム/OFF)はトレイ側で設定するため、ここでは話者IDのみ変更する。
func handleVoiceSet(w http.ResponseWriter, r *http.Request) {
	if blockedCrossOrigin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	_ = r.ParseForm()
	which := r.FormValue("which")
	id, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("spk")))
	if id > 0 {
		speakers, _ := fetchSpeakers(getCfg().Server)
		if ensureValidSpeaker(speakers, id) == id { // 登録済み話者のみ採用
			updateCfg(func(c *Config) {
				if which == "notify" {
					c.NotifySpeaker = id
				} else {
					c.Speaker = id
				}
			})
			if which == "notify" {
				go ensureNotifyCache()
			}
			logLine("voice speaker set: " + which + " = " + itoa(id))
		}
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handleServerAdd(w http.ResponseWriter, r *http.Request) {
	if blockedCrossOrigin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	_ = r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	u := strings.TrimSpace(r.FormValue("url"))
	if name != "" && (strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")) {
		updateCfg(func(c *Config) {
			if c.Servers == nil {
				c.Servers = map[string]string{}
			}
			c.Servers[name] = u
		})
		logLine("server added: " + name + " -> " + u)
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handleServerDelete(w http.ResponseWriter, r *http.Request) {
	if blockedCrossOrigin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	_ = r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	updateCfg(func(c *Config) {
		u, ok := c.Servers[name]
		if !ok {
			return
		}
		if u == c.Server { // 使用中は削除しない
			return
		}
		if len(c.Servers) <= 1 { // 最後の1つは残す
			return
		}
		delete(c.Servers, name)
		logLine("server deleted: " + name)
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handleServerUse(w http.ResponseWriter, r *http.Request) {
	if blockedCrossOrigin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	_ = r.ParseForm()
	u := strings.TrimSpace(r.FormValue("url"))
	// 登録済みサーバーのみ採用(任意URL採用によるSSRF/合成サーバーすり替えを防ぐ)
	known := false
	for _, v := range getCfg().Servers {
		if v == u {
			known = true
			break
		}
	}
	if u != "" && known {
		speakers, err := fetchSpeakers(u)
		updateCfg(func(c *Config) {
			c.Server = u
			if err == nil && len(speakers) > 0 {
				c.Speaker = ensureValidSpeaker(speakers, c.Speaker)
				c.NotifySpeaker = ensureValidSpeaker(speakers, c.NotifySpeaker)
			}
		})
		go ensureNotifyCache() // 新サーバー用の確認音キャッシュを用意
		logLine("server switched (web) to " + u)
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

# Claude TTS Tray

Claude Code（VSCode拡張・Obsidian Claudian・ターミナル等すべて）が応答を終えたとき、
最後の返答を**音声で読み上げる**常駐トレイアプリ。確認（permission等）の通知音も鳴らせる。

これまで `~/.claude/hooks/tts-notify.py`（Python）でやっていた読み上げを、
**Go製の単一exe + ローカルHTTPデーモン**に置き換えたもの。Pythonの毎回起動（〜200ms）が消え、
フックは約10msで完了する。設定（ON/OFF・サーバー・音声・確認音）はトレイメニューから変更できる。

## なぜ拡張機能ではなくフック+常駐なのか

VSCodeでもObsidian Claudianでも鳴るのは、どちらも内部で `claude` CLI を呼び、
CLIが共通の `~/.claude/settings.json` のフックを実行するから。フックはエディタ非依存。
VSCode拡張にすると **VSCode内でしか動かず Claudianで鳴らなくなる** ため、フック方式を維持している。

## 構成

```
フック (settings.json)                  常駐トレイ (claude-tts-tray.exe)
  Stop  → exe hook stop   ──┐
  Notification → exe hook notify ─┼─ HTTP POST ─→ 127.0.0.1:7331
                                  │                  ├ /stop   最後の返答を読み上げ
  ※ exe は stdin を転送するだけ    │                  ├ /notify 確認音
    （常に exit 0・高速起動）       │                  ├ /speak  任意テキスト
                                  │                  └ /ping   状態
                                  └─ 合成: AivisSpeech / VOICEVOX 互換API
                                     再生: winmm PlaySound
```

- **単一exe・二役**: 引数なし起動＝常駐トレイ、`hook <mode>` 起動＝極薄フッククライアント。
- **設定**: `~/.claude/tts-config.json`（トレイUIが書き、デーモンが読む唯一の設定源）。
- **ログ**: `~/.claude/tts-tray.log`。

## トレイメニュー

| 項目 | 内容 |
|------|------|
| 読み上げ 有効 | ON/OFF トグル（OFFで全停止、アイコンが灰色に） |
| 今すぐ停止 | 再生中の音声を即座に止める（トレイアイコン左クリックでも同じ） |
| サーバー | 登録済みサーバーを切替（AivisSpeechリモート/ローカル・VOICEVOXローカル等） |
| 　サーバーを追加・編集… | ブラウザで設定ページを開く（追加/削除/選択） |
| 音声（読み上げ） | 返答読み上げ(Stop)の話者を選択 |
| 音声（確認） | 確認通知(Notification)の話者を選択 |
| 確認音 | 発話「確認してください」(キャッシュで即時) / チャイム音 / なし |
| テスト発話（読み上げ） | 読み上げ話者で試聴 |
| テスト発話（確認） | 確認話者で試聴 |
| 再起動して反映 | サーバー追加・変更後にメニュー表示を更新 |
| 終了 | 常駐を終了 |

### トレイアイコンの操作

- **左クリック** … 今すぐ停止（再生中の音声を止める）
- **右クリック** … メニュー表示
- アイコンの色: **緑**=有効 / **灰**=無効 / **オレンジ(停止マーク■)**=発話中。
  発話中はアイコンが停止マークに変わるので、そのまま左クリックで止められる。

> **読み上げと確認で話者を別々に設定できる**（`speaker` と `notify_speaker`）。
> サーバーを切り替えると話者一覧が変わるため、音声メニューの表示更新には「再起動して反映」を使う
> （切替時に有効な話者へ自動補正は行う）。

## サーバー設定ページ（追加・編集ダイアログ）

トレイの「サーバーを追加・編集…」で、既定ブラウザに設定ページ（`http://127.0.0.1:7331/`）が開く。
ここで **サーバーの追加・削除・選択** ができる（JSON を手で編集しなくてよい）。

- 追加: 表示名とURL（`http://ホスト:ポート`）を入力して「追加」
- 選択: 各行の「使う」で即時切替（その時点で有効な話者へ自動補正）
- 削除: 「削除」（使用中・最後の1つは削除不可）
- 反映: 追加・削除・選択はその場で保存。トレイの「サーバー」「音声」メニュー**表示**の更新には「再起動して反映」を使う

> セキュリティ: ローカルHTTPは 127.0.0.1 限定バインドに加え、`Origin` ヘッダで
> ブラウザからのクロスオリジンPOSTを拒否する（localhostを狙うCSRF対策）。
> フック(curl)は Origin を送らないため通常どおり通る。

## 設定ファイル例

```json
{
	"enabled": true,
	"server": "http://127.0.0.1:50021",
	"speaker": 20,
	"notify_speaker": 13,
	"notify_mode": "speak",
	"notify_text": "確認してください",
	"max_chars": 600,
	"port": 7331,
	"servers": {
		"AivisSpeech (リモート)": "http://100.79.167.35:10101",
		"AivisSpeech (ローカル)": "http://127.0.0.1:10101",
		"VOICEVOX (ローカル)": "http://127.0.0.1:50021"
	}
}
```

- `speaker` … 読み上げ(Stop)の話者、`notify_speaker` … 確認(Notification)の話者。
- サーバーの選択肢は設定ページから追加できる（`servers` に「表示名: URL」が追記される）。

## ビルド

```bash
export PATH="$PATH:/c/Program Files/Go/bin"
cd /c/xampp/Project/claude-tts-tray
python temp/gen_icon.py                 # アイコン再生成（初回のみ）
CGO_ENABLED=0 go build -ldflags="-H=windowsgui -s -w" -o claude-tts-tray.exe .
```

`-H=windowsgui` でコンソール窓を出さない。CGO不要（Windowsのsystrayは純Go）。

## 合成エンジン（現用: ローカルVOICEVOX）

合成先は **このPCのローカル VOICEVOX ENGINE 0.25.2**（`http://127.0.0.1:50021`）。
- 読み上げ(Stop) = **もち子さん ノーマル**（speaker=20）
- 確認(Notification) = **青山龍星 ノーマル**（notify_speaker=13、キャッシュで即時）
- 実測レイテンシ: ウォーム時 短文で **約0.6〜0.75秒**（初回のみモデルロードで~1.7秒）。

### 遅延の背景
以前はリモートshibaの**AivisSpeech**で合成しており、短文でも synthesis に約2.6秒（テキスト長に依存しない固定コスト、ネットワークは数十ms）かかっていた。
ローカルVOICEVOX（軽量なHiFi-GAN系）に移行し、往復ゼロ＋高速化で大幅改善。AivisSpeechの高音質な花音に戻したい場合はトレイ「サーバー」からshibaを選べる（可逆）。

> Intel第12世代以降のP/E混在CPUでは合成スレッドが遅いEコアに載ると悪化するため、エンジンは
> `--cpu_num_threads 15`（論理コアの約3/4）で起動している。

### ローカルVOICEVOXエンジン
- 本体: `C:\xampp\Project\voicevox-engine\windows-cpu\run.exe`
- 起動: `run.exe --host 127.0.0.1 --port 50021 --cpu_num_threads 15`
- 隠し常駐: `C:\xampp\Project\voicevox-engine\start-voicevox-hidden.vbs`（wscriptでコンソール窓なし起動）

### 確認音キャッシュ
確認の文言は固定なので、**一度だけ合成して `~/.claude/tts-cache/` にWAVキャッシュ**し、以降はファイルを即再生する。
起動時・確認話者/サーバー変更時にバックグラウンドで先に用意する。

### 読み上げの整形（コード・URLは読まない）
`cleanForSpeech` が読み上げ前に以下を除去する: コードブロック(```...```)・インラインコード(`` `...` ``)・
URL（http(s)・`www.`・ドメイン+TLD）・Windows/Unixパス・Markdownリンク/画像・見出し/箇条書き記号。
バージョン番号や `Node.js`・`and/or` などの通常文は残す（`clean_test.go` で検証済み）。

## 重要: フックはデーモン必須

旧 `tts-notify.py` は各フックでPythonがTTSを直接実行していたが、本方式は
**常駐デーモンが動いていないと無音**（フックは `curl` 失敗で即 exit 0 する＝セッションは壊さないが喋らない）。
つまり常駐の起動忘れ＝TTS停止。必ず自動起動を有効化しておくこと。

## 自動起動（ログイン時）

- 有効化: `autostart-setup.cmd` をダブルクリック（**VOICEVOXエンジン**＋**トレイ**の両方をHKCU Runに登録し、その場で起動）
- 解除: `autostart-remove.cmd` をダブルクリック（両方を解除）

レジストリは `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` の `ClaudeTTSTray`（トレイ）と `VoicevoxEngine`（`wscript`でvbs→run.exe）。ユーザー単位・可逆。

## settings.json のフック

```jsonc
"Notification": [{ "matcher": "*", "hooks": [
  { "type": "command", "command": "\"C:\\xampp\\Project\\claude-tts-tray\\claude-tts-tray.exe\" hook notify", "timeout": 30, "async": true }
]}]
"Stop": [ /* ... */
  { "type": "command", "command": "\"C:\\xampp\\Project\\claude-tts-tray\\claude-tts-tray.exe\" hook stop", "timeout": 180, "async": true }
]
```

cmd.exe・bash どちらのシェルから実行されても動作することを確認済み。

## 旧実装

`~/.claude/hooks/tts-notify.py` は残してあり、手動フォールバックとして使える。
settings.json のフックを上記exe方式から戻せば再び有効になる。

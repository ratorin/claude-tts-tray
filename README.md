# Claude TTS Tray

Claude Code（VSCode拡張・Obsidian Claudian・ターミナル等すべて）が応答を終えたときや
確認（質問・許可など）の場面で**音を鳴らす**常駐トレイアプリ。Go製の単一exe + ローカルHTTPデーモン。

## 既定の動作（サーバー不要）

インストール直後は **音声合成サーバー無し** で、**効果音だけ**鳴ります（エンジン導入不要で即使える）:
- 応答完了（Stop）→ 完了の効果音
- 確認（質問/許可/アイドル）→ 通知の効果音

**読み上げ（音声）にしたい場合**は、トレイの「サーバーを追加・編集…」から
**VOICEVOX / AivisSpeech**（VOICEVOX互換API）を接続して選ぶと、最後の返答を読み上げ・確認時に発話します。
話者は読み上げ用・確認用で別々に選べます。設定（ON/OFF・サーバー・音声・確認音）はトレイメニューから変更可。

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

### フォルダ構成

```
claude-tts-tray/
├── *.go                 Goソース(package main: tray/server/tts/hook/config/sys_*/icons_*/sounds/util)
├── go.mod, go.sum
├── README.md, LICENSE
├── assets/              埋め込み素材(icon_*.ico/.png, sound_*.wav)
├── scripts/             ビルド/インストール/自動起動/素材生成
│   ├── build-linux.sh, install-linux.sh, uninstall-linux.sh
│   └── gen_icon.py, gen_sounds.py
├── dist/                ビルド成果物(gitignore)
└── temp/                作業用スクラッチ(gitignore)
```

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

## 設定ファイル `~/.claude/tts-config.json`

既定（サーバー無し＝効果音のみ）:

```json
{
	"enabled": true,
	"server": "",
	"notify_mode": "chime",
	"port": 7331,
	"servers": {
		"AivisSpeech (ローカル)": "http://127.0.0.1:10101",
		"VOICEVOX (ローカル)": "http://127.0.0.1:50021"
	}
}
```

サーバーを接続して読み上げ・発話にした例:

```json
{
	"enabled": true,
	"server": "http://127.0.0.1:50021",
	"speaker": 20,
	"notify_speaker": 13,
	"notify_mode": "speak",
	"notify_text": "確認してください"
}
```

- **`server` が空** … 効果音のみ（Stop=完了音 / Notification=通知音、合成不要）。
- **`server` を設定** … 読み上げ(Stop)。`notify_mode:"speak"` で確認も発話、`"chime"` で効果音、`"none"` で無音。
- `speaker` … 読み上げの話者、`notify_speaker` … 確認(発話)の話者。サーバーの話者IDを指定。
- サーバーの選択肢は設定ページ（トレイ「サーバーを追加・編集…」）から追加でき、`servers` に追記される。

## ビルド

```bash
export PATH="$PATH:/c/Program Files/Go/bin"
cd /c/xampp/Project/claude-tts-tray
python scripts/gen_icon.py                 # アイコン再生成（.ico+.png、初回のみ）
CGO_ENABLED=0 go build -ldflags="-H=windowsgui -s -w" -o claude-tts-tray.exe .
```

`-H=windowsgui` でコンソール窓を出さない。CGO不要（Windowsのsystrayは純Go）。

## クロスプラットフォーム / Linux版

コードはOS非依存部（HTTPデーモン・フック・合成・整形・トランスクリプト解析）と、
ビルドタグで分離したOS依存部からなる:

| 機能 | Windows (`sys_windows.go`/`icons_windows.go`) | Linux (`sys_linux.go`/`icons_linux.go`) |
|------|------|------|
| 音声再生 | winmm `PlaySoundW` | `paplay` / `ffplay` / `aplay` にシェルアウト |
| 停止 | `PlaySound(NULL)` | 再生プロセスを kill |
| トレイアイコン | `.ico` 埋め込み | `.png` 埋め込み |
| トレイ | systray(純Go/win32) | systray(純Go/**DBus StatusNotifierItem**) |
| ブラウザ起動 | `rundll32` | `xdg-open` |

**Linuxビルド（cgo不要・静的リンク。Windows上からクロスビルド可）:**
```bash
./scripts/build-linux.sh        # → dist/claude-tts-tray-linux-amd64 (ELF, static)
```

**Linuxへインストール（デスクトップ環境が必要）:**
```bash
./scripts/install-linux.sh      # ~/.local/bin に配置 + XDG autostart 登録
./scripts/uninstall-linux.sh    # 削除
```
- 音声プレイヤー（`paplay`/`ffplay`/`aplay` のいずれか）が必要。
- トレイ表示には StatusNotifierItem ホスト（KDE/最近のGNOMEは拡張）が必要。
- フック（`~/.claude/settings.json`）は `<path>/claude-tts-tray hook stop` / `hook notify`（絶対パス）。
- `~/.claude/tts-config.json` の `server` を、そのマシンから到達可能な合成サーバーに設定する
  （ローカルにエンジンが無い場合はネットワーク上のVOICEVOX/AivisSpeechを指す）。

## 遅延の目安（合成エンジン・スペック別）

読み上げ開始までの待ち時間は、ほぼ合成エンジンの処理時間で決まる（ネットワークは数十ms）。
短文（十数文字）の参考値:

| エンジン | 実行環境 | 短文の目安 |
|---------|---------|-----------|
| VOICEVOX | CPU（最新デスクトップ, 例: Intel 第13世代 20スレッド） | 約0.6〜0.8秒（ウォーム）／初回 ~1.7秒（モデルロード） |
| VOICEVOX | CPU（低スペック / VPS） | 数秒 |
| VOICEVOX | GPU（NVIDIA） | サブ秒（CPU比 約25〜30倍速） |
| AivisSpeech | CPU | 約3〜5秒（BERT前処理が重く固定コスト大。長文は数十秒） |
| AivisSpeech | GPU（DirectML / CUDA） | 約1.5〜2秒 |

- **軽量・低遅延を優先 → VOICEVOX**、**高音質を優先 → AivisSpeech**（どちらもVOICEVOX互換API、トレイ「サーバー」で切替可）。
- GPUの無いCPUのみ環境では VOICEVOX が現実的に最速。**確認音はキャッシュ**されるため初回以降は即時（後述）。
- Intel 第12世代以降のP/Eコア混在CPUでは、合成スレッドが遅いEコアに載ると悪化する。
  VOICEVOXは起動オプション `--cpu_num_threads`（論理コアの約3/4）や、電源プランを「最適なパフォーマンス」にして回避できる。

> 参考値は環境差が大きい目安。VOICEVOX/AivisSpeech は公式サイトからダウンロードして起動し、
> トレイの「サーバーを追加・編集…」でそのURL（例 `http://127.0.0.1:50021`）を登録する。

### 確認音キャッシュ
確認の文言は固定なので、**一度だけ合成して `~/.claude/tts-cache/` にWAVキャッシュ**し、以降はファイルを即再生する。
起動時・確認話者/サーバー変更時にバックグラウンドで先に用意する。

### 読み上げの整形（コード・URLは読まない）
`cleanForSpeech` が読み上げ前に以下を除去する: コードブロック(```...```)・インラインコード(`` `...` ``)・
URL（http(s)・`www.`・ドメイン+TLD）・Windows/Unixパス・Markdownリンク/画像・見出し/箇条書き記号。
バージョン番号や `Node.js`・`and/or` などの通常文は残す（`clean_test.go` で検証済み）。

## 重要: フックはデーモン必須

フックは極薄クライアントで、合成・再生は常駐デーモンが担う。そのため
**常駐デーモンが動いていないと無音**（フックは接続失敗で即 exit 0 ＝セッションは壊さないが喋らない）。
つまり常駐の起動忘れ＝音が出ない。必ず自動起動を有効化しておくこと。

## 自動起動（ログイン時）

トレイメニューの **「ログイン時に自動起動」** をオン/オフするだけ（**レジストリ不使用**・アプリ内で完結）:
- **Windows**: スタートアップフォルダに `.lnk` ショートカットを作成/削除（IShellLink）
- **Linux**: `~/.config/autostart/claude-tts-tray.desktop` を作成/削除

どちらもファイルを置くだけなので、エクスプローラ/ファイラから見えて手動でも消せる。

> 合成エンジン（VOICEVOX/AivisSpeech）を別途使う場合、エンジン側の自動起動は各エンジンで設定する
> （このアプリの自動起動はトレイ常駐のみを対象）。

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

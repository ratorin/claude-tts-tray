package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Config は読み上げの設定。~/.claude/tts-config.json に保存される。
// フック(curl)・トレイUI・本デーモンが共有する唯一の設定源。
type Config struct {
	Enabled       bool              `json:"enabled"`        // 読み上げ ON/OFF
	Server        string            `json:"server"`         // 合成サーバーURL (AivisSpeech / VOICEVOX 互換)
	Speaker       int               `json:"speaker"`        // 読み上げ(Stop)の話者スタイルID
	NotifySpeaker int               `json:"notify_speaker"` // 確認(Notification)の話者スタイルID
	NotifyMode    string            `json:"notify_mode"`    // 確認音: speak | chime | none
	NotifyText    string            `json:"notify_text"`    // speak時に読み上げる文言
	MaxChars      int               `json:"max_chars"`      // 読み上げ最大文字数
	Port          int               `json:"port"`           // ローカル待受ポート
	Servers       map[string]string `json:"servers"`        // 選択肢(表示名 -> URL)
}

var (
	cfg    Config
	cfgMu  sync.RWMutex
	saveMu sync.Mutex // saveConfig の直列化(一時ファイル衝突防止)
)

// configPath は設定ファイルの絶対パスを返す。
func configPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".claude", "tts-config.json")
}

// defaultConfig は初回起動時の既定値。
func defaultConfig() Config {
	return Config{
		Enabled:       true,
		Server:        "http://127.0.0.1:10101", // ローカルのAivisSpeech既定。VOICEVOXなら50021
		Speaker:       1325133120,               // AivisSpeech 花音 / ノーマル (読み上げ)
		NotifySpeaker: 1325133120,               // 確認(初期値は読み上げと同じ)
		NotifyMode:    "speak",
		NotifyText:    "確認してください",
		MaxChars:      600,
		Port:          7331,
		Servers: map[string]string{
			"AivisSpeech (ローカル)": "http://127.0.0.1:10101",
			"VOICEVOX (ローカル)":    "http://127.0.0.1:50021",
		},
	}
}

// loadConfig は設定を読み込む。無ければ既定値で作成する。
// 既存ファイルに欠けたフィールドは既定値で補完する。
func loadConfig() {
	def := defaultConfig()
	c := def

	data, err := os.ReadFile(configPath())
	if err == nil {
		_ = json.Unmarshal(data, &c)
	}
	// 必須フィールドの補完
	if c.Server == "" {
		c.Server = def.Server
	}
	if c.NotifyMode == "" {
		c.NotifyMode = def.NotifyMode
	}
	if c.NotifyText == "" {
		c.NotifyText = def.NotifyText
	}
	if c.MaxChars <= 0 {
		c.MaxChars = def.MaxChars
	}
	if c.Speaker <= 0 {
		c.Speaker = def.Speaker
	}
	if c.NotifySpeaker <= 0 {
		c.NotifySpeaker = c.Speaker // 未設定なら読み上げと同じ話者を継承
	}
	if c.Port <= 0 {
		c.Port = def.Port
	}
	if len(c.Servers) == 0 {
		c.Servers = def.Servers
	}

	cfgMu.Lock()
	cfg = c
	cfgMu.Unlock()

	if err != nil {
		saveConfig() // 初回は書き出して可視化
	}
}

// saveConfig は現在の設定をディスクに書き出す。
// 一意な一時ファイル経由で原子的に差し替える。失敗時はログに記録し残骸を片付ける。
func saveConfig() {
	saveMu.Lock()
	defer saveMu.Unlock()

	cfgMu.RLock()
	data, err := json.MarshalIndent(cfg, "", "\t")
	cfgMu.RUnlock()
	if err != nil {
		logLine("saveConfig marshal failed: " + err.Error())
		return
	}

	dir := filepath.Dir(configPath())
	_ = os.MkdirAll(dir, 0o755)

	f, err := os.CreateTemp(dir, "tts-config-*.tmp")
	if err != nil {
		logLine("saveConfig CreateTemp failed: " + err.Error())
		return
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		logLine("saveConfig write failed: " + err.Error())
		return
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		logLine("saveConfig close failed: " + err.Error())
		return
	}
	if err := os.Rename(tmp, configPath()); err != nil {
		// Windowsでは宛先が他プロセスにロックされていると失敗しうる
		logLine("saveConfig rename failed: " + err.Error())
		os.Remove(tmp)
	}
}

// getCfg は設定のスナップショット(コピー)を返す。
// Servers マップはディープコピーして共有参照を断つ(並行アクセス安全)。
func getCfg() Config {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	c := cfg
	if cfg.Servers != nil {
		m := make(map[string]string, len(cfg.Servers))
		for k, v := range cfg.Servers {
			m[k] = v
		}
		c.Servers = m
	}
	return c
}

// updateCfg はロック下で設定を更新し保存する。
func updateCfg(fn func(c *Config)) {
	cfgMu.Lock()
	fn(&cfg)
	cfgMu.Unlock()
	saveConfig()
}

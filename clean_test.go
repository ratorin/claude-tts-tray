package main

import "testing"

// cleanForSpeech がコード/URL/パスを除去し、地の文は残すことを検証する。
func TestCleanForSpeech(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		mustGone  []string // 出力に含まれてはいけない断片
		mustKeep  []string // 出力に残るべき断片
	}{
		{
			name:     "fenced code block",
			in:       "修正しました。\n```go\nfunc main() { fmt.Println(\"x\") }\n```\n完了です。",
			mustGone: []string{"func main", "Println", "```"},
			mustKeep: []string{"修正しました", "完了です"},
		},
		{
			name:     "inline code",
			in:       "`cancelCurrent()` を呼びます。",
			mustGone: []string{"cancelCurrent", "`"},
			mustKeep: []string{"を呼びます"},
		},
		{
			name:     "https url",
			in:       "詳細は https://example.com/path?q=1 を参照。",
			mustGone: []string{"https", "example.com", "path"},
			mustKeep: []string{"詳細は", "を参照"},
		},
		{
			name:     "bare url and www",
			in:       "github.com/VOICEVOX/voicevox_engine と www.example.jp を見て。",
			mustGone: []string{"github.com", "voicevox_engine", "www.example.jp"},
			mustKeep: []string{"を見て"},
		},
		{
			name:     "unix and windows paths",
			in:       "/usr/local/bin/run と C:\\xampp\\php を確認。",
			mustGone: []string{"/usr/local/bin", "xampp", "php"},
			mustKeep: []string{"を確認"},
		},
		{
			name:     "keep normal prose with dots and slash",
			in:       "バージョンは1.2.3です。Node.jsとand/orは残す。",
			mustGone: []string{},
			mustKeep: []string{"1.2.3", "Node.js", "and/or", "残す"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := cleanForSpeech(c.in, 600)
			for _, g := range c.mustGone {
				if contains(out, g) {
					t.Errorf("[%s] 除去されるべき %q が残った: out=%q", c.name, g, out)
				}
			}
			for _, k := range c.mustKeep {
				if !contains(out, k) {
					t.Errorf("[%s] 残るべき %q が消えた: out=%q", c.name, k, out)
				}
			}
		})
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

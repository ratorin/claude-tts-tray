package main

import _ "embed"

// 合成エンジン不要の既定効果音(WindowsもLinuxも同じWAVを再生)。
// サーバー未設定時に、読み上げ/発話の代わりに鳴らす。

//go:embed sound_done.wav
var soundDone []byte

//go:embed sound_notify.wav
var soundNotify []byte

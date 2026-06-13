//go:build !windows

package main

import _ "embed"

//go:embed assets/icon_on.png
var iconOn []byte

//go:embed assets/icon_off.png
var iconOff []byte

//go:embed assets/icon_speaking.png
var iconSpeaking []byte

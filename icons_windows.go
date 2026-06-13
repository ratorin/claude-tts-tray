//go:build windows

package main

import _ "embed"

//go:embed assets/icon_on.ico
var iconOn []byte

//go:embed assets/icon_off.ico
var iconOff []byte

//go:embed assets/icon_speaking.ico
var iconSpeaking []byte

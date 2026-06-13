//go:build windows

package main

import _ "embed"

//go:embed icon_on.ico
var iconOn []byte

//go:embed icon_off.ico
var iconOff []byte

//go:embed icon_speaking.ico
var iconSpeaking []byte

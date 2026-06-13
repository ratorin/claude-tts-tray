//go:build !windows

package main

import _ "embed"

//go:embed icon_on.png
var iconOn []byte

//go:embed icon_off.png
var iconOff []byte

//go:embed icon_speaking.png
var iconSpeaking []byte

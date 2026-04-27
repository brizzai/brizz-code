package main

import (
	"github.com/brizzai/fleet/internal/chrome"
	"github.com/brizzai/fleet/internal/debuglog"
)

// handleChromeHost runs the native messaging host for the Chrome extension.
// Invoked by Chrome when the extension calls connectNative().
func handleChromeHost() {
	debuglog.Init()
	defer debuglog.Close()
	debuglog.Logger.Info("chrome-host starting")

	chrome.RunNativeHost()
}

package main

import (
	"github.com/brizzai/brizz-code/internal/chrome"
	"github.com/brizzai/brizz-code/internal/debuglog"
)

// handleChromeHost runs the native messaging host for the Chrome extension.
// Invoked by Chrome when the extension calls connectNative().
func handleChromeHost() {
	debuglog.Init()
	defer debuglog.Close()
	debuglog.Logger.Info("chrome-host starting")

	chrome.RunNativeHost()
}

//go:build gui && linux && cgo

package main

import (
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

func applyPlatformOptions(o *options.App) {
	// GTK compositors honour a transparent window but not the Windows acrylic API. The panel is
	// small and static, so software rendering costs nothing and sidesteps the blank or flickering
	// WebKit views Linux desktops report with GPU acceleration; it is also what Wails picks when
	// Linux options are absent (wails#2977).
	o.Linux = &linux.Options{
		WindowIsTranslucent: true,
		WebviewGpuPolicy:    linux.WebviewGpuPolicyNever,
	}
}

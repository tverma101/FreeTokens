//go:build gui && windows

package main

import (
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// The popover uses the native acrylic backdrop so the rounded CSS shell sits on a
// real translucent surface instead of a flat rectangle.
func applyPlatformOptions(o *options.App) {
	o.Windows = &windows.Options{
		WebviewIsTransparent: true,
		WindowIsTranslucent:  true,
		BackdropType:         windows.Acrylic,
		Theme:                windows.Dark,
	}
}

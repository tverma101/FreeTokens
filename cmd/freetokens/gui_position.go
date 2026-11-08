//go:build gui && (windows || (linux && cgo))

package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// windowSize returns the pixel size the window manager actually gave the popover,
// which includes the display's DPI scaling.
func windowSize(ctx context.Context) (int, int) {
	w, h := runtime.WindowGetSize(ctx)
	if w <= 0 || h <= 0 {
		return windowWidth, windowHeight
	}
	return w, h
}

// topRight is the fallback anchor when the tray position is unavailable: the
// top-right corner of the primary display, just below the menu bar.
func topRight(ctx context.Context) (int, int) {
	w := 1280
	if screens, err := runtime.ScreenGetAll(ctx); err == nil {
		for _, s := range screens {
			if s.IsPrimary {
				w = s.Size.Width
				break
			}
		}
	}
	width, _ := windowSize(ctx)
	if w < width+32 {
		w = width + 32
	}
	return w - width - 16, 40
}

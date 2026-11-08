//go:build gui && windows

package main

import (
	"context"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32              = windows.NewLazySystemDLL("user32.dll")
	procGetCursorPos    = user32.NewProc("GetCursorPos")
	procMonitorFromRect = user32.NewProc("MonitorFromRect")
	procGetMonitorInfo  = user32.NewProc("GetMonitorInfoW")
)

type nativePoint struct{ X, Y int32 }

type nativeRect struct{ Left, Top, Right, Bottom int32 }

type monitorInfo struct {
	CbSize    uint32
	RcMonitor nativeRect
	RcWork    nativeRect
	DwFlags   uint32
}

// windowPosition puts the popover in the bottom-right corner of the monitor holding
// the pointer, above the taskbar. The work area already excludes the taskbar, so no
// hardcoded screen geometry is needed for multi-monitor or scaled displays.
func windowPosition(ctx context.Context) (int, int) {
	var p nativePoint
	ok, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	if ok == 0 {
		return topRight(ctx)
	}
	// MonitorFromRect takes a pointer, unlike MonitorFromPoint's by-value POINT.
	r := nativeRect{Left: p.X, Top: p.Y, Right: p.X + 1, Bottom: p.Y + 1}
	const monitorDefaultToNearest = 2
	hmon, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), monitorDefaultToNearest)
	if hmon == 0 {
		return topRight(ctx)
	}
	info := monitorInfo{CbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
	got, _, _ := procGetMonitorInfo.Call(hmon, uintptr(unsafe.Pointer(&info)))
	if got == 0 {
		return topRight(ctx)
	}
	w, h := windowSize(ctx)
	return int(info.RcWork.Right) - w - 8, int(info.RcWork.Bottom) - h - 8
}

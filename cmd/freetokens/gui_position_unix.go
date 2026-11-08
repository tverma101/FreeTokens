//go:build gui && linux && cgo

package main

import "context"

func windowPosition(ctx context.Context) (int, int) { return topRight(ctx) }

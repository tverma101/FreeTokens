//go:build gui && !production

package main

// Wails compiles a desktop stub without the production tag: the binary starts, shows an error
// dialog and exits instead of opening the popover. Build with `-tags "gui production"`; the
// undefined name below turns the missing tag into a build error, not a runtime dialog.
var _ = guiBuildsNeedTheProductionTag

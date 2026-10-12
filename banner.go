package jig

import (
	"fmt"
	"io"
	"runtime"
	"strings"
)

// Version is the framework version. Release builds set it with
//
//	-ldflags "-X github.com/jigframework/jig.Version=v1.2.3"
//
// and it defaults to "dev" otherwise.
var Version = "dev"

// banner is the ASCII art printed when an application starts. It is wide
// enough to be recognisable and short enough not to push the first real log
// line off a small terminal.
const banner = `
   _ _
  (_|_) __ _
  | | |/ _` + "`" + ` |
  | | | (_| |
 _/ |_|\__, |
|__/   |___/
`

// writeBanner prints the startup banner followed by a status line naming the
// application, the framework version and the Go runtime.
func writeBanner(w io.Writer, app string, resources int) {
	noun := "resources"
	if resources == 1 {
		noun = "resource"
	}
	fmt.Fprint(w, strings.TrimLeft(banner, "\n"))
	fmt.Fprintf(w, " :: jig %s ::  %s  (%d %s, %s)\n\n", Version, app, resources, noun, runtime.Version())
}

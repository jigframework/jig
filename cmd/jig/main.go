// Command jig is the command-line interface for the jig framework.
//
//	jig new <name>   scaffold a new application
//	jig version      print the version
//
// A jig application is an ordinary Go binary; there is nothing to generate
// and the CLI is only a convenience for starting a project.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jigframework/jig"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "jig:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch args[0] {
	case "new":
		return cmdNew(ctx, args[1:])
	case "version", "-v", "--version":
		fmt.Println("jig", jig.Version)
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `jig is an opinionated framework for CRUD and SaaS applications in Go.

Usage:
  jig <command> [arguments]

Commands:
  new <name>   scaffold a new application
  version      print the version
`)
}

func notImplemented(cmd string) error {
	return fmt.Errorf("%s is not implemented yet", cmd)
}

func cmdNew(_ context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: jig new <name>")
	}
	return notImplemented("new")
}

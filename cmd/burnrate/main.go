package main

import (
	"fmt"
	"os"
)

var Version = "dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: burnrate <serve|status|accounts|resume|token|recover|version>")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "version":
		runVersion()
	case "status":
		runStatus()
	case "accounts":
		runAccounts()
	case "serve":
		runServe()
	case "resume":
		runResume(os.Args[2:])
	case "token":
		runToken()
	case "recover":
		runRecover()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

package main

import (
	"fmt"
	"os"

	"github.com/loq/lm-get/internal/api"
	"github.com/loq/lm-get/internal/cache"
	"github.com/loq/lm-get/internal/cli"
	"github.com/loq/lm-get/internal/config"
	"github.com/loq/lm-get/internal/display"
	"github.com/loq/lm-get/internal/tui"
)

var version = "0.9.1"

func main() {
	api.Version = version

	if len(os.Args) < 2 {
		if err := tui.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "%s[ERROR]%s %v\n", display.Red, display.Reset, err)
			os.Exit(1)
		}
		return
	}

	switch os.Args[1] {
	case "search":
		cli.CmdSearch(os.Args[2:])
	case "info":
		cli.CmdInfo(os.Args[2:])
	case "download":
		cli.CmdDownload(os.Args[2:])
	case "list":
		cli.CmdList(os.Args[2:])
	case "remove":
		cli.CmdRemove(os.Args[2:])
	case "cache":
		cache.CmdCache(os.Args[2:])
	case "config":
		if len(os.Args) > 2 && os.Args[2] == "init" {
			config.CmdConfigInit()
		} else {
			fmt.Println("Usage: lm-get config init")
		}
	case "help", "--help", "-h":
		cli.PrintHelp()
	case "version", "--version", "-v":
		fmt.Println("lm-get", version)
	default:
		fmt.Fprintf(os.Stderr, "%sUnknown command: %s%s\n", display.Red, os.Args[1], display.Reset)
		cli.PrintHelp()
		os.Exit(1)
	}
}

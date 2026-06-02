package main

import (
	"fmt"
	"os"
)

const version = "0.3.0"

func main() {
	if len(os.Args) < 2 {
		if err := runTUI(); err != nil {
			fmt.Fprintf(os.Stderr, "%s[ERROR]%s %v\n", red, reset, err)
			os.Exit(1)
		}
		return
	}

	switch os.Args[1] {
	case "search":
		cmdSearch(os.Args[2:])
	case "info":
		cmdInfo(os.Args[2:])
	case "download":
		cmdDownload(os.Args[2:])
	case "list":
		cmdList(os.Args[2:])
	case "remove":
		cmdRemove(os.Args[2:])
	case "cache":
		cmdCache(os.Args[2:])
	case "config":
		if len(os.Args) > 2 && os.Args[2] == "init" {
			cmdConfigInit()
		} else {
			fmt.Println("Usage: lm-get config init")
		}
	case "help", "--help", "-h":
		printHelp()
	case "version", "--version", "-v":
		fmt.Println("lm-get", version)
	default:
		fmt.Fprintf(os.Stderr, "%sUnknown command: %s%s\n", red, os.Args[1], reset)
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Printf(`%slm-get%s - Search and download GGUF models from Hugging Face

%sUsage:%s
  lm-get                Interactive TUI mode (default)
  lm-get <command>      CLI mode

%sCommands:%s
  search   <query>             Search for GGUF models
  info     <user/repo>         List quantizations for a model
  download <user/repo> <file>  Download a GGUF file
  list                         List downloaded models
  remove   <user/repo>         Remove a downloaded model
  cache    [clear|info]        Manage API cache
  config   init                Create default config file

%sOptions:%s
  -n <int>         Number of search results (default: 20)
  --page, -p <int> Page number for pagination
  --sort <field>   Sort by: downloads, likes, lastModified
  --direction <d>  Sort direction: -1 (desc), 1 (asc)
  -o <dir>         Override download directory
  --dry-run        Simulate download without writing files
  --help, -h       Show this help
  --version, -v    Show version

%sExamples:%s
  lm-get                              Interactive mode
  lm-get search qwen                  Search for qwen models
  lm-get search qwen --sort likes     Sort by likes
  lm-get search qwen --page 2         Second page of results
  lm-get info unsloth/Qwen3-5B-GGUF   Show available files
  lm-get list                         Show downloaded models
  lm-get remove unsloth/Qwen3-5B-GGUF Delete a model
  lm-get cache clear                  Clear API cache
`, bold, reset, bold, reset, bold, reset, bold, reset, bold, reset)
}

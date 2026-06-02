package main

import (
	"fmt"
)

func cmdInfo(args []string) {
	if len(args) == 0 {
		printError("Usage: lm-get info <user/repo>")
		return
	}

	repo := args[0]
	rawFiles, err := listFiles(repo)
	if err != nil {
		printError("Failed to list files: %v", err)
		return
	}

	modelFiles := parseModelFiles(rawFiles)
	entries := buildDetailEntries(modelFiles, repo)

	if len(entries) == 0 {
		fmt.Printf("%sNo GGUF files found in %s%s\n", yellow, repo, reset)
		return
	}

	totalRAM := getSystemRAM()
	freeDisk := getDiskFree(expandHome(loadConfig().DownloadsDir))

	fmt.Printf("\n%s%s%s — %d quantization(s)%s\n\n", bold, repo, reset, len(entries), reset)

	for i, e := range entries {
		tags := ""
		if e.IsMmproj {
			tags += yellow + " [mmproj]" + reset
		}
		if e.IsShard {
			tags += blue + fmt.Sprintf(" [%d shards]", e.ShardTotal) + reset
		}
		if e.Downloaded {
			tags += green + " [✓ downloaded]" + reset
		}

		color := ramColor(e.TotalSize, totalRAM)
		ramTag := ""
		if color == green {
			ramTag = green + " ✓fits" + reset
		} else if color == red {
			ramTag = red + " ✗RAM" + reset
		}

		diskTag := ""
		if freeDisk >= 0 {
			if e.TotalSize > freeDisk {
				diskTag = red + " ✗disk" + reset
			}
		}

		name := truncate(e.DisplayName, 50)
		fmt.Printf("  %s%2d%s  %-52s  %s%-10s%s%s%s\n",
			cyan, i+1, reset,
			name,
			color, formatSize(e.TotalSize), reset,
			ramTag, tags+diskTag,
		)
	}

	fmt.Printf("\n%sUse 'lm-get download %s <file>' to download%s\n", gray, repo, reset)
}

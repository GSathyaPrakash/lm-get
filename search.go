package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func cmdSearch(args []string) {
	if len(args) == 0 {
		printError("Usage: lm-get search <query>")
		return
	}

	query := args[0]
	limit := 20
	sortBy := ""
	direction := ""
	page := 1

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "-n":
			if i+1 < len(args) {
				if n, err := strconv.Atoi(args[i+1]); err == nil && n > 0 {
					limit = n
					i++
				}
			}
		case "--sort":
			if i+1 < len(args) {
				sortBy = args[i+1]
				i++
			}
		case "--direction":
			if i+1 < len(args) {
				direction = args[i+1]
				i++
			}
		case "--page", "-p":
			if i+1 < len(args) {
				if p, err := strconv.Atoi(args[i+1]); err == nil && p > 0 {
					page = p
					i++
				}
			}
		}
	}

	cfg := loadConfig()
	if sortBy == "" {
		sortBy = cfg.DefaultSort
	}
	if direction == "" {
		direction = "-1"
	}

	models, err := searchModels(query, limit, sortBy, direction, page)
	if err != nil {
		printError("Search failed: %v", err)
		return
	}

	if len(models) == 0 {
		if page > 1 {
			fmt.Printf("%sNo more results for '%s' (page %d)%s\n", yellow, query, page, reset)
		} else {
			fmt.Printf("%sNo GGUF models found for '%s'%s\n", yellow, query, reset)
		}
		return
	}

	sortLabel := sortBy
	switch sortBy {
	case "downloads":
		sortLabel = "downloads"
	case "likes":
		sortLabel = "likes"
	case "lastModified":
		sortLabel = "last modified"
	}

	fmt.Printf("\n%sSearch results for '%s'%s (page %d, sorted by %s)\n\n", bold, query, reset, page, sortLabel)

	for i, m := range models {
		t, _ := time.Parse(time.RFC3339, m.LastModified)
		relTime := ""
		if !t.IsZero() {
			relTime = relativeTime(t)
		}

		ggufCount := 0
		for _, s := range m.Siblings {
			if strings.HasSuffix(strings.ToLower(s.RFilename), ".gguf") {
				ggufCount++
			}
		}

		name := truncate(m.ID, 50)
		num := (page-1)*limit + i + 1

		fmt.Printf("  %s%2d%s  %s%-52s%s  %s↓%s %-8s  %s♥%s %-8s  %s🗎%s %d GGUF  %s%s%s\n",
			cyan, num, reset,
			bold, name, reset,
			blue, reset, formatNumber(m.Downloads),
			red, reset, formatNumber(m.Likes),
			gray, reset, ggufCount,
			gray, relTime, reset,
		)
	}

	fmt.Printf("\n%sUse 'lm-get info <user/repo>' to see available files%s\n", gray, reset)
	if len(models) == limit {
		fmt.Printf("%sUse --page %d for next page%s\n", gray, page+1, reset)
	}
}

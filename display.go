package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	blue   = "\033[34m"
	cyan   = "\033[36m"
	gray   = "\033[90m"
)

func formatNumber(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d mins ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%d months ago", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%d years ago", int(d.Hours()/(24*365)))
	}
}

func progressBar(current, total int64, width int) string {
	if total == 0 {
		return ""
	}
	pct := float64(current) / float64(total)
	filled := int(pct * float64(width))
	if filled > width {
		filled = width
	}
	bar := strings.Builder{}
	bar.WriteString("[")
	for i := 0; i < width; i++ {
		if i < filled {
			bar.WriteString("=")
		} else {
			bar.WriteString(" ")
		}
	}
	bar.WriteString("]")
	return fmt.Sprintf("%s %s/%s %.0f%%", bar.String(), formatSize(current), formatSize(total), pct*100)
}

func ramColor(fileSize int64, totalRAM int64) string {
	if totalRAM <= 0 {
		return ""
	}
	if fileSize <= totalRAM {
		return green
	}
	return red
}

func printError(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s[ERROR]%s %s\n", red, reset, fmt.Sprintf(format, args...))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max < 4 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func scrollText(s string, max int, offset int) string {
	if len(s) <= max {
		return s
	}
	if offset >= len(s) {
		offset = 0
	}
	end := offset + max
	if end > len(s) {
		end = len(s)
	}
	return s[offset:end]
}

func dirSize(path string) int64 {
	var size int64
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil {
			if e.IsDir() {
				size += dirSize(filepath.Join(path, e.Name()))
			} else {
				size += info.Size()
			}
		}
	}
	return size
}

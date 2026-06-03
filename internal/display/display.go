package display

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	Reset  = "\033[0m"
	Bold   = "\033[1m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Blue   = "\033[34m"
	Cyan   = "\033[36m"
	Gray   = "\033[90m"
)

func FormatNumber(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func FormatSize(bytes int64) string {
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

func RelativeTime(t time.Time) string {
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

func ProgressBar(current, total int64, width int) string {
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
	return fmt.Sprintf("%s %s/%s %.0f%%", bar.String(), FormatSize(current), FormatSize(total), pct*100)
}

func RAMColor(fileSize int64, totalRAM int64) string {
	if totalRAM <= 0 {
		return ""
	}
	if fileSize <= totalRAM {
		return Green
	}
	return Red
}

func PrintError(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s[ERROR]%s %s\n", Red, Reset, fmt.Sprintf(format, args...))
}

func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max < 4 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func ExpandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return home + "/" + path[2:]
	}
	return path
}

func DirSize(path string) int64 {
	var size int64
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil {
			if e.IsDir() {
				size += DirSize(path+"/"+e.Name())
			} else {
				size += info.Size()
			}
		}
	}
	return size
}

func GetSystemRAM() int64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if kb, err := parseOrZero(fields[1]); err == nil {
					return kb * 1024
				}
			}
		}
	}
	return 0
}

func parseOrZero(s string) (int64, error) {
	var v int64
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}

func WordWrap(text string, width int) string {
	if width <= 0 {
		return text
	}
	var b strings.Builder
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if len(line) == 0 {
			b.WriteString("\n")
			continue
		}
		words := strings.Fields(line)
		lineLen := 0
		for _, word := range words {
			if lineLen+len(word)+1 > width {
				b.WriteString("\n")
				lineLen = 0
			}
			if lineLen > 0 {
				b.WriteString(" ")
				lineLen++
			}
			b.WriteString(word)
			lineLen += len(word)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func StripHTML(s string) string {
	var out strings.Builder
	inTag := false
	inEntity := false
	for i := 0; i < len(s); i++ {
		if s[i] == '<' {
			inTag = true
			continue
		}
		if s[i] == '>' && inTag {
			inTag = false
			continue
		}
		if s[i] == '&' {
			inEntity = true
			continue
		}
		if s[i] == ';' && inEntity {
			inEntity = false
			continue
		}
		if !inTag && !inEntity {
			out.WriteByte(s[i])
		}
	}
	return out.String()
}

func RenderMarkdown(s string, width int) string {
	s = stripFrontMatter(s)
	s = StripHTML(s)

	var b strings.Builder
	lines := strings.Split(s, "\n")
	inCodeBlock := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") {
			if !inCodeBlock {
				lang := strings.TrimPrefix(trimmed, "```")
				if lang != "" {
					b.WriteString(Cyan + lang + Reset + "\n")
				}
			}
			inCodeBlock = !inCodeBlock
			continue
		}

		if inCodeBlock {
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteString("\n")
			continue
		}

		if strings.HasPrefix(trimmed, "![") || strings.HasPrefix(trimmed, "[![") {
			continue
		}

		if strings.HasPrefix(trimmed, "### ") {
			text := renderInline(strings.TrimPrefix(trimmed, "### "))
			b.WriteString(Bold + Cyan + text + Reset + "\n")
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			text := renderInline(strings.TrimPrefix(trimmed, "## "))
			b.WriteString(Bold + Cyan + text + Reset + "\n")
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			text := renderInline(strings.TrimPrefix(trimmed, "# "))
			b.WriteString(Bold + Cyan + text + Reset + "\n")
			continue
		}

		if isHorizontalRule(trimmed) {
			hrW := width
			if hrW > 60 {
				hrW = 60
			}
			b.WriteString(Gray + strings.Repeat("─", hrW) + Reset + "\n")
			continue
		}

		line = renderInline(line)
		wrapped := WordWrap(line, width)
		b.WriteString(wrapped)
	}

	return b.String()
}

func stripFrontMatter(s string) string {
	if !strings.HasPrefix(s, "---") {
		return s
	}
	idx := strings.Index(s[3:], "---")
	if idx < 0 {
		return s
	}
	rest := s[3+idx+3:]
	rest = strings.TrimPrefix(rest, "\n")
	return rest
}

func renderInline(s string) string {
	s = regexp.MustCompile(`\*\*\*(.+?)\*\*\*`).ReplaceAllString(s, Bold+"$1"+Reset)
	s = regexp.MustCompile(`\*\*(.+?)\*\*`).ReplaceAllString(s, Bold+"$1"+Reset)
	s = regexp.MustCompile(`\*(.+?)\*`).ReplaceAllString(s, "$1")
	s = regexp.MustCompile("`(.+?)`").ReplaceAllString(s, Cyan+"$1"+Reset)
	s = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`).ReplaceAllString(s, "$1")
	return s
}

func isHorizontalRule(s string) bool {
	if len(s) < 3 {
		return false
	}
	c := s[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	for _, ch := range s {
		if ch != rune(c) && ch != ' ' {
			return false
		}
	}
	return true
}

func StripMarkdown(s string) string {
	return RenderMarkdown(s, 80)
}

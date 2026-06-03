package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/loq/lm-get/internal/config"
	"github.com/loq/lm-get/internal/display"
)

type CacheEntry struct {
	Data      []byte  `json:"data"`
	FetchedAt float64 `json:"fetched_at"`
}

var cacheDir string

func initCache() {
	home, _ := os.UserHomeDir()
	cacheDir = filepath.Join(home, ".cache", "lm-get")
	os.MkdirAll(cacheDir, 0755)
}

func ensureInit() {
	if cacheDir == "" {
		initCache()
	}
}

func cacheKey(url string) string {
	h := 0
	for _, c := range url {
		h = h*31 + int(c)
	}
	return filepath.Join(cacheDir, fmt.Sprintf("%x.json", h))
}

func Get(url string) []byte {
	ensureInit()
	data, err := os.ReadFile(cacheKey(url))
	if err != nil {
		return nil
	}
	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil
	}
	cfg := config.Load()
	ttl := float64(cfg.CacheTTL)
	if ttl == 0 {
		ttl = 300
	}
	if time.Now().Unix()-int64(entry.FetchedAt) > int64(ttl) {
		return nil
	}
	return entry.Data
}

func Set(url string, data []byte) {
	ensureInit()
	entry := CacheEntry{
		Data:      data,
		FetchedAt: float64(time.Now().Unix()),
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return
	}
	os.WriteFile(cacheKey(url), encoded, 0644)
}

func Clear() error {
	ensureInit()
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		os.Remove(filepath.Join(cacheDir, e.Name()))
	}
	return nil
}

func Info() (int64, int) {
	ensureInit()
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return 0, 0
	}
	var totalSize int64
	count := 0
	for _, e := range entries {
		if info, err := e.Info(); err == nil {
			totalSize += info.Size()
			count++
		}
	}
	return totalSize, count
}

func CmdCache(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: lm-get cache [clear|info]")
		return
	}
	switch args[0] {
	case "clear":
		if err := Clear(); err != nil {
			display.PrintError("Failed to clear cache: %v", err)
			return
		}
		fmt.Printf("%s✓ Cache cleared%s\n", display.Green, display.Reset)
	case "info":
		size, count := Info()
		fmt.Printf("Cache: %d entries, %s\n", count, display.FormatSize(size))
	default:
		display.PrintError("Unknown cache command: %s", args[0])
	}
}

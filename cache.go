package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
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

func cacheKey(url string) string {
	h := 0
	for _, c := range url {
		h = h*31 + int(c)
	}
	return filepath.Join(cacheDir, fmt.Sprintf("%x.json", h))
}

func cacheGet(url string) []byte {
	if cacheDir == "" {
		initCache()
	}
	data, err := os.ReadFile(cacheKey(url))
	if err != nil {
		return nil
	}
	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil
	}
	cfg := loadConfig()
	ttl := float64(cfg.CacheTTL)
	if ttl == 0 {
		ttl = 300
	}
	if time.Now().Unix()-int64(entry.FetchedAt) > int64(ttl) {
		return nil
	}
	return entry.Data
}

func cacheSet(url string, data []byte) {
	if cacheDir == "" {
		initCache()
	}
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

func cacheClear() error {
	if cacheDir == "" {
		initCache()
	}
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		os.Remove(filepath.Join(cacheDir, e.Name()))
	}
	return nil
}

func cacheInfo() (int64, int) {
	if cacheDir == "" {
		initCache()
	}
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

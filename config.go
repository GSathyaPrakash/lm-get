package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	DownloadsDir   string `json:"downloads_dir"`
	ResultsPerPage int    `json:"results_per_page"`
	DefaultSort    string `json:"default_sort"`
	CacheTTL       int    `json:"cache_ttl_seconds"`
}

func defaultConfig() Config {
	return Config{
		DownloadsDir:   "~/Models",
		ResultsPerPage: 20,
		DefaultSort:    "downloads",
		CacheTTL:       300,
	}
}

func (c Config) validate() Config {
	if c.ResultsPerPage <= 0 {
		c.ResultsPerPage = 20
	}
	if c.ResultsPerPage > 100 {
		c.ResultsPerPage = 100
	}
	if c.CacheTTL < 0 {
		c.CacheTTL = 300
	}
	validSorts := map[string]bool{"downloads": true, "likes": true, "lastModified": true}
	if !validSorts[c.DefaultSort] {
		c.DefaultSort = "downloads"
	}
	if c.DownloadsDir == "" {
		c.DownloadsDir = "~/Models"
	}
	return c
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "lm-get", "config.json")
}

func loadConfig() Config {
	cfg := defaultConfig()
	data, err := os.ReadFile(configPath())
	if err != nil {
		return cfg
	}
	json.Unmarshal(data, &cfg)
	return cfg.validate()
}

func saveConfig(cfg Config) error {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg.validate(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func cmdConfigInit() {
	path := configPath()
	if _, err := os.Stat(path); err == nil {
		fmt.Println("Config already exists at", path)
		return
	}
	cfg := defaultConfig()
	if err := saveConfig(cfg); err != nil {
		printError("Failed to create config: %v", err)
		return
	}
	fmt.Printf("%s✓ Config created at %s%s\n", green, path, reset)
}

func cmdCache(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: lm-get cache [clear|info]")
		return
	}
	switch args[0] {
	case "clear":
		if err := cacheClear(); err != nil {
			printError("Failed to clear cache: %v", err)
			return
		}
		fmt.Printf("%s✓ Cache cleared%s\n", green, reset)
	case "info":
		size, count := cacheInfo()
		fmt.Printf("Cache: %d entries, %s\n", count, formatSize(size))
	default:
		printError("Unknown cache command: %s", args[0])
	}
}

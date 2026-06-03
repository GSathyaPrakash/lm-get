package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/loq/lm-get/internal/display"
)

type Config struct {
	DownloadsDir   string `json:"downloads_dir"`
	ResultsPerPage int    `json:"results_per_page"`
	DefaultSort    string `json:"default_sort"`
	CacheTTL       int    `json:"cache_ttl_seconds"`
	RunCommand     string `json:"run_command"`
}

func Default() Config {
	return Config{
		DownloadsDir:   "~/Models",
		ResultsPerPage: 20,
		DefaultSort:    "downloads",
		CacheTTL:       300,
		RunCommand:     "llama-server --model {model} --port 8080",
	}
}

func (c Config) Validate() Config {
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
	if c.RunCommand == "" {
		c.RunCommand = "llama-server --model {model} --port 8080"
	}
	return c
}

func Path() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "lm-get", "config.json")
}

func Load() Config {
	cfg := Default()
	data, err := os.ReadFile(Path())
	if err != nil {
		return cfg
	}
	json.Unmarshal(data, &cfg)
	return cfg.Validate()
}

func Save(cfg Config) error {
	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg.Validate(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func CmdConfigInit() {
	path := Path()
	if _, err := os.Stat(path); err == nil {
		fmt.Println("Config already exists at", path)
		return
	}
	cfg := Default()
	if err := Save(cfg); err != nil {
		display.PrintError("Failed to create config: %v", err)
		return
	}
	fmt.Printf("%s✓ Config created at %s%s\n", display.Green, path, display.Reset)
}

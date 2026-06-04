package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

type ServerEntry struct {
	PID     int    `json:"pid"`
	Command string `json:"command"`
	LogPath string `json:"log_path"`
}

var (
	mu       sync.Mutex
	stateDir string
)

func initDir() {
	if stateDir != "" {
		return
	}
	home, _ := os.UserHomeDir()
	stateDir = filepath.Join(home, ".cache", "lm-get")
	os.MkdirAll(stateDir, 0755)
	os.MkdirAll(filepath.Join(stateDir, "logs"), 0755)
}

func stateFile() string {
	initDir()
	return filepath.Join(stateDir, "servers.json")
}

func LogDir() string {
	initDir()
	return filepath.Join(stateDir, "logs")
}

func Load() map[string]ServerEntry {
	mu.Lock()
	defer mu.Unlock()

	initDir()
	data, err := os.ReadFile(stateFile())
	if err != nil {
		return make(map[string]ServerEntry)
	}

	var entries map[string]ServerEntry
	if json.Unmarshal(data, &entries) != nil {
		return make(map[string]ServerEntry)
	}

	dirty := false
	for k, e := range entries {
		if !IsAlive(e.PID) {
			delete(entries, k)
			dirty = true
		}
	}
	if dirty {
		save(entries)
	}
	return entries
}

func Save(entries map[string]ServerEntry) {
	mu.Lock()
	defer mu.Unlock()
	save(entries)
}

func save(entries map[string]ServerEntry) {
	initDir()
	data, _ := json.MarshalIndent(entries, "", "  ")
	os.WriteFile(stateFile(), data, 0644)
}

func Set(modelPath string, entry ServerEntry) {
	mu.Lock()
	defer mu.Unlock()
	initDir()
	entries := make(map[string]ServerEntry)
	data, err := os.ReadFile(stateFile())
	if err == nil {
		json.Unmarshal(data, &entries)
	}
	entries[modelPath] = entry
	save(entries)
}

func Remove(modelPath string) {
	mu.Lock()
	defer mu.Unlock()
	initDir()
	entries := make(map[string]ServerEntry)
	data, err := os.ReadFile(stateFile())
	if err == nil {
		json.Unmarshal(data, &entries)
	}
	delete(entries, modelPath)
	save(entries)
}

func IsAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

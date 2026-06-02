package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type localFile struct {
	Name    string
	Size    int64
	ModTime time.Time
}

type localModel struct {
	Repo      string
	User      string
	Name      string
	Size      int64
	ModTime   time.Time
	Files     []localFile
	Expanded  bool
}

type localSort int

const (
	localSortTime localSort = iota
	localSortSize
)

var localSortNames = []string{"updated", "size"}

func scanLocalModels() []localModel {
	cfg := loadConfig()
	baseDir := expandHome(cfg.DownloadsDir)

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil
	}

	var models []localModel
	for _, userDir := range entries {
		if !userDir.IsDir() {
			continue
		}
		userPath := filepath.Join(baseDir, userDir.Name())
		repos, err := os.ReadDir(userPath)
		if err != nil {
			continue
		}
		for _, repoDir := range repos {
			if !repoDir.IsDir() {
				continue
			}
			repoPath := filepath.Join(userPath, repoDir.Name())
			dirInfo, err := repoDir.Info()
			if err != nil {
				continue
			}
			var files []localFile
			var totalSize int64
			if entries, err := os.ReadDir(repoPath); err == nil {
				for _, f := range entries {
					if f.IsDir() || !strings.HasSuffix(strings.ToLower(f.Name()), ".gguf") {
						continue
					}
					fi, err := f.Info()
					if err != nil {
						continue
					}
					files = append(files, localFile{
						Name:    f.Name(),
						Size:    fi.Size(),
						ModTime: fi.ModTime(),
					})
					totalSize += fi.Size()
				}
			}
			if len(files) == 0 {
				continue
			}
			models = append(models, localModel{
				Repo:    userDir.Name() + "/" + repoDir.Name(),
				User:    userDir.Name(),
				Name:    repoDir.Name(),
				Size:    totalSize,
				ModTime: dirInfo.ModTime(),
				Files:   files,
			})
		}
	}
	return models
}

func sortLocalModels(models []localModel, s localSort) {
	sort.SliceStable(models, func(i, j int) bool {
		switch s {
		case localSortSize:
			return models[i].Size > models[j].Size
		default:
			return models[i].ModTime.After(models[j].ModTime)
		}
	})
}

func cmdList(args []string) {
	cfg := loadConfig()
	baseDir := expandHome(cfg.DownloadsDir)

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("No models directory at %s\n", baseDir)
			return
		}
		printError("Failed to read downloads: %v", err)
		return
	}

	var totalSize int64
	count := 0

	fmt.Printf("\n%sDownloaded models in %s%s\n\n", bold, baseDir, reset)

	for _, userDir := range entries {
		if !userDir.IsDir() {
			continue
		}
		userPath := filepath.Join(baseDir, userDir.Name())
		repos, err := os.ReadDir(userPath)
		if err != nil {
			continue
		}
		for _, repoDir := range repos {
			if !repoDir.IsDir() {
				continue
			}
			repoPath := filepath.Join(userPath, repoDir.Name())
			size := dirSize(repoPath)
			totalSize += size
			count++

			fileCount := 0
			if files, err := os.ReadDir(repoPath); err == nil {
				for _, f := range files {
					if !f.IsDir() && strings.HasSuffix(strings.ToLower(f.Name()), ".gguf") {
						fileCount++
					}
				}
			}

			fmt.Printf("  %s%s/%s%s  %s%-10s%s  %d file(s)\n",
				cyan, userDir.Name(), repoDir.Name(), reset,
				green, formatSize(size), reset,
				fileCount,
			)
		}
	}

	if count == 0 {
		fmt.Printf("  %sNo models downloaded yet.%s\n", gray, reset)
	} else {
		fmt.Printf("\n  %sTotal: %d models, %s%s\n", bold, count, formatSize(totalSize), reset)
	}
}

func cmdRemove(args []string) {
	if len(args) == 0 {
		printError("Usage: lm-get remove <user/repo>")
		return
	}

	repo := args[0]
	cfg := loadConfig()
	repoPath := filepath.Join(expandHome(cfg.DownloadsDir), repo)

	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		printError("Model not found: %s", repo)
		return
	}

	size := dirSize(repoPath)
	fmt.Printf("Remove %s (%s)? [y/N] ", repo, formatSize(size))

	var answer string
	fmt.Scanln(&answer)
	if strings.ToLower(answer) != "y" {
		fmt.Println("Cancelled.")
		return
	}

	if err := os.RemoveAll(repoPath); err != nil {
		printError("Failed to remove: %v", err)
		return
	}

	fmt.Printf("%s✓ Removed %s (%s freed)%s\n", green, repo, formatSize(size), reset)
}

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/loq/lm-get/internal/api"
	"github.com/loq/lm-get/internal/config"
	"github.com/loq/lm-get/internal/display"
	"github.com/loq/lm-get/internal/download"
	"github.com/loq/lm-get/internal/models"
)

func CmdSearch(args []string) {
	if len(args) == 0 {
		display.PrintError("Usage: lm-get search <query>")
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

	cfg := config.Load()
	if sortBy == "" {
		sortBy = cfg.DefaultSort
	}
	if direction == "" {
		direction = "-1"
	}

	hfModels, err := api.SearchModels(query, limit, sortBy, direction, page)
	if err != nil {
		display.PrintError("Search failed: %v", err)
		return
	}

	if len(hfModels) == 0 {
		if page > 1 {
			fmt.Printf("%sNo more results for '%s' (page %d)%s\n", display.Yellow, query, page, display.Reset)
		} else {
			fmt.Printf("%sNo GGUF models found for '%s'%s\n", display.Yellow, query, display.Reset)
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

	fmt.Printf("\n%sSearch results for '%s'%s (page %d, sorted by %s)\n\n", display.Bold, query, display.Reset, page, sortLabel)

	for i, m := range hfModels {
		t, _ := time.Parse(time.RFC3339, m.LastModified)
		relTime := ""
		if !t.IsZero() {
			relTime = display.RelativeTime(t)
		}

		ggufCount := 0
		for _, s := range m.Siblings {
			if strings.HasSuffix(strings.ToLower(s.RFilename), ".gguf") {
				ggufCount++
			}
		}

		name := display.Truncate(m.ID, 50)
		num := (page-1)*limit + i + 1

		fmt.Printf("  %s%2d%s  %s%-52s%s  %s↓%s %-8s  %s♥%s %-8s  %s🗎%s %d GGUF  %s%s%s\n",
			display.Cyan, num, display.Reset,
			display.Bold, name, display.Reset,
			display.Blue, display.Reset, display.FormatNumber(m.Downloads),
			display.Red, display.Reset, display.FormatNumber(m.Likes),
			display.Gray, display.Reset, ggufCount,
			display.Gray, relTime, display.Reset,
		)
	}

	fmt.Printf("\n%sUse 'lm-get info <user/repo>' to see available files%s\n", display.Gray, display.Reset)
	if len(hfModels) == limit {
		fmt.Printf("%sUse --page %d for next page%s\n", display.Gray, page+1, display.Reset)
	}
}

func CmdInfo(args []string) {
	if len(args) == 0 {
		display.PrintError("Usage: lm-get info <user/repo>")
		return
	}

	repo := args[0]
	rawFiles, err := api.ListFiles(repo)
	if err != nil {
		display.PrintError("Failed to list files: %v", err)
		return
	}

	modelFiles := api.ParseModelFiles(rawFiles)
	entries := api.BuildDetailEntries(modelFiles, repo)

	if len(entries) == 0 {
		fmt.Printf("%sNo GGUF files found in %s%s\n", display.Yellow, repo, display.Reset)
		return
	}

	totalRAM := display.GetSystemRAM()
	freeDisk := api.GetDiskFree(display.ExpandHome(config.Load().DownloadsDir))

	fmt.Printf("\n%s%s%s — %d quantization(s)%s\n\n", display.Bold, repo, display.Reset, len(entries), display.Reset)

	for i, e := range entries {
		tags := ""
		if e.IsMmproj {
			tags += display.Yellow + " [mmproj]" + display.Reset
		}
		if e.IsShard {
			tags += display.Blue + fmt.Sprintf(" [%d shards]", e.ShardTotal) + display.Reset
		}
		if e.Downloaded {
			tags += display.Green + " [✓ downloaded]" + display.Reset
		}

		color := display.RAMColor(e.TotalSize, totalRAM)
		ramTag := ""
		if color == display.Green {
			ramTag = display.Green + " ✓fits" + display.Reset
		} else if color == display.Red {
			ramTag = display.Red + " ✗RAM" + display.Reset
		}

		diskTag := ""
		if freeDisk >= 0 {
			if e.TotalSize > freeDisk {
				diskTag = display.Red + " ✗disk" + display.Reset
			}
		}

		name := display.Truncate(e.DisplayName, 50)
		fmt.Printf("  %s%2d%s  %-52s  %s%-10s%s%s%s\n",
			display.Cyan, i+1, display.Reset,
			name,
			color, display.FormatSize(e.TotalSize), display.Reset,
			ramTag, tags+diskTag,
		)
	}

	fmt.Printf("\n%sUse 'lm-get download %s <file>' to download%s\n", display.Gray, repo, display.Reset)
}

func CmdDownload(args []string) {
	if len(args) < 2 {
		display.PrintError("Usage: lm-get download <user/repo> <file>")
		return
	}

	repo := args[0]
	fileName := args[1]
	dryRun := false
	outputDir := ""

	for i := 2; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			dryRun = true
		case "-o":
			if i+1 < len(args) {
				outputDir = args[i+1]
				i++
			}
		}
	}

	cfg := config.Load()
	if outputDir == "" {
		outputDir = cfg.DownloadsDir
	}
	outputDir = display.ExpandHome(outputDir)

	destDir := filepath.Join(outputDir, repo)
	destPath := filepath.Join(destDir, filepath.Base(fileName))
	downloadURL := api.GetDownloadURL(repo, fileName)

	fmt.Printf("Downloading %s%s%s → %s\n", display.Bold, fileName, display.Reset, destPath)

	if dryRun {
		fmt.Printf("%s[dry-run]%s Would download from:\n  %s\n", display.Yellow, display.Reset, downloadURL)
		return
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		display.PrintError("Failed to create directory: %v", err)
		return
	}

	if err := download.File(downloadURL, destPath); err != nil {
		display.PrintError("Download failed: %v", err)
		return
	}

	if info, err := os.Stat(destPath); err == nil {
		fmt.Printf("\n%s✓ Saved to %s (%s)%s\n", display.Green, destPath, display.FormatSize(info.Size()), display.Reset)
	} else {
		fmt.Printf("\n%s✓ Saved to %s%s\n", display.Green, destPath, display.Reset)
	}

	if api.HasMmprojFiles(repo) {
		fmt.Printf("\n%sThis model has vision/mmproj files. Use 'lm-get info %s' to see them.%s\n", display.Yellow, repo, display.Reset)
	}
}

func CmdList(args []string) {
	cfg := config.Load()
	baseDir := display.ExpandHome(cfg.DownloadsDir)

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("No models directory at %s\n", baseDir)
			return
		}
		display.PrintError("Failed to read downloads: %v", err)
		return
	}

	var totalSize int64
	count := 0

	fmt.Printf("\n%sDownloaded models in %s%s\n\n", display.Bold, baseDir, display.Reset)

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
			size := display.DirSize(repoPath)
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
				display.Cyan, userDir.Name(), repoDir.Name(), display.Reset,
				display.Green, display.FormatSize(size), display.Reset,
				fileCount,
			)
		}
	}

	if count == 0 {
		fmt.Printf("  %sNo models downloaded yet.%s\n", display.Gray, display.Reset)
	} else {
		fmt.Printf("\n  %sTotal: %d models, %s%s\n", display.Bold, count, display.FormatSize(totalSize), display.Reset)
	}
}

func CmdRemove(args []string) {
	if len(args) == 0 {
		display.PrintError("Usage: lm-get remove <user/repo>")
		return
	}

	repo := args[0]
	cfg := config.Load()
	repoPath := filepath.Join(display.ExpandHome(cfg.DownloadsDir), repo)

	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		display.PrintError("Model not found: %s", repo)
		return
	}

	size := display.DirSize(repoPath)
	fmt.Printf("Remove %s (%s)? [y/N] ", repo, display.FormatSize(size))

	var answer string
	fmt.Scanln(&answer)
	if strings.ToLower(answer) != "y" {
		fmt.Println("Cancelled.")
		return
	}

	if err := os.RemoveAll(repoPath); err != nil {
		display.PrintError("Failed to remove: %v", err)
		return
	}

	fmt.Printf("%s✓ Removed %s (%s freed)%s\n", display.Green, repo, display.FormatSize(size), display.Reset)
}

func ScanLocalModels() []models.LocalModel {
	cfg := config.Load()
	baseDir := display.ExpandHome(cfg.DownloadsDir)

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil
	}

	var localModels []models.LocalModel
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
			var files []models.LocalFileEntry
			var totalSize int64
			if subEntries, err := os.ReadDir(repoPath); err == nil {
				for _, f := range subEntries {
					if f.IsDir() || !strings.HasSuffix(strings.ToLower(f.Name()), ".gguf") {
						continue
					}
					fi, err := f.Info()
					if err != nil {
						continue
					}
					files = append(files, models.LocalFileEntry{
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
			localModels = append(localModels, models.LocalModel{
				Repo:    userDir.Name() + "/" + repoDir.Name(),
				User:    userDir.Name(),
				Name:    repoDir.Name(),
				Size:    totalSize,
				ModTime: dirInfo.ModTime(),
				Files:   files,
			})
		}
	}
	return localModels
}

func SortLocalModels(localModels []models.LocalModel, s models.LocalSort) {
	sort.SliceStable(localModels, func(i, j int) bool {
		switch s {
		case models.LocalSortSize:
			return localModels[i].Size > localModels[j].Size
		default:
			return localModels[i].ModTime.After(localModels[j].ModTime)
		}
	})
}

func PrintHelp() {
	fmt.Printf(`%slm-get%s - Search and download GGUF models from Hugging Face

%sUsage:%s
  lm-get                Interactive TUI mode (default)
  lm-get <command>      CLI mode

%sCommands:%s
  search   <query>             Search for GGUF models
  info     <user/repo>         List quantizations for a model
  download <user/repo> <file>  Download a GGUF file
  list                         List downloaded models
  remove   <user/repo>         Remove a downloaded model
  cache    [clear|info]        Manage API cache
  config   init                Create default config file

%sOptions:%s
  -n <int>         Number of search results (default: 20)
  --page, -p <int> Page number for pagination
  --sort <field>   Sort by: downloads, likes, lastModified
  --direction <d>  Sort direction: -1 (desc), 1 (asc)
  -o <dir>         Override download directory
  --dry-run        Simulate download without writing files
  --help, -h       Show this help
  --version, -v    Show version

%sExamples:%s
  lm-get                              Interactive mode
  lm-get search qwen                  Search for qwen models
  lm-get search qwen --sort likes     Sort by likes
  lm-get search qwen --page 2         Second page of results
  lm-get info unsloth/Qwen3-5B-GGUF   Show available files
  lm-get list                         Show downloaded models
  lm-get remove unsloth/Qwen3-5B-GGUF Delete a model
  lm-get cache clear                  Clear API cache
`, display.Bold, display.Reset, display.Bold, display.Reset, display.Bold, display.Reset, display.Bold, display.Reset, display.Bold, display.Reset)
}

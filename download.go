package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func cmdDownload(args []string) {
	if len(args) < 2 {
		printError("Usage: lm-get download <user/repo> <file>")
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

	cfg := loadConfig()
	if outputDir == "" {
		outputDir = cfg.DownloadsDir
	}
	outputDir = expandHome(outputDir)

	destDir := filepath.Join(outputDir, repo)
	destPath := filepath.Join(destDir, filepath.Base(fileName))
	downloadURL := getDownloadURL(repo, fileName)

	fmt.Printf("Downloading %s%s%s → %s\n", bold, fileName, reset, destPath)

	if dryRun {
		fmt.Printf("%s[dry-run]%s Would download from:\n  %s\n", yellow, reset, downloadURL)
		return
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		printError("Failed to create directory: %v", err)
		return
	}

	if err := downloadFile(downloadURL, destPath); err != nil {
		printError("Download failed: %v", err)
		return
	}

	if info, err := os.Stat(destPath); err == nil {
		fmt.Printf("\n%s✓ Saved to %s (%s)%s\n", green, destPath, formatSize(info.Size()), reset)
	} else {
		fmt.Printf("\n%s✓ Saved to %s%s\n", green, destPath, reset)
	}

	if hasMmprojFiles(repo) {
		fmt.Printf("\n%sThis model has vision/mmproj files. Use 'lm-get info %s' to see them.%s\n", yellow, repo, reset)
	}
}

func downloadFile(url, destPath string) error {
	var existingSize int64
	if info, err := os.Stat(destPath); err == nil {
		existingSize = info.Size()
	}

	if existingSize > 0 {
		fmt.Printf("File exists (%s). Resuming...\n", formatSize(existingSize))
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	if existingSize > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existingSize))
	}

	req.Header.Set("User-Agent", "lm-get/"+version)

	resp, err := dlClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	totalSize := resp.ContentLength
	if resp.StatusCode == http.StatusPartialContent {
		totalSize += existingSize
	} else {
		existingSize = 0
	}

	if totalSize > 0 {
		freeSpace := getDiskFree(filepath.Dir(destPath))
		if freeSpace >= 0 && totalSize > freeSpace {
			return fmt.Errorf("not enough disk space: need %s, have %s free", formatSize(totalSize), formatSize(freeSpace))
		}
	}

	flags := os.O_CREATE | os.O_WRONLY
	if existingSize > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	f, err := os.OpenFile(destPath, flags, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if existingSize > 0 {
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			return err
		}
		fmt.Printf("Resuming from %s\n", formatSize(existingSize))
	}

	buf := make([]byte, 1024*1024)
	var downloaded int64
	lastUpdate := time.Time{}

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			downloaded += int64(n)

			now := time.Now()
			if now.Sub(lastUpdate) > 100*time.Millisecond || readErr != nil {
				if totalSize > 0 {
					fmt.Printf("\r  %s", progressBar(existingSize+downloaded, totalSize, 40))
				} else {
					fmt.Printf("\r  %s downloaded", formatSize(downloaded))
				}
				lastUpdate = now
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	return nil
}

func downloadMultipleFiles(repo string, paths []string, outputDir string) error {
	cfg := loadConfig()
	baseDir := expandHome(cfg.DownloadsDir)
	if outputDir != "" {
		baseDir = expandHome(outputDir)
	}
	destDir := filepath.Join(baseDir, repo)

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}

	var totalSize int64
	for _, p := range paths {
		destPath := filepath.Join(destDir, filepath.Base(p))
		if info, err := os.Stat(destPath); err == nil {
			totalSize -= info.Size()
		}
	}

	for i, p := range paths {
		destPath := filepath.Join(destDir, filepath.Base(p))
		fmt.Printf("\n%s[%d/%d]%s Downloading %s\n", cyan, i+1, len(paths), reset, filepath.Base(p))
		url := getDownloadURL(repo, p)
		if err := downloadFile(url, destPath); err != nil {
			return fmt.Errorf("failed to download %s: %w", filepath.Base(p), err)
		}
	}
	return nil
}

func hasMmprojFiles(repo string) bool {
	rawFiles, err := listFiles(repo)
	if err != nil {
		return false
	}
	for _, f := range rawFiles {
		if strings.HasPrefix(strings.ToLower(filepath.Base(f.Path)), "mmproj") {
			return true
		}
	}
	return false
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func getSystemRAM() int64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if kb, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
					return kb * 1024
				}
			}
		}
	}
	return 0
}

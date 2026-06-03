package download

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/loq/lm-get/internal/api"
	"github.com/loq/lm-get/internal/config"
	"github.com/loq/lm-get/internal/display"
)

func File(downloadURL, destPath string) error {
	var existingSize int64
	if info, err := os.Stat(destPath); err == nil {
		existingSize = info.Size()
	}

	if existingSize > 0 {
		fmt.Printf("File exists (%s). Resuming...\n", display.FormatSize(existingSize))
	}

	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return err
	}

	if existingSize > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existingSize))
	}

	req.Header.Set("User-Agent", "lm-get/"+api.Version)

	resp, err := api.DLClient.Do(req)
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
		freeSpace := api.GetDiskFree(filepath.Dir(destPath))
		if freeSpace >= 0 && totalSize > freeSpace {
			return fmt.Errorf("not enough disk space: need %s, have %s free", display.FormatSize(totalSize), display.FormatSize(freeSpace))
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
		fmt.Printf("Resuming from %s\n", display.FormatSize(existingSize))
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
					fmt.Printf("\r  %s", display.ProgressBar(existingSize+downloaded, totalSize, 40))
				} else {
					fmt.Printf("\r  %s downloaded", display.FormatSize(downloaded))
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

func MultipleFiles(repo string, paths []string, outputDir string) error {
	cfg := config.Load()
	baseDir := display.ExpandHome(cfg.DownloadsDir)
	if outputDir != "" {
		baseDir = display.ExpandHome(outputDir)
	}
	destDir := filepath.Join(baseDir, repo)

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}

	for i, p := range paths {
		destPath := filepath.Join(destDir, filepath.Base(p))
		fmt.Printf("\n%s[%d/%d]%s Downloading %s\n", display.Cyan, i+1, len(paths), display.Reset, filepath.Base(p))
		downloadURL := api.GetDownloadURL(repo, p)
		if err := File(downloadURL, destPath); err != nil {
			return fmt.Errorf("failed to download %s: %w", filepath.Base(p), err)
		}
	}
	return nil
}

func ToFile(downloadURL, destPath string, progress chan<- int64) error {
	var existingSize int64
	if info, err := os.Stat(destPath); err == nil {
		existingSize = info.Size()
	}

	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return err
	}
	if existingSize > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existingSize))
	}
	req.Header.Set("User-Agent", "lm-get/"+api.Version)

	resp, err := api.DLClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	total := resp.ContentLength
	if resp.StatusCode == http.StatusPartialContent {
		total += existingSize
	} else {
		existingSize = 0
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
		f.Seek(0, io.SeekEnd)
	}

	buf := make([]byte, 1024*1024)
	var downloaded int64
	lastSend := time.Now()

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			downloaded += int64(n)

			now := time.Now()
			if now.Sub(lastSend) > 150*time.Millisecond && progress != nil {
				lastSend = now
				progress <- existingSize + downloaded
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	f.Sync()
	return nil
}

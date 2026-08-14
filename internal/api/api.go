package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/loq/lm-get/internal/cache"
	"github.com/loq/lm-get/internal/config"
	"github.com/loq/lm-get/internal/display"
	"github.com/loq/lm-get/internal/models"
)

var Version = "dev"

var APIClient = &http.Client{Timeout: 30 * time.Second}

var DLClient = &http.Client{
	Timeout: 0,
	Transport: &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    90 * time.Second,
		DisableCompression: true,
	},
}

func hfGet(rawURL string) ([]byte, error) {
	cached := cache.Get(rawURL)
	if cached != nil {
		return cached, nil
	}

	var data []byte
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		data, err = hfGetOnce(rawURL)
		if err == nil {
			cache.Set(rawURL, data)
			return data, nil
		}
		if attempt < 2 {
			time.Sleep(time.Duration(attempt+1) * time.Second)
		}
	}
	return nil, err
}

func hfGetOnce(rawURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "lm-get/"+Version)

	resp, err := APIClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	return io.ReadAll(resp.Body)
}

func SearchModels(query string, limit int, sortBy string, direction string, page int) ([]models.HFModel, error) {
	if direction == "" {
		direction = "-1"
	}
	if sortBy == "" {
		sortBy = "downloads"
	}

	skip := 0
	if page > 1 {
		skip = (page - 1) * limit
	}

	u, _ := url.Parse("https://huggingface.co/api/models")
	q := u.Query()
	q.Set("search", query)
	q.Set("filter", "gguf")
	q.Set("sort", sortBy)
	q.Set("direction", direction)
	q.Set("limit", fmt.Sprintf("%d", limit))
	q.Set("skip", fmt.Sprintf("%d", skip))
	q.Set("full", "true")
	q.Set("type", "model")
	u.RawQuery = q.Encode()

	data, err := hfGet(u.String())
	if err != nil {
		return nil, err
	}

	var hfModels []models.HFModel
	if err := json.Unmarshal(data, &hfModels); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return FilterGGUF(hfModels), nil
}

func FilterGGUF(hfModels []models.HFModel) []models.HFModel {
	var result []models.HFModel
	for _, m := range hfModels {
		for _, s := range m.Siblings {
			if strings.HasSuffix(strings.ToLower(s.RFilename), ".gguf") {
				result = append(result, m)
				break
			}
		}
	}
	return result
}

func ListFiles(repo string) ([]models.HFFileTree, error) {
	data, err := hfGet("https://huggingface.co/api/models/" + repo + "/tree/main")
	if err != nil {
		return nil, err
	}

	var files []models.HFFileTree
	if err := json.Unmarshal(data, &files); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	var gguf []models.HFFileTree
	for _, f := range files {
		if f.Type == "file" && strings.HasSuffix(f.Path, ".gguf") {
			gguf = append(gguf, f)
		}
		if f.Type == "directory" {
			subFiles, subErr := listSubDir(repo, f.Path)
			if subErr == nil {
				gguf = append(gguf, subFiles...)
			}
		}
	}
	return gguf, nil
}

func listSubDir(repo, dirPath string) ([]models.HFFileTree, error) {
	data, err := hfGet("https://huggingface.co/api/models/" + repo + "/tree/main/" + dirPath)
	if err != nil {
		return nil, err
	}

	var files []models.HFFileTree
	if err := json.Unmarshal(data, &files); err != nil {
		return nil, err
	}

	var gguf []models.HFFileTree
	for _, f := range files {
		if f.Type == "file" && strings.HasSuffix(f.Path, ".gguf") {
			gguf = append(gguf, f)
		}
	}
	return gguf, nil
}

func ParseModelFiles(rawFiles []models.HFFileTree) []models.ModelFile {
	var files []models.ModelFile
	for _, f := range rawFiles {
		size := f.Size
		if f.LFS != nil && f.LFS.Size > 0 {
			size = f.LFS.Size
		}

		name := filepath.Base(f.Path)
		mf := models.ModelFile{
			Path: f.Path,
			Size: size,
		}

		if strings.HasPrefix(strings.ToLower(name), "mmproj") {
			mf.IsMmproj = true
		}

		if idx, total, ok := ParseShardPattern(name); ok {
			mf.IsShard = true
			mf.ShardIndex = idx
			mf.ShardTotal = total
			mf.ShardGroup = name[:len(name)-len(fmt.Sprintf("-%05d-of-%05d.gguf", idx, total))]
		}

		files = append(files, mf)
	}
	return files
}

func BuildDetailEntries(files []models.ModelFile, repo string) []models.DetailEntry {
	shardGroups := map[string][]models.ModelFile{}
	var singles []models.ModelFile
	var mmprojFiles []models.ModelFile

	for _, f := range files {
		if f.IsMmproj {
			mmprojFiles = append(mmprojFiles, f)
			continue
		}
		if f.IsShard {
			shardGroups[f.ShardGroup] = append(shardGroups[f.ShardGroup], f)
			continue
		}
		singles = append(singles, f)
	}

	var entries []models.DetailEntry

	for _, f := range singles {
		entries = append(entries, models.DetailEntry{
			DisplayName: filepath.Base(f.Path),
			Paths:       []string{f.Path},
			TotalSize:   f.Size,
			Downloaded:  IsFileDownloaded(repo, f.Path),
		})
	}

	for name, shards := range shardGroups {
		var totalSize int64
		var paths []string
		allDownloaded := true
		for _, s := range shards {
			totalSize += s.Size
			paths = append(paths, s.Path)
			if !IsFileDownloaded(repo, s.Path) {
				allDownloaded = false
			}
		}
		entries = append(entries, models.DetailEntry{
			DisplayName: name,
			Paths:       paths,
			TotalSize:   totalSize,
			IsShard:     true,
			ShardTotal:  len(shards),
			Downloaded:  allDownloaded,
		})
	}

	for _, f := range mmprojFiles {
		entries = append(entries, models.DetailEntry{
			DisplayName: filepath.Base(f.Path),
			Paths:       []string{f.Path},
			TotalSize:   f.Size,
			IsMmproj:    true,
			Downloaded:  IsFileDownloaded(repo, f.Path),
		})
	}

	return entries
}

func ParseShardPattern(name string) (int, int, bool) {
	lower := strings.ToLower(name)
	if !strings.Contains(lower, "-of-") || !strings.HasSuffix(lower, ".gguf") {
		return 0, 0, false
	}

	trimmed := strings.TrimSuffix(lower, ".gguf")
	ofIdx := strings.LastIndex(trimmed, "-of-")
	if ofIdx < 0 {
		return 0, 0, false
	}
	totalStr := trimmed[ofIdx+4:]

	prefix := trimmed[:ofIdx]
	lastDash := strings.LastIndex(prefix, "-")
	if lastDash < 0 {
		return 0, 0, false
	}
	idxStr := prefix[lastDash+1:]

	var idx, total int
	fmt.Sscanf(idxStr, "%05d", &idx)
	fmt.Sscanf(totalStr, "%05d", &total)
	if idx > 0 && total > 1 {
		return idx, total, true
	}
	return 0, 0, false
}

func IsFileDownloaded(repo, filePath string) bool {
	cfg := config.Load()
	base := filepath.Base(filePath)
	localPath := filepath.Join(display.ExpandHome(cfg.DownloadsDir), repo, base)
	info, err := os.Stat(localPath)
	if err != nil {
		return false
	}
	return info.Size() > 0
}

func GetDownloadURL(repo, filePath string) string {
	return fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s?download=true", repo, filePath)
}

func FetchReadme(repo string) (string, error) {
	urls := []string{
		"https://huggingface.co/" + repo + "/raw/main/README.md",
		"https://huggingface.co/" + repo + "/raw/main/readme.md",
	}
	for _, u := range urls {
		data, err := hfGet(u)
		if err == nil {
			return string(data), nil
		}
	}
	return "", fmt.Errorf("no README found")
}

func GetDiskFree(path string) int64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return -1
	}
	return int64(stat.Bavail) * int64(stat.Bsize)
}

func HasMmprojFiles(repo string) bool {
	rawFiles, err := ListFiles(repo)
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

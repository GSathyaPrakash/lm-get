package main

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
)

type HFModel struct {
	ID           string `json:"id"`
	Downloads    int    `json:"downloads"`
	Likes        int    `json:"likes"`
	LastModified string `json:"lastModified"`
	Siblings     []struct {
		RFilename string `json:"rfilename"`
	} `json:"siblings"`
}

type HFFileTree struct {
	Type string `json:"type"`
	Path string `json:"path"`
	Size int64  `json:"size"`
	OID  string `json:"oid"`
	LFS  *struct {
		Size int64 `json:"size"`
	} `json:"lfs"`
}

type ModelFile struct {
	Path       string
	Size       int64
	IsMmproj   bool
	IsShard    bool
	ShardGroup string
	ShardIndex int
	ShardTotal int
}

type DetailEntry struct {
	DisplayName string
	Paths       []string
	TotalSize   int64
	IsMmproj    bool
	IsShard     bool
	ShardTotal  int
	Downloaded  bool
}

var apiClient = &http.Client{Timeout: 30 * time.Second}

var dlClient = &http.Client{
	Timeout: 0,
	Transport: &http.Transport{
		MaxIdleConns:        10,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  true,
	},
}

func hfGet(rawURL string) ([]byte, error) {
	cached := cacheGet(rawURL)
	if cached != nil {
		return cached, nil
	}

	var data []byte
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		data, err = hfGetOnce(rawURL)
		if err == nil {
			cacheSet(rawURL, data)
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
	req.Header.Set("User-Agent", "lm-get/"+version)

	resp, err := apiClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	return io.ReadAll(resp.Body)
}

func searchModels(query string, limit int, sortBy string, direction string, page int) ([]HFModel, error) {
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
	q.Set("search", query+" gguf")
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

	var models []HFModel
	if err := json.Unmarshal(data, &models); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return filterGGUF(models), nil
}

func filterGGUF(models []HFModel) []HFModel {
	var result []HFModel
	for _, m := range models {
		for _, s := range m.Siblings {
			if strings.HasSuffix(strings.ToLower(s.RFilename), ".gguf") {
				result = append(result, m)
				break
			}
		}
	}
	return result
}

func listFiles(repo string) ([]HFFileTree, error) {
	data, err := hfGet("https://huggingface.co/api/models/" + repo + "/tree/main")
	if err != nil {
		return nil, err
	}

	var files []HFFileTree
	if err := json.Unmarshal(data, &files); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	var gguf []HFFileTree
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

func listSubDir(repo, dirPath string) ([]HFFileTree, error) {
	data, err := hfGet("https://huggingface.co/api/models/" + repo + "/tree/main/" + dirPath)
	if err != nil {
		return nil, err
	}

	var files []HFFileTree
	if err := json.Unmarshal(data, &files); err != nil {
		return nil, err
	}

	var gguf []HFFileTree
	for _, f := range files {
		if f.Type == "file" && strings.HasSuffix(f.Path, ".gguf") {
			gguf = append(gguf, f)
		}
	}
	return gguf, nil
}

func parseModelFiles(rawFiles []HFFileTree) []ModelFile {
	var files []ModelFile
	for _, f := range rawFiles {
		size := f.Size
		if f.LFS != nil && f.LFS.Size > 0 {
			size = f.LFS.Size
		}

		name := filepath.Base(f.Path)
		mf := ModelFile{
			Path: f.Path,
			Size: size,
		}

		if strings.HasPrefix(strings.ToLower(name), "mmproj") {
			mf.IsMmproj = true
		}

		if idx, total, ok := parseShardPattern(name); ok {
			mf.IsShard = true
			mf.ShardIndex = idx
			mf.ShardTotal = total
			mf.ShardGroup = name[:len(name)-len(fmt.Sprintf("-%05d-of-%05d.gguf", idx, total))]
		}

		files = append(files, mf)
	}
	return files
}

func buildDetailEntries(files []ModelFile, repo string) []DetailEntry {
	shardGroups := map[string][]ModelFile{}
	var singles []ModelFile
	var mmprojFiles []ModelFile

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

	var entries []DetailEntry

	for _, f := range singles {
		entries = append(entries, DetailEntry{
			DisplayName: filepath.Base(f.Path),
			Paths:       []string{f.Path},
			TotalSize:   f.Size,
			Downloaded:  isFileDownloaded(repo, f.Path),
		})
	}

	for name, shards := range shardGroups {
		var totalSize int64
		var paths []string
		allDownloaded := true
		for _, s := range shards {
			totalSize += s.Size
			paths = append(paths, s.Path)
			if !isFileDownloaded(repo, s.Path) {
				allDownloaded = false
			}
		}
		entries = append(entries, DetailEntry{
			DisplayName: name,
			Paths:       paths,
			TotalSize:   totalSize,
			IsShard:     true,
			ShardTotal:  len(shards),
			Downloaded:  allDownloaded,
		})
	}

	for _, f := range mmprojFiles {
		entries = append(entries, DetailEntry{
			DisplayName: filepath.Base(f.Path),
			Paths:       []string{f.Path},
			TotalSize:   f.Size,
			IsMmproj:    true,
			Downloaded:  isFileDownloaded(repo, f.Path),
		})
	}

	return entries
}

func parseShardPattern(name string) (int, int, bool) {
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

func isFileDownloaded(repo, filePath string) bool {
	cfg := loadConfig()
	base := filepath.Base(filePath)
	localPath := filepath.Join(expandHome(cfg.DownloadsDir), repo, base)
	info, err := os.Stat(localPath)
	if err != nil {
		return false
	}
	return info.Size() > 0
}

func getDownloadURL(repo, filePath string) string {
	return fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s?download=true", repo, filePath)
}

func fetchReadme(repo string) (string, error) {
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

func getDiskFree(path string) int64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return -1
	}
	return int64(stat.Bavail) * int64(stat.Bsize)
}

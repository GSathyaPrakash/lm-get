package models

import "time"

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

type LocalFileEntry struct {
	Name    string
	Size    int64
	ModTime time.Time
}

type LocalModel struct {
	Repo     string
	User     string
	Name     string
	Size     int64
	ModTime  time.Time
	Files    []LocalFileEntry
	Expanded bool
}

type LocalSort int

const (
	LocalSortTime LocalSort = iota
	LocalSortSize
)

var LocalSortNames = []string{"updated", "size"}

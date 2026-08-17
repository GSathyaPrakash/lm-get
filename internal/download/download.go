package download

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/loq/lm-get/internal/api"
	"github.com/loq/lm-get/internal/config"
	"github.com/loq/lm-get/internal/display"
)

// segmentSize bounds the unit of work per HTTP range request. Completed
// segments are persisted to the state file so an interrupted download
// resumes only the missing ranges. The actual size adapts between min/max
// based on file size.
const (
	minSegmentSize = 4 << 20
	maxSegmentSize = 16 << 20
)

// stateSuffix sidecar tracks completed byte ranges for parallel resume.
const stateSuffix = ".lmget-state"

var contentRangeRe = regexp.MustCompile(`bytes\s+(\d+)-(\d+)/(\d+)`)

type rangeSpan struct {
	Start int64 `json:"s"`
	End   int64 `json:"e"` // exclusive
}

type stateFile struct {
	Total int64       `json:"total"`
	Done  []rangeSpan `json:"done"`
}

// engine downloads url to dest using parallel HTTP range requests.
type engine struct {
	url      string
	dest     string
	stateLoc string
	conns    int
	total    int64
	doneBase int64 // bytes already complete on resume

	mu    sync.Mutex
	done  []rangeSpan // merged, sorted
	f     *os.File
	state *os.File

	written    atomic.Int64 // bytes in completed segments
	inflight   atomic.Int64 // bytes received in open segments (progress only)
	progressFn func(current, total int64)

	partialsMu sync.Mutex
	partials   []rangeSpan // per-worker current extent, for state snapshots
}

func (e *engine) current() int64 {
	return e.doneBase + e.written.Load() + e.inflight.Load()
}

// updatePartial records a worker's in-flight extent [start,end) for the
// periodic state snapshot. idx 0 clears the slot.
func (e *engine) updatePartial(idx int, start, end int64) {
	e.partialsMu.Lock()
	defer e.partialsMu.Unlock()
	if idx < len(e.partials) {
		if end > start {
			e.partials[idx] = rangeSpan{start, end}
		} else {
			e.partials[idx] = rangeSpan{}
		}
	}
}

// snapshotState persists completed ranges plus in-flight extents. Data is
// fsynced first so claimed bytes are durable even on power loss.
func (e *engine) snapshotState() {
	e.mu.Lock()
	e.f.Sync()
	e.mu.Unlock()

	e.partialsMu.Lock()
	spans := make([]rangeSpan, 0, len(e.done)+len(e.partials))
	spans = append(spans, e.done...)
	for _, p := range e.partials {
		if p.End > p.Start {
			spans = append(spans, p)
		}
	}
	e.partialsMu.Unlock()

	e.mu.Lock()
	merged := append([]rangeSpan(nil), spans...)
	sort.Slice(merged, func(i, j int) bool { return merged[i].Start < merged[j].Start })
	var compact []rangeSpan
	for _, r := range merged {
		if n := len(compact); n > 0 && r.Start <= compact[n-1].End {
			if r.End > compact[n-1].End {
				compact[n-1].End = r.End
			}
		} else {
			compact = append(compact, r)
		}
	}
	st := stateFile{Total: e.total, Done: compact}
	e.mu.Unlock()

	if data, err := json.Marshal(&st); err == nil {
		if e.state == nil {
			if f, err := os.OpenFile(e.stateLoc, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644); err == nil {
				e.state = f
			}
		}
		if e.state != nil {
			if _, err := e.state.WriteAt(data, 0); err == nil {
				e.state.Truncate(int64(len(data)))
				e.state.Sync()
			}
		}
	}
}

func runEngine(url, dest string, conns int, progressFn func(current, total int64)) error {
	e := &engine{
		url:        url,
		dest:       dest,
		stateLoc:   dest + stateSuffix,
		conns:      conns,
		progressFn: progressFn,
		partials:   make([]rangeSpan, conns),
	}
	return e.run()
}

func (e *engine) run() error {
	total, supportsRange, err := e.probe()
	if err != nil {
		return err
	}
	e.total = total

	if !supportsRange || total <= 0 {
		return e.singleStream()
	}

	if err := e.loadState(); err != nil {
		return err
	}

	// Already-complete check (no state file = previous completed download).
	if _, statErr := os.Stat(e.stateLoc); os.IsNotExist(statErr) {
		if info, err := os.Stat(e.dest); err == nil && info.Size() == total && info.Size() > 0 {
			return nil
		}
	}

	if e.doneBase == 0 {
		fmt.Printf("Downloading with %d connections\n", e.conns)
	} else {
		fmt.Printf("Resuming from %s (%d connections)\n", display.FormatSize(e.doneBase), e.conns)
	}

	// Persist state before truncating so a crash never leaves a
	// preallocated file without resume info.
	if err := e.saveState(); err != nil {
		return err
	}

	f, err := os.OpenFile(e.dest, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	e.f = f
	if info, _ := f.Stat(); info == nil || info.Size() != total {
		if err := f.Truncate(total); err != nil {
			f.Close()
			return err
		}
	}

	missing := e.missingRanges()
	if len(missing) == 0 {
		return e.finish()
	}

	err = e.downloadRanges(missing)
	if err != nil {
		f.Close()
		return err
	}
	return e.finish()
}

// probe determines total size and range support via a 1-byte range request.
func (e *engine) probe() (int64, bool, error) {
	req, err := http.NewRequest("GET", e.url, nil)
	if err != nil {
		return 0, false, err
	}
	req.Header.Set("Range", "bytes=0-0")
	req.Header.Set("User-Agent", "lm-get/"+api.Version)

	resp, err := api.DLClient.Do(req)
	if err != nil {
		return 0, false, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode == http.StatusPartialContent {
		if m := contentRangeRe.FindStringSubmatch(resp.Header.Get("Content-Range")); m != nil {
			total, _ := strconv.ParseInt(m[3], 10, 64)
			return total, true, nil
		}
	}
	if resp.StatusCode == http.StatusOK {
		return resp.ContentLength, false, nil
	}
	return 0, false, fmt.Errorf("HTTP %d", resp.StatusCode)
}

func (e *engine) loadState() error {
	data, err := os.ReadFile(e.stateLoc)
	if err != nil {
		return nil // no state = fresh download
	}
	var st stateFile
	if json.Unmarshal(data, &st) != nil || st.Total != e.total {
		os.Remove(e.stateLoc)
		return nil
	}
	e.done = st.Done
	for _, r := range e.done {
		e.doneBase += r.End - r.Start
	}
	return nil
}

func (e *engine) saveState() error {
	e.mu.Lock()
	st := stateFile{Total: e.total, Done: e.done}
	e.mu.Unlock()

	data, err := json.Marshal(&st)
	if err != nil {
		return err
	}
	if e.state == nil {
		f, err := os.OpenFile(e.stateLoc, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
		if err != nil {
			return err
		}
		e.state = f
	}
	if _, err := e.state.WriteAt(data, 0); err != nil {
		return err
	}
	if err := e.state.Truncate(int64(len(data))); err != nil {
		return err
	}
	return e.state.Sync()
}

// missingRanges returns [0,total) minus completed ranges, sorted.
func (e *engine) missingRanges() []rangeSpan {
	e.mu.Lock()
	defer e.mu.Unlock()

	var out []rangeSpan
	var pos int64
	for _, r := range e.done {
		if r.Start > pos {
			out = append(out, rangeSpan{pos, r.Start})
		}
		if r.End > pos {
			pos = r.End
		}
	}
	if pos < e.total {
		out = append(out, rangeSpan{pos, e.total})
	}
	return out
}

// downloadRanges splits missing spans into ~conns balanced assignments and
// fetches them in parallel, segment by segment.
func (e *engine) downloadRanges(missing []rangeSpan) error {
	var totalMissing int64
	for _, r := range missing {
		totalMissing += r.End - r.Start
	}

	assignments := make([][]rangeSpan, e.conns)
	target := (totalMissing + int64(e.conns) - 1) / int64(e.conns)
	segSize := (totalMissing + int64(e.conns)*8 - 1) / (int64(e.conns) * 8)
	if segSize < minSegmentSize {
		segSize = minSegmentSize
	}
	if segSize > maxSegmentSize {
		segSize = maxSegmentSize
	}
	i := 0
	for _, r := range missing {
		for r.Start < r.End {
			remaining := target
			for remaining > 0 && r.Start < r.End {
				n := r.End - r.Start
				if n > segSize {
					n = segSize
				}
				if n > remaining {
					n = remaining
				}
				assignments[i] = append(assignments[i], rangeSpan{r.Start, r.Start + n})
				r.Start += n
				remaining -= n
			}
			i = (i + 1) % e.conns
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, e.conns)

	for idx, segs := range assignments {
		if len(segs) == 0 {
			continue
		}
		wg.Add(1)
		go func(idx int, segs []rangeSpan) {
			defer wg.Done()
			if err := e.fetchSegments(ctx, idx, segs); err != nil {
				cancel()
				errCh <- err
			}
		}(idx, segs)
	}

	// Progress reporter + periodic resume-state snapshots.
	stopProgress := make(chan struct{})
	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		snapshot := time.NewTicker(3 * time.Second)
		defer snapshot.Stop()
		for {
			select {
			case <-stopProgress:
				if e.progressFn != nil {
					e.progressFn(e.current(), e.total)
				}
				return
			case <-snapshot.C:
				e.snapshotState()
			case <-ticker.C:
				if e.progressFn != nil {
					e.progressFn(e.current(), e.total)
				}
			}
		}
	}()

	wg.Wait()
	close(stopProgress)
	<-progressDone

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

// fetchSegments downloads a worker's segments sequentially, retrying each
// segment on failure.
func (e *engine) fetchSegments(ctx context.Context, idx int, segs []rangeSpan) error {
	for _, seg := range segs {
		var err error
		for attempt := 0; attempt < 6; attempt++ {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			err = e.fetchSegment(ctx, idx, seg)
			if err == nil {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 2 * time.Second):
			}
		}
		if err != nil {
			return fmt.Errorf("range %d-%d: %w", seg.Start, seg.End, err)
		}
	}
	return nil
}

func (e *engine) fetchSegment(ctx context.Context, idx int, seg rangeSpan) error {
	req, err := http.NewRequestWithContext(ctx, "GET", e.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", seg.Start, seg.End-1))
	req.Header.Set("User-Agent", "lm-get/"+api.Version)
	e.updatePartial(idx, 0, 0)

	resp, err := api.DLClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	buf := make([]byte, 1024*1024)
	var done int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := e.f.WriteAt(buf[:n], seg.Start+done); wErr != nil {
				e.inflight.Add(-done)
				return wErr
			}
			done += int64(n)
			e.inflight.Add(int64(n))
			e.updatePartial(idx, seg.Start, seg.Start+done)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			e.inflight.Add(-done)
			return readErr
		}
	}

	if done != seg.End-seg.Start {
		e.inflight.Add(-done)
		return fmt.Errorf("short read: got %d of %d bytes", done, seg.End-seg.Start)
	}

	e.inflight.Add(-done)
	e.written.Add(done)
	e.updatePartial(idx, 0, 0)
	// Make written bytes durable before the state file claims them, so a
	// resumed download is valid even after power loss.
	e.mu.Lock()
	e.f.Sync()
	e.mu.Unlock()
	e.markDone(seg)
	return nil
}

// markDone merges a completed span into the in-memory state. Persistence
// happens via periodic snapshotState calls.
func (e *engine) markDone(seg rangeSpan) {
	e.mu.Lock()
	defer e.mu.Unlock()
	merged := append(e.done, seg)
	sort.Slice(merged, func(i, j int) bool { return merged[i].Start < merged[j].Start })

	var compact []rangeSpan
	for _, r := range merged {
		if n := len(compact); n > 0 && r.Start <= compact[n-1].End {
			if r.End > compact[n-1].End {
				compact[n-1].End = r.End
			}
		} else {
			compact = append(compact, r)
		}
	}
	e.done = compact
}

func (e *engine) finish() error {
	if err := e.f.Sync(); err != nil {
		e.f.Close()
		return err
	}
	if err := e.f.Close(); err != nil {
		return err
	}
	if e.state != nil {
		e.state.Close()
	}
	return os.Remove(e.stateLoc)
}

// singleStream handles servers without range support.
func (e *engine) singleStream() error {
	req, err := http.NewRequest("GET", e.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "lm-get/"+api.Version)

	resp, err := api.DLClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	if e.total > 0 {
		freeSpace := api.GetDiskFree(filepath.Dir(e.dest))
		if freeSpace >= 0 && e.total > freeSpace {
			return fmt.Errorf("not enough disk space: need %s, have %s free", display.FormatSize(e.total), display.FormatSize(freeSpace))
		}
	}

	os.Remove(e.stateLoc)
	f, err := os.OpenFile(e.dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, 1024*1024)
	var downloaded int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := f.Write(buf[:n]); wErr != nil {
				return wErr
			}
			downloaded += int64(n)
			if e.progressFn != nil {
				e.progressFn(downloaded, e.total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return f.Sync()
}

func connectionCount() int {
	conns := config.Load().ParallelConnections
	if conns < 1 {
		conns = 1
	}
	if conns > 16 {
		conns = 16
	}
	return conns
}

// File downloads with a CLI progress bar.
func File(downloadURL, destPath string) error {
	if info, err := os.Stat(destPath); err == nil && info.Size() > 0 {
		if _, stateErr := os.Stat(destPath + stateSuffix); os.IsNotExist(stateErr) {
			fmt.Printf("File exists (%s)\n", display.FormatSize(info.Size()))
			return nil
		}
		fmt.Printf("Incomplete download found (%s). Resuming...\n", display.FormatSize(info.Size()))
	}

	progress := func(current, total int64) {
		if total > 0 {
			fmt.Printf("\r  %s", display.ProgressBar(current, total, 40))
		} else {
			fmt.Printf("\r  %s downloaded", display.FormatSize(current))
		}
	}

	if err := runEngine(downloadURL, destPath, connectionCount(), progress); err != nil {
		return err
	}
	fmt.Println()
	return nil
}

// FileWithRetries wraps File with whole-engine retries; resume makes each
// attempt continue where the last one died.
func FileWithRetries(downloadURL, destPath string, attempts int) error {
	var err error
	for i := 0; i < attempts; i++ {
		err = File(downloadURL, destPath)
		if err == nil {
			return nil
		}
		fmt.Printf("%sretry %d/%d: %v%s\n", display.Yellow, i+1, attempts, err, display.Reset)
		time.Sleep(time.Duration(i+1) * 2 * time.Second)
	}
	return err
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
		if err := FileWithRetries(downloadURL, destPath, 3); err != nil {
			return fmt.Errorf("failed to download %s: %w", filepath.Base(p), err)
		}
	}
	return nil
}

// ToFile downloads with channel-based progress (for TUI).
// Sends cumulative byte counts; file is guaranteed complete on nil return.
func ToFile(downloadURL, destPath string, progress chan<- int64) error {
	var lastSent time.Time
	progressFn := func(current, total int64) {
		now := time.Now()
		if now.Sub(lastSent) > 150*time.Millisecond {
			lastSent = now
			select {
			case progress <- current:
			default:
			}
		}
	}

	err := runEngine(downloadURL, destPath, connectionCount(), progressFn)
	return err
}

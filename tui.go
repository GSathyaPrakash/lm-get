package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type view int

const (
	viewInput view = iota
	viewSearch
	viewLoading
	viewDetail
	viewDownloading
	viewMmprojPrompt
	viewServerLogs
)

var sortOptions = []string{"downloads", "likes", "lastModified"}

type tuiModel struct {
	state        view
	input        textinput.Model
	models       []HFModel
	entries      []DetailEntry
	rawFiles     []HFFileTree
	selected     int
	fileCursor   int
	scrollOff    int
	readmeScroll int
	query        string
	repo         string
	err          string
	windowW      int
	windowH      int
	totalRAM     int64
	downloading  bool
	dlCurrent    int64
	dlTotal      int64
	dlDone       bool
	dlOK         bool
	dlFileName   string
	dlDestPath   string
	sortIdx      int
	page         int
	showHelp     bool
	readme       string
	detailFocus  int
	mmprojEntry  *DetailEntry
	dlQueue      []string
	dlQueueIdx   int
	nameScroll   int
	localModels  []localModel
	localCursor  int
	localScroll  int
	localSortBy  localSort
	localFocus   bool
	deleteConfirm bool
	deleteTarget  string
	runningProcs  []*os.Process
	serverLogs    []string
	serverName    string
	viewLogs      bool
	logScroll     int
	logCh         chan string
}

type localRow struct {
	repoIdx int
	fileIdx int
	isFile  bool
}

func (m *tuiModel) localRows() []localRow {
	var rows []localRow
	for ri, mod := range m.localModels {
		rows = append(rows, localRow{repoIdx: ri})
		if mod.Expanded {
			for fi := range mod.Files {
				rows = append(rows, localRow{repoIdx: ri, fileIdx: fi, isFile: true})
			}
		}
	}
	return rows
}

func (m *tuiModel) currentLocalRow() *localRow {
	rows := m.localRows()
	if m.localCursor >= 0 && m.localCursor < len(rows) {
		return &rows[m.localCursor]
	}
	return nil
}

type searchResultMsg struct {
	models []HFModel
	err    error
}

type filesResultMsg struct {
	files  []HFFileTree
	readme string
	err    error
}

type dlProgressMsg struct {
	current int64
	total   int64
	done    bool
	ok      bool
	ch      chan dlProgressMsg
}

func newTUIModel() tuiModel {
	ti := textinput.New()
	ti.Placeholder = "Search models (e.g. qwen, llama, mistral)..."
	ti.Focus()
	ti.CharLimit = 100
	ti.Width = 50

	cfg := loadConfig()
	sortIdx := 0
	for i, s := range sortOptions {
		if s == cfg.DefaultSort {
			sortIdx = i
			break
		}
	}

	models := scanLocalModels()
	sortLocalModels(models, 0)

	return tuiModel{
		state:       viewInput,
		input:       ti,
		totalRAM:    getSystemRAM(),
		sortIdx:     sortIdx,
		windowW:     80,
		windowH:     24,
		page:        1,
		localModels: models,
		localSortBy: localSortTime,
	}
}

func (m tuiModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.showHelp {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "?", "esc", "q":
				m.showHelp = false
			}
			return m, nil
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.windowW = msg.Width
		m.windowH = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.deleteConfirm {
			switch msg.String() {
			case "y", "Y":
				if m.deleteTarget != "" {
					cfg := loadConfig()
					targetPath := filepath.Join(expandHome(cfg.DownloadsDir), m.deleteTarget)
					os.RemoveAll(targetPath)
				}
				m.deleteConfirm = false
				m.deleteTarget = ""
				m.refreshLocal()
				m.localCursor = 0
				m.localScroll = 0
				return m, nil
			case "n", "N", "esc":
				m.deleteConfirm = false
				m.deleteTarget = ""
				return m, nil
			}
			return m, nil
		}

		if m.state == viewInput {
			if m.viewLogs {
				switch msg.String() {
				case "esc", "l":
					m.viewLogs = false
					return m, nil
				case "k":
					for _, p := range m.runningProcs {
						syscall.Kill(-p.Pid, syscall.SIGTERM)
					}
					for _, p := range m.runningProcs {
						p.Wait()
					}
					m.runningProcs = nil
					m.serverLogs = append(m.serverLogs, "", "[Server stopped]")
					return m, nil
				case "up":
					if m.logScroll > 0 {
						m.logScroll--
					}
					return m, nil
				case "down":
					m.logScroll++
					return m, nil
				}
				return m, nil
			}
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc":
				return m, tea.Quit
			case "tab":
				if len(m.localModels) > 0 {
					m.localFocus = !m.localFocus
					if m.localFocus {
						m.input.Blur()
					} else {
						m.input.Focus()
					}
				}
				return m, nil
			case "enter":
					if m.localFocus {
						row := m.currentLocalRow()
						if row != nil {
							if row.isFile {
								cmd := m.runLocalFile(row)
								return m, cmd
							} else {
								m.localModels[row.repoIdx].Expanded = !m.localModels[row.repoIdx].Expanded
							}
						}
						return m, nil
					}
					return m.handleEnter()
					default:
					if m.localFocus {
						switch msg.String() {
						case "up":
							return m.handleLocalUp()
						case "down":
							return m.handleLocalDown()
						case "s":
							m.localSortBy = (m.localSortBy + 1) % 2
							sortLocalModels(m.localModels, m.localSortBy)
							m.localCursor = 0
							m.localScroll = 0
							return m, nil
						case "d":
							row := m.currentLocalRow()
							if row != nil {
								m.deleteConfirm = true
								if row.isFile {
									m.deleteTarget = m.localModels[row.repoIdx].Repo + "/" + m.localModels[row.repoIdx].Files[row.fileIdx].Name
								} else {
									m.deleteTarget = m.localModels[row.repoIdx].Repo
								}
							}
							return m, nil
						case "r":
							row := m.currentLocalRow()
							if row != nil && row.isFile {
								cmd := m.runLocalFile(row)
								return m, cmd
							}
							return m, nil
						case "k":
							for _, p := range m.runningProcs {
								syscall.Kill(-p.Pid, syscall.SIGTERM)
							}
							for _, p := range m.runningProcs {
								p.Wait()
							}
							m.runningProcs = nil
							m.serverLogs = append(m.serverLogs, "", "[Server stopped]")
							return m, nil
						case "l":
							if len(m.serverLogs) > 0 {
								m.viewLogs = true
							}
							return m, nil
						}
						return m, nil
					}
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				return m, cmd
			}
		}

		if m.state == viewMmprojPrompt {
			switch msg.String() {
			case "y", "Y":
				if m.mmprojEntry != nil {
					cfg := loadConfig()
					destDir := filepath.Join(expandHome(cfg.DownloadsDir), m.repo)
					paths := m.mmprojEntry.Paths
					m.dlQueue = paths
					m.dlQueueIdx = 0
					m.dlFileName = filepath.Base(paths[0])
					m.dlDestPath = filepath.Join(destDir, filepath.Base(paths[0]))
					m.dlCurrent = 0
					m.dlTotal = m.mmprojEntry.TotalSize
					m.dlDone = false
					m.dlOK = false
					m.downloading = true
					m.state = viewDownloading
					url := getDownloadURL(m.repo, paths[0])
					return m, doDownload(url, m.dlDestPath, m.mmprojEntry.TotalSize, nil)
				}
				m.state = viewDetail
				return m, nil
			case "n", "N", "esc":
				m.state = viewDetail
				return m, nil
			case "ctrl+c":
				return m, tea.Quit
			}
			return m, nil
		}

		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			return m.handleEsc()
		case "enter":
			return m.handleEnter()
		case "up", "k":
			return m.handleUp()
		case "down", "j":
			return m.handleDown()
		case "s":
			if m.state == viewSearch {
				m.sortIdx = (m.sortIdx + 1) % len(sortOptions)
				m.page = 1
				m.state = viewLoading
				cfg := loadConfig()
				return m, doSearch(m.query, cfg.ResultsPerPage, sortOptions[m.sortIdx], "-1", m.page)
			}
		case "left":
			if m.state == viewSearch && m.page > 1 {
				m.page--
				m.state = viewLoading
				cfg := loadConfig()
				return m, doSearch(m.query, cfg.ResultsPerPage, sortOptions[m.sortIdx], "-1", m.page)
			}
		case "right":
			if m.state == viewSearch {
				m.page++
				m.state = viewLoading
				cfg := loadConfig()
				return m, doSearch(m.query, cfg.ResultsPerPage, sortOptions[m.sortIdx], "-1", m.page)
			}
		case "tab":
			if m.state == viewDetail {
				m.detailFocus = (m.detailFocus + 1) % 2
				m.scrollOff = 0
			}
		case "?":
			m.showHelp = true
			return m, nil
		case "q":
			if m.state == viewDownloading {
				return m, tea.Quit
			}
			if m.state == viewSearch {
				m.state = viewInput
				m.input.SetValue("")
				m.err = ""
				m.page = 1
				m.refreshLocal()
				return m, nil
			}
			if m.state == viewDetail {
				m.state = viewSearch
				m.scrollOff = 0
				return m, nil
			}
		}

	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseWheelUp:
			if m.state == viewInput && m.localFocus {
				return m.handleLocalUp()
			}
			return m.handleScrollUp()
		case tea.MouseWheelDown:
			if m.state == viewInput && m.localFocus {
				return m.handleLocalDown()
			}
			return m.handleScrollDown()
		}

	case searchResultMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			m.state = viewInput
			m.refreshLocal()
			return m, nil
		}
		m.models = msg.models
		m.selected = 0
		m.scrollOff = 0
		if len(m.models) == 0 {
			m.err = fmt.Sprintf("No GGUF models found for '%s'", m.query)
			m.state = viewInput
			m.refreshLocal()
			return m, nil
		}
		m.state = viewSearch
		return m, nil

	case filesResultMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			m.state = viewSearch
			return m, nil
		}
		m.rawFiles = msg.files
		mf := parseModelFiles(msg.files)
		m.entries = buildDetailEntries(mf, m.repo)
		m.readme = msg.readme
		m.fileCursor = 0
		m.scrollOff = 0
		m.readmeScroll = 0
		m.detailFocus = 1
		m.state = viewDetail
		return m, nil

	case dlProgressMsg:
		m.dlCurrent = msg.current
		m.dlTotal = msg.total
		if msg.done {
			m.dlDone = true
			m.dlOK = msg.ok
			m.downloading = false
			return m, nil
		}
		return m, waitForProgress(msg.ch)

	case serverLogMsg:
		if msg.line != "" {
			m.serverLogs = append(m.serverLogs, msg.line)
		}
		if m.logCh != nil {
			return m, waitForServerLog(m.logCh)
		}
	}

	return m, nil
}

func (m *tuiModel) refreshLocal() {
	m.localModels = scanLocalModels()
	sortLocalModels(m.localModels, m.localSortBy)
	if m.localCursor >= len(m.localModels) {
		m.localCursor = len(m.localModels) - 1
	}
	if m.localCursor < 0 {
		m.localCursor = 0
	}
}

func (m tuiModel) handleEsc() (tea.Model, tea.Cmd) {
	switch m.state {
	case viewInput:
		return m, tea.Quit
	case viewSearch:
		m.state = viewInput
		m.input.SetValue("")
		m.err = ""
		m.page = 1
	case viewDetail:
		m.state = viewSearch
		m.scrollOff = 0
	case viewDownloading:
		return m, tea.Quit
	}
	return m, nil
}

func (m tuiModel) handleEnter() (tea.Model, tea.Cmd) {
	switch m.state {
	case viewInput:
		m.query = m.input.Value()
		if strings.TrimSpace(m.query) == "" {
			m.err = "Please enter a search query"
			return m, nil
		}
		m.err = ""
		m.page = 1
		m.state = viewLoading
		cfg := loadConfig()
		return m, doSearch(m.query, cfg.ResultsPerPage, sortOptions[m.sortIdx], "-1", m.page)

	case viewSearch:
		if len(m.models) > 0 && m.selected < len(m.models) {
			m.repo = m.models[m.selected].ID
			m.state = viewLoading
			return m, doListFiles(m.repo)
		}

	case viewDetail:
		if len(m.entries) > 0 && m.fileCursor < len(m.entries) {
			entry := m.entries[m.fileCursor]
			if entry.IsMmproj {
				break
			}
			cfg := loadConfig()
			destDir := filepath.Join(expandHome(cfg.DownloadsDir), m.repo)

			m.dlQueue = entry.Paths
			m.dlQueueIdx = 0
			m.dlFileName = entry.DisplayName
			m.dlCurrent = 0
			m.dlTotal = entry.TotalSize
			m.dlDone = false
			m.dlOK = false
			m.downloading = true
			m.state = viewDownloading

			firstPath := entry.Paths[0]
			m.dlDestPath = filepath.Join(destDir, filepath.Base(firstPath))
			url := getDownloadURL(m.repo, firstPath)
			return m, doDownload(url, m.dlDestPath, entry.TotalSize, entry.Paths[1:])
		}

	case viewDownloading:
		if m.dlDone {
			if m.dlOK {
				hasMmproj := false
				var mmprojEntry *DetailEntry
				for i := range m.entries {
					if m.entries[i].IsMmproj && !m.entries[i].Downloaded {
						hasMmproj = true
						mmprojEntry = &m.entries[i]
						break
					}
				}
				if hasMmproj && mmprojEntry != nil {
					m.mmprojEntry = mmprojEntry
					m.state = viewMmprojPrompt
					return m, nil
				}
			}
			mf := parseModelFiles(m.rawFiles)
			m.entries = buildDetailEntries(mf, m.repo)
			m.state = viewDetail
			return m, nil
		}
	}

	return m, nil
}

func (m tuiModel) handleUp() (tea.Model, tea.Cmd) {
	switch m.state {
	case viewSearch:
		if m.selected > 0 {
			m.selected--
			if m.selected < m.scrollOff {
				m.scrollOff = m.selected
			}
		}
	case viewDetail:
		if m.detailFocus == 1 {
			if m.fileCursor > 0 {
				m.fileCursor--
				if m.fileCursor < m.scrollOff {
					m.scrollOff = m.fileCursor
				}
			}
		} else {
			if m.readmeScroll > 0 {
				m.readmeScroll--
			}
		}
	}
	return m, nil
}

func (m tuiModel) handleDown() (tea.Model, tea.Cmd) {
	switch m.state {
	case viewSearch:
		if m.selected < len(m.models)-1 {
			m.selected++
			visibleH := m.visibleH()
			if m.selected >= m.scrollOff+visibleH {
				m.scrollOff = m.selected - visibleH + 1
			}
		}
	case viewDetail:
		if m.detailFocus == 1 {
			if m.fileCursor < len(m.entries)-1 {
				m.fileCursor++
				visibleH := m.visibleH()
				if m.fileCursor >= m.scrollOff+visibleH {
					m.scrollOff = m.fileCursor - visibleH + 1
				}
			}
		} else {
			m.readmeScroll++
		}
	}
	return m, nil
}

func (m tuiModel) visibleH() int {
	h := m.windowH - 5
	if h < 1 {
		return 20
	}
	return h
}

func (m tuiModel) handleScrollUp() (tea.Model, tea.Cmd) {
	if m.state == viewDetail {
		if m.detailFocus == 0 {
			if m.readmeScroll > 0 {
				m.readmeScroll--
			}
		} else {
			return m.handleUp()
		}
	} else {
		return m.handleUp()
	}
	return m, nil
}

func (m tuiModel) handleScrollDown() (tea.Model, tea.Cmd) {
	if m.state == viewDetail {
		if m.detailFocus == 0 {
			m.readmeScroll++
		} else {
			return m.handleDown()
		}
	} else {
		return m.handleDown()
	}
	return m, nil
}

func (m tuiModel) handleLocalUp() (tea.Model, tea.Cmd) {
	rows := m.localRows()
	if m.localCursor > 0 {
		m.localCursor--
		if m.localCursor < m.localScroll {
			m.localScroll = m.localCursor
		}
	}
	_ = rows
	return m, nil
}

func (m tuiModel) handleLocalDown() (tea.Model, tea.Cmd) {
	rows := m.localRows()
	if m.localCursor < len(rows)-1 {
		m.localCursor++
		vis := m.windowH - 7
		if vis < 3 {
			vis = 5
		}
		if m.localCursor >= m.localScroll+vis {
			m.localScroll = m.localCursor - vis + 1
		}
	}
	return m, nil
}

type serverLogMsg struct {
	line string
}

func waitForServerLog(ch chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return nil
		}
		return serverLogMsg{line: line}
	}
}

func (m *tuiModel) runLocalFile(row *localRow) tea.Cmd {
	if row == nil || !row.isFile {
		return nil
	}
	mod := m.localModels[row.repoIdx]
	f := mod.Files[row.fileIdx]
	cfg := loadConfig()
	modelPath := filepath.Join(expandHome(cfg.DownloadsDir), mod.Repo, f.Name)
	runCmd := cfg.RunCommand
	runCmd = strings.ReplaceAll(runCmd, "{model}", modelPath)
	parts := strings.Fields(runCmd)
	if len(parts) == 0 {
		return nil
	}
	c := exec.Command(parts[0], parts[1:]...)
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	logR, logW, _ := os.Pipe()
	c.Stdout = logW
	c.Stderr = logW
	if err := c.Start(); err != nil {
		logW.Close()
		logR.Close()
		return nil
	}
	logW.Close()
	m.runningProcs = append(m.runningProcs, c.Process)
	m.serverName = f.Name
	m.serverLogs = []string{fmt.Sprintf("$ %s", runCmd), ""}
	m.logScroll = 0
	m.viewLogs = true

	m.logCh = make(chan string, 256)
	go func(r *os.File, ch chan string) {
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				for _, l := range strings.Split(chunk, "\n") {
					if l != "" {
						ch <- l
					}
				}
			}
			if err != nil {
				break
			}
		}
		r.Close()
		close(ch)
	}(logR, m.logCh)

	return waitForServerLog(m.logCh)
}

func (m tuiModel) View() string {
	if m.showHelp {
		return m.viewHelp()
	}
	switch m.state {
	case viewInput:
		if m.viewLogs {
			return m.viewServerLogs()
		}
		return m.viewInput()
	case viewLoading:
		return m.viewLoading()
	case viewSearch:
		return m.viewSearch()
	case viewDetail:
		return m.viewDetail()
	case viewDownloading:
		return m.viewDownloading()
	case viewMmprojPrompt:
		return m.viewMmprojPrompt()
	}
	return ""
}

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	selStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("228")).Background(lipgloss.Color("63"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	greenStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	redStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	yellowStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	blueStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	mmprojTag     = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
	downloadedTag = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	boxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	focusedBox    = lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).Padding(0, 1).BorderForeground(lipgloss.Color("14"))
)

func stripHTML(s string) string {
	var out strings.Builder
	inTag := false
	inEntity := false
	for i := 0; i < len(s); i++ {
		if s[i] == '<' {
			inTag = true
			continue
		}
		if s[i] == '>' && inTag {
			inTag = false
			continue
		}
		if s[i] == '&' {
			inEntity = true
			continue
		}
		if s[i] == ';' && inEntity {
			inEntity = false
			continue
		}
		if !inTag && !inEntity {
			out.WriteByte(s[i])
		}
	}
	return out.String()
}

func stripMarkdown(s string) string {
	s = stripHTML(s)
	lines := strings.Split(s, "\n")
	var out strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") {
			continue
		}
		if strings.HasPrefix(trimmed, "---") {
			continue
		}
		if strings.HasPrefix(trimmed, "![") {
			continue
		}
		if strings.HasPrefix(trimmed, "| ") {
			continue
		}

		line = strings.TrimPrefix(line, "### ")
		line = strings.TrimPrefix(line, "## ")
		line = strings.TrimPrefix(line, "# ")
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimPrefix(line, "> ")

		for strings.Contains(line, "**") {
			line = strings.Replace(line, "**", "", 1)
		}
		for strings.Contains(line, "__") {
			line = strings.Replace(line, "__", "", 1)
		}
		line = strings.ReplaceAll(line, "`", "")

		if len(strings.TrimSpace(line)) > 0 {
			out.WriteString(line)
			out.WriteString("\n")
		}
	}
	return out.String()
}

func regexpReplace(s, pattern, repl string) string {
	return s
}

func wordWrap(text string, width int) string {
	if width <= 0 {
		return text
	}
	var b strings.Builder
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if len(line) == 0 {
			b.WriteString("\n")
			continue
		}
		words := strings.Fields(line)
		lineLen := 0
		for _, word := range words {
			if lineLen+len(word)+1 > width {
				b.WriteString("\n")
				lineLen = 0
			}
			if lineLen > 0 {
				b.WriteString(" ")
				lineLen++
			}
			b.WriteString(word)
			lineLen += len(word)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (m tuiModel) viewHelp() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("lm-get — Help"))
	b.WriteString("\n\n")

	helpItems := []struct{ key, desc string }{
		{"↑/k, ↓/j", "Navigate up/down"},
		{"Enter", "Select / Download"},
		{"Esc", "Go back"},
		{"q", "Back / Quit"},
		{"s", "Cycle sort (search or downloaded panel)"},
		{"d", "Delete model (downloaded panel)"},
		{"r", "Run model with llama-server (downloaded panel)"},
		{"k", "Kill running server"},
		{"l", "View server logs"},
		{"←/→", "Previous/Next page (in search)"},
		{"Tab", "Switch focus: search ↔ downloaded / README ↔ Files"},
		{"?", "Toggle this help"},
		{"Ctrl+C", "Force quit"},
	}

	for _, item := range helpItems {
		b.WriteString(fmt.Sprintf("  %s%-12s%s  %s\n", cyan, item.key, reset, item.desc))
	}

	b.WriteString("\n" + helpStyle.Render("Press ? or Esc to close"))
	return b.String()
}

func (m tuiModel) viewInput() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("lm-get"))
	b.WriteString(dimStyle.Render(" — Search and download GGUF models from Hugging Face"))
	b.WriteString("\n\n")

	if !m.localFocus {
		b.WriteString(m.input.View())
	} else {
		b.WriteString(dimStyle.Render(m.input.View()))
	}

	if m.err != "" {
		b.WriteString("\n")
		b.WriteString(errStyle.Render("[ERROR] " + m.err))
	}

	b.WriteString("\n")

	sortLabel := sortOptions[m.sortIdx]
	if m.localFocus {
		b.WriteString(helpStyle.Render(fmt.Sprintf("Tab: focus search · sort: %s · ? help", sortLabel)))
	} else {
		b.WriteString(helpStyle.Render(fmt.Sprintf("Enter to search · Tab: focus models · sort: %s · ? help · Esc quit", sortLabel)))
	}

	b.WriteString("\n")

	if len(m.localModels) > 0 {
		panelTitle := "Downloaded Models"
		if m.localFocus {
			panelTitle = headerStyle.Render("Downloaded Models ◀")
		} else {
			panelTitle = dimStyle.Render("Downloaded Models")
		}
		b.WriteString(panelTitle)
		localSortLabel := localSortNames[m.localSortBy]
		b.WriteString(dimStyle.Render(fmt.Sprintf(" (%d models, sort: %s)", len(m.localModels), localSortLabel)))
		b.WriteString("\n")

		rows := m.localRows()

		panelH := m.windowH - 7
		if panelH < 3 {
			panelH = 5
		}
		if m.localScroll >= len(rows) {
			m.localScroll = len(rows) - 1
		}
		if m.localScroll < 0 {
			m.localScroll = 0
		}
		end := m.localScroll + panelH
		if end > len(rows) {
			end = len(rows)
		}

		nameW := m.windowW - 46
		if nameW < 15 {
			nameW = 15
		}

		for i := m.localScroll; i < end; i++ {
			r := rows[i]
			mod := m.localModels[r.repoIdx]
			cursor := "  "
			style := lipgloss.NewStyle()
			if i == m.localCursor && m.localFocus {
				cursor = cursorStyle.Render("> ")
				style = selStyle
			}

			if r.isFile {
				f := mod.Files[r.fileIdx]
				relTime := relativeTime(f.ModTime)
				fname := truncate(f.Name, nameW-2)
				line := fmt.Sprintf("  %-*s  %-9s  %-12s", nameW-2, fname, formatSize(f.Size), relTime)
				b.WriteString(cursor)
				b.WriteString(style.Render(line))
			} else {
				relTime := relativeTime(mod.ModTime)
				expandIcon := "▸"
				if mod.Expanded {
					expandIcon = "▾"
				}
				name := truncate(mod.Repo, nameW-3)
				line := fmt.Sprintf("%s %-*s  %-9s  %2d files  %-12s",
					expandIcon, nameW-3, name, formatSize(mod.Size), len(mod.Files), relTime)
				b.WriteString(cursor)
				b.WriteString(style.Render(line))
			}
			b.WriteString("\n")
		}
	}

	if m.deleteConfirm {
		b.WriteString("\n")
		b.WriteString(yellowStyle.Render(fmt.Sprintf("Delete %s? [y/N]", m.deleteTarget)))
	}

	if len(m.localModels) == 0 {
		b.WriteString(dimStyle.Render("\nNo downloaded models"))
	} else if m.localFocus {
		b.WriteString(helpStyle.Render("Enter expand/run · r run · l logs · k kill · d delete · s sort · Tab: focus search"))
	}

	return b.String()
}

func (m tuiModel) viewLoading() string {
	return fmt.Sprintf("\n  Loading...\n\n  %s", helpStyle.Render("Please wait"))
}

func (m tuiModel) viewSearch() string {
	var b strings.Builder

	sortLabel := sortOptions[m.sortIdx]
	b.WriteString(headerStyle.Render(fmt.Sprintf("Search: %s", m.query)))
	b.WriteString(dimStyle.Render(fmt.Sprintf("  (page %d, sort: %s, %d models)", m.page, sortLabel, len(m.models))))
	b.WriteString("\n\n")

	visibleH := m.visibleH()
	end := m.scrollOff + visibleH
	if end > len(m.models) {
		end = len(m.models)
	}

	nameColW := m.windowW - 55
	if nameColW < 20 {
		nameColW = 30
	}
	if nameColW > 55 {
		nameColW = 55
	}

	for i := m.scrollOff; i < end; i++ {
		mod := m.models[i]
		cursor := "  "
		style := lipgloss.NewStyle()
		if i == m.selected {
			cursor = cursorStyle.Render("> ")
			style = selStyle
		}

		t, _ := time.Parse(time.RFC3339, mod.LastModified)
		relTime := ""
		if !t.IsZero() {
			relTime = relativeTime(t)
		}

		ggufCount := 0
		for _, s := range mod.Siblings {
			if strings.HasSuffix(strings.ToLower(s.RFilename), ".gguf") {
				ggufCount++
			}
		}

		name := truncate(mod.ID, nameColW)
		line := fmt.Sprintf("%-"+fmt.Sprintf("%d", nameColW)+"s  ↓%-8s  ♥%-7s  %-7s  %-12s",
			name, formatNumber(mod.Downloads), formatNumber(mod.Likes), fmt.Sprintf("%d GGUF", ggufCount), relTime)

		b.WriteString(cursor)
		b.WriteString(style.Render(line))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	nav := "↑/↓ navigate · Enter select · s sort · ←/→ page"
	if m.page > 1 {
		nav += " (p prev)"
	}
	nav += " · ? help · Esc back"
	b.WriteString(helpStyle.Render(nav))
	return b.String()
}

func (m tuiModel) viewDetail() string {
	halfW := m.windowW / 2
	if halfW < 20 {
		halfW = 30
	}
	paneH := m.windowH - 4
	if paneH < 5 {
		paneH = 10
	}

	var leftB strings.Builder
	leftTitle := dimStyle.Render("README")
	if m.detailFocus == 0 {
		leftTitle = headerStyle.Render("README ◀")
	}
	leftB.WriteString(leftTitle)
	leftB.WriteString("\n")

	if m.readme != "" {
		clean := stripMarkdown(m.readme)
		wrapped := wordWrap(clean, halfW-2)
		lines := strings.Split(wrapped, "\n")
		totalLines := len(lines)
		start := m.readmeScroll
		if start > totalLines-paneH+1 {
			start = totalLines - paneH + 1
		}
		if start < 0 {
			start = 0
		}
		end := start + paneH - 1
		if end > totalLines {
			end = totalLines
		}
		for _, l := range lines[start:end] {
			if lipgloss.Width(l) > halfW {
				l = truncate(l, halfW)
			}
			leftB.WriteString(l)
			leftB.WriteString("\n")
		}
	} else {
		leftB.WriteString(dimStyle.Render("No README available"))
	}

	var rightB strings.Builder
	rightTitle := dimStyle.Render("Files")
	if m.detailFocus == 1 {
		rightTitle = headerStyle.Render("Files ◀")
	}
	rightB.WriteString(rightTitle)
	rightB.WriteString(fmt.Sprintf("\n%d quantization(s)\n\n", len(m.entries)))

	visibleH := paneH - 4
	if visibleH < 3 {
		visibleH = 5
	}
	fileEnd := m.scrollOff + visibleH
	if fileEnd > len(m.entries) {
		fileEnd = len(m.entries)
	}

	for i := m.scrollOff; i < fileEnd; i++ {
		e := m.entries[i]
		cursor := "  "
		style := lipgloss.NewStyle()
		if i == m.fileCursor {
			cursor = cursorStyle.Render("> ")
			style = selStyle
		}

		ramStr := " "
		if m.totalRAM > 0 {
			if e.TotalSize <= m.totalRAM {
				ramStr = greenStyle.Render("✓")
			} else {
				ramStr = redStyle.Render("✗")
			}
		}

		sizeW := 9
		visW := 4
		shardW := 4
		dlW := 2
		tagW := visW + 1 + shardW + 1 + dlW
		nameW := halfW - sizeW - 2 - tagW - 5
		if nameW < 8 {
			nameW = 8
		}

		name := truncate(e.DisplayName, nameW)
		sizeStr := fmt.Sprintf("%*s", sizeW, formatSize(e.TotalSize))

		visStr := fmt.Sprintf("%-*s", visW, "")
		if e.IsMmproj {
			visStr = fmt.Sprintf("%-*s", visW, mmprojTag.Render("vis"))
		}

		shardStr := fmt.Sprintf("%-*s", shardW, "")
		if e.IsShard {
			shardStr = fmt.Sprintf("%-*s", shardW, blueStyle.Render(fmt.Sprintf("%dsh", e.ShardTotal)))
		}

		dlStr := fmt.Sprintf("%-*s", dlW, "")
		if e.Downloaded {
			dlStr = fmt.Sprintf("%-*s", dlW, downloadedTag.Render("✓"))
		}

		rightB.WriteString(cursor)
		rightB.WriteString(style.Render(fmt.Sprintf("%-*s %s", nameW, name, sizeStr)))
		rightB.WriteString(" ")
		rightB.WriteString(ramStr)
		rightB.WriteString(" ")
		rightB.WriteString(visStr)
		rightB.WriteString(" ")
		rightB.WriteString(shardStr)
		rightB.WriteString(" ")
		rightB.WriteString(dlStr)
		rightB.WriteString("\n")
	}

	leftStyle := lipgloss.NewStyle().Width(halfW)
	rightStyle := lipgloss.NewStyle().Width(halfW)

	leftPane := leftStyle.Render(leftB.String())
	rightPane := rightStyle.Render(rightB.String())

	joined := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)

	return headerStyle.Render(m.repo) + "\n" + joined + "\n" +
		helpStyle.Render("Tab switch · ↑/↓ nav · Enter download · ? help · Esc back")
}

func (m tuiModel) viewDownloading() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render(fmt.Sprintf("Downloading: %s", m.dlFileName)))
	b.WriteString("\n\n")

	if m.dlDone {
		if m.dlOK {
			b.WriteString(greenStyle.Render("✓ Download complete — integrity verified"))
		} else {
			b.WriteString(redStyle.Render("✗ Download complete — SIZE MISMATCH (file may be corrupted)"))
		}
		b.WriteString(fmt.Sprintf("\n  %s\n", m.dlDestPath))
		b.WriteString("\n" + helpStyle.Render("Enter to continue · Esc to quit"))
	} else {
		pct := float64(0)
		if m.dlTotal > 0 {
			pct = float64(m.dlCurrent) / float64(m.dlTotal) * 100
		}

		width := 40
		filled := int(pct / 100 * float64(width))
		bar := strings.Builder{}
		bar.WriteString("[")
		for i := 0; i < width; i++ {
			if i < filled {
				bar.WriteString("=")
			} else {
				bar.WriteString(" ")
			}
		}
		bar.WriteString("]")

		b.WriteString(fmt.Sprintf("  %s %.1f%%\n", bar.String(), pct))
		b.WriteString(fmt.Sprintf("  %s / %s\n", formatSize(m.dlCurrent), formatSize(m.dlTotal)))
		b.WriteString("\n" + helpStyle.Render("Please wait... · Esc to quit"))
	}

	return b.String()
}

func (m tuiModel) viewMmprojPrompt() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render(m.repo))
	b.WriteString("\n\n")
	b.WriteString("This model supports vision and has mmproj files available.\n\n")
	if m.mmprojEntry != nil {
		b.WriteString(fmt.Sprintf("  %s (%s)\n\n", m.mmprojEntry.DisplayName, formatSize(m.mmprojEntry.TotalSize)))
	}
	b.WriteString(helpStyle.Render("Download mmproj? "))
	b.WriteString(greenStyle.Render("[y] Yes") + "  " + redStyle.Render("[n] No"))
	b.WriteString("\n")
	return b.String()
}

func (m tuiModel) viewServerLogs() string {
	var b strings.Builder
	running := ""
	if len(m.runningProcs) > 0 {
		running = greenStyle.Render(fmt.Sprintf(" (running, pid %d)", m.runningProcs[0].Pid))
	}
	b.WriteString(headerStyle.Render(fmt.Sprintf("Server: %s", m.serverName)))
	b.WriteString(running)
	b.WriteString("\n\n")

	logH := m.windowH - 4
	if logH < 5 {
		logH = 10
	}

	totalLines := len(m.serverLogs)
	start := m.logScroll
	if start > totalLines-logH {
		start = totalLines - logH
	}
	if start < 0 {
		start = 0
	}
	end := start + logH
	if end > totalLines {
		end = totalLines
	}

	for _, l := range m.serverLogs[start:end] {
		if strings.Contains(l, "error") || strings.Contains(l, "Error") || strings.Contains(l, "ERROR") {
			b.WriteString(redStyle.Render(l))
		} else if strings.HasPrefix(l, "$") {
			b.WriteString(dimStyle.Render(l))
		} else if strings.HasPrefix(l, "[") {
			b.WriteString(yellowStyle.Render(l))
		} else if strings.Contains(l, "listening") || strings.Contains(l, "Listening") {
			b.WriteString(greenStyle.Render(l))
		} else {
			b.WriteString(l)
		}
		b.WriteString("\n")
	}

	b.WriteString(helpStyle.Render("k kill server · l/Esc back · ↑/↓ scroll"))
	return b.String()
}

func doSearch(query string, limit int, sortBy string, direction string, page int) tea.Cmd {
	return func() tea.Msg {
		models, err := searchModels(query, limit, sortBy, direction, page)
		return searchResultMsg{models: models, err: err}
	}
}

func doListFiles(repo string) tea.Cmd {
	return func() tea.Msg {
		files, err := listFiles(repo)
		var readme string
		if err == nil {
			readme, _ = fetchReadme(repo)
		}
		return filesResultMsg{files: files, readme: readme, err: err}
	}
}

func doDownload(url, destPath string, expectedSize int64, remainingPaths []string) tea.Cmd {
	progress := make(chan dlProgressMsg, 64)

	go func() {
		defer close(progress)

		sendDone := func(ok bool) {
			progress <- dlProgressMsg{done: true, ok: ok, ch: progress}
		}

		_ = os.MkdirAll(filepath.Dir(destPath), 0755)

		freeSpace := getDiskFree(filepath.Dir(destPath))
		if freeSpace >= 0 && expectedSize > freeSpace {
			sendDone(false)
			return
		}

		var err error
		for attempt := 0; attempt < 3; attempt++ {
			err = downloadToFile(url, destPath, progress)
			if err == nil {
				break
			}
			if attempt < 2 {
				time.Sleep(time.Duration(attempt+1) * time.Second)
			}
		}
		if err != nil {
			sendDone(false)
			return
		}

		ok := true
		if expectedSize > 0 {
			if info, statErr := os.Stat(destPath); statErr == nil {
				if info.Size() != expectedSize {
					ok = false
				}
			} else {
				ok = false
			}
		}

		progress <- dlProgressMsg{current: expectedSize, total: expectedSize, done: true, ok: ok, ch: progress}
	}()

	return waitForProgress(progress)
}

func downloadToFile(url, destPath string, progress chan dlProgressMsg) error {
	var existingSize int64
	if info, err := os.Stat(destPath); err == nil {
		existingSize = info.Size()
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
			if now.Sub(lastSend) > 150*time.Millisecond {
				lastSend = now
				progress <- dlProgressMsg{current: existingSize + downloaded, total: total, ch: progress}
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

func waitForProgress(ch chan dlProgressMsg) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

func runTUI() error {
	m := newTUIModel()
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

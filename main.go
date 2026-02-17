package main

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"bytes"

	"github.com/alecthomas/chroma/v2/quick"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var vaultDir string

// Editor exec

type fileEditedMsg struct {
	err error
}

type fileLoadedMsg struct {
	content string
}

type imageRenderedMsg struct{}

type clearViewportMsg struct{}

// isImageFile checks if a file is an image by extension or content detection.
func isImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".svg", ".ico", ".tiff":
		return true
	}
	// Fallback: read first 512 bytes and detect
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		return false
	}
	ct := http.DetectContentType(buf[:n])
	return strings.HasPrefix(ct, "image/")
}

// clearKittyGraphics sends the escape sequence to delete all Kitty images.
func clearKittyGraphics() tea.Cmd {
	return func() tea.Msg {
		fmt.Print("\x1b_Ga=d,d=a\x1b\\")
		return nil
	}
}

// renderImage uses chafa to generate a Kitty image escape sequence, then
// writes it directly to stdout at a specific cell offset. The output is
// *captured* first to prevent chafa's own cursor movements from wrecking
// the Bubble Tea TUI.
func renderImage(path string, cols, rows, xOffset, yOffset int) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("chafa", "-f", "kitty", "-s", fmt.Sprintf("%dx%d", cols, rows), path)
		output, err := cmd.Output()
		if err != nil {
			return imageRenderedMsg{}
		}
		// Save cursor, move to preview pane origin, write image, restore cursor.
		// All written as a single os.Stdout.Write to minimize interference
		// with Bubble Tea's rendering loop.
		var buf bytes.Buffer
		buf.WriteString("\x1b[s")                                     // save cursor
		buf.WriteString(fmt.Sprintf("\x1b[%d;%dH", yOffset, xOffset)) // move to preview pane
		buf.Write(output)                                             // kitty escape sequence
		buf.WriteString("\x1b[u")                                     // restore cursor
		os.Stdout.Write(buf.Bytes())
		return imageRenderedMsg{}
	}
}

// readFile loads text file content with syntax highlighting.
// Images are NOT handled here — they use renderImage() instead.
func readFile(path string) tea.Cmd {
	return func() tea.Msg {
		content, err := os.ReadFile(path)
		if err != nil {
			return fileLoadedMsg{content: "Error reading file"}
		}

		// Check extension first to bypass flaky http.DetectContentType
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".md", ".markdown", ".txt", ".go", ".c", ".cpp", ".h", ".py", ".js", ".ts", ".html", ".css", ".json", ".yaml", ".yml", ".toml", ".sh", ".mod", ".sum":
			// Known text — skip binary detection
		default:
			buffer := make([]byte, 512)
			copy(buffer, content)
			contentType := http.DetectContentType(buffer)

			if strings.HasPrefix(contentType, "audio/") ||
				strings.HasPrefix(contentType, "video/") ||
				contentType == "application/octet-stream" {
				return fileLoadedMsg{content: fmt.Sprintf("[Binary file: %s]", contentType)}
			}
		}

		var buf bytes.Buffer
		err = quick.Highlight(&buf, string(content), "markdown", "terminal256", "monokai")
		if err != nil {
			return fileLoadedMsg{content: string(content)}
		}

		return fileLoadedMsg{content: buf.String()}
	}
}

func getEditor() string {
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	return "nvim"
}

func openInEditor(path string) tea.Cmd {
	cmd := exec.Command(getEditor(), path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return fileEditedMsg{err: err}
	})
}

// List item

type item struct {
	title   string
	desc    string
	modTime time.Time
	creTime time.Time
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title }

// Keys

type keyMap struct {
	New           key.Binding
	Rename        key.Binding
	Delete        key.Binding
	TogglePreview key.Binding
	CycleSort     key.Binding
	Quit          key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.New, k.Rename, k.Delete, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.New, k.Rename, k.Delete},
		{k.TogglePreview, k.CycleSort, k.Quit},
	}
}

// Sort modes
type sortMode int

const (
	sortModifiedDesc sortMode = iota
	sortModifiedAsc
	sortCreatedDesc
	sortCreatedAsc
)

func (s sortMode) String() string {
	switch s {
	case sortModifiedDesc:
		return "Modified (Newest)"
	case sortModifiedAsc:
		return "Modified (Oldest)"
	case sortCreatedDesc:
		return "Created (Newest)"
	case sortCreatedAsc:
		return "Created (Oldest)"
	default:
		return "Unknown"
	}
}

// Model

type model struct {
	list         list.Model
	input        textinput.Model
	viewport     viewport.Model
	keys         keyMap
	help         help.Model
	inputMode    bool
	renameMode   bool
	renameTarget string
	ready        bool
	selectedFile string
	showPreview  bool
	showingImage bool
	width        int
	height       int
	sortMode     sortMode
	deleting     bool
}

// Init

func (m model) Init() tea.Cmd { return nil }

func initialModel() model {
	if err := os.MkdirAll(vaultDir, 0o755); err != nil {
		log.Fatal(err)
	}

	items := listFiles(sortModifiedDesc)

	delegate := list.NewDefaultDelegate()
	l := list.New(items, delegate, 0, 0)

	ti := textinput.New()
	ti.Placeholder = "filename (empty for default)"
	ti.CharLimit = 128
	ti.Width = 40

	keys := keyMap{
		New:           key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "new")),
		Rename:        key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "rename")),
		Delete:        key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "delete")),
		TogglePreview: key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "preview")),
		CycleSort:     key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "sort")),
		Quit:          key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
	}

	return model{
		list:        l,
		input:       ti,
		keys:        keys,
		help:        help.New(),
		showPreview: true, // Default to true
		sortMode:    sortModifiedDesc,
	}
}

// loadFileOrImage determines if a file is an image or text and dispatches
// to the appropriate handler. Images are rendered directly to stdout as an
// overlay; text is loaded into the viewport.
func (m model) loadFileOrImage(path string) tea.Cmd {
	if isImageFile(path) {
		// Calculate preview pane absolute position (1-indexed for ANSI)
		xOffset := m.width/3 + 3 // list width + gap
		yOffset := 4             // header + margins
		cols := m.viewport.Width
		rows := m.viewport.Height
		return tea.Sequence(
			clearKittyGraphics(),
			func() tea.Msg { return clearViewportMsg{} },
			renderImage(path, cols, rows, xOffset, yOffset),
		)
	}
	// Text file: clear any lingering image, then load text
	m.showingImage = false
	return tea.Sequence(
		clearKittyGraphics(),
		readFile(path),
	)
}

// Update

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		var listWidth, viewportWidth int
		if m.showPreview {
			listWidth = msg.Width / 3
			viewportWidth = msg.Width - listWidth - 4
		} else {
			listWidth = msg.Width - 2
			viewportWidth = 0
		}

		m.list.SetSize(listWidth, msg.Height-6)

		if !m.ready {
			m.viewport = viewport.New(viewportWidth, msg.Height-6)
			m.viewport.HighPerformanceRendering = false
			m.ready = true

			// Initial load if something is selected
			if m.list.SelectedItem() != nil {
				i := m.list.SelectedItem().(item)
				m.selectedFile = i.title
				path := filepath.Join(vaultDir, i.title)
				return m, m.loadFileOrImage(path)
			}

		} else {
			m.viewport.Width = viewportWidth
			m.viewport.Height = msg.Height - 6
		}

	case fileLoadedMsg:
		m.showingImage = false
		m.viewport.SetContent(msg.content)
		m.viewport.GotoTop()

	case clearViewportMsg:
		// Blank the viewport so old text doesn't bleed under image overlay
		m.viewport.SetContent(strings.Repeat("\n", m.viewport.Height))

	case imageRenderedMsg:
		// Image was drawn directly to stdout as overlay.
		m.showingImage = true

	case tea.MouseMsg:
		if msg.Button != tea.MouseButtonWheelUp && msg.Button != tea.MouseButtonWheelDown {
			return m, nil
		}

		var listWidth int
		if m.showPreview {
			listWidth = m.width / 3
		} else {
			listWidth = m.width - 2
		}

		if msg.X < listWidth {
			// Scroll List
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				// Scroll up 1 item
				m.list.CursorUp()
			case tea.MouseButtonWheelDown:
				// Scroll down 1 item
				m.list.CursorDown()
			}
			// Update selection immediately after scrolling
			if m.list.SelectedItem() != nil {
				i := m.list.SelectedItem().(item)
				if i.title != m.selectedFile {
					m.selectedFile = i.title
					path := filepath.Join(vaultDir, i.title)
					return m, m.loadFileOrImage(path)
				}
			}
		} else if m.showPreview && msg.X > listWidth {
			// Scroll Viewport
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				m.viewport.LineUp(1)
			case tea.MouseButtonWheelDown:
				m.viewport.LineDown(1)
			}
			// Update viewport in case it needs to redraw
			var cmdViewport tea.Cmd
			m.viewport, cmdViewport = m.viewport.Update(msg)
			return m, cmdViewport
		}
		return m, nil

	case fileEditedMsg:
		m.list.SetItems(listFiles(m.sortMode))
		return m, tea.EnableMouseAllMotion

	case tea.KeyMsg:
		// DELETE CONFIRMATION MODE
		if m.deleting {
			switch msg.String() {
			case "y", "Y":
				if it, ok := m.list.SelectedItem().(item); ok {
					os.Remove(filepath.Join(vaultDir, it.title))
					m.list.SetItems(listFiles(m.sortMode))
				}
				m.deleting = false
				return m, nil
			case "n", "N", "esc":
				m.deleting = false
				return m, nil
			default:
				return m, nil
			}
		}

		// INPUT MODE
		if m.inputMode {
			switch msg.String() {

			case "enter":
				name := m.input.Value()
				if name == "" {
					name = time.Now().Format("note-20060102-150405")
				}

				if m.renameMode {
					oldPath := filepath.Join(vaultDir, m.renameTarget)
					newPath := filepath.Join(vaultDir, name)

					if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
						// handle error or ignore
					}
					os.Rename(oldPath, newPath)

					m.renameMode = false
					m.inputMode = false
					m.input.SetValue("")
					m.list.SetItems(listFiles(m.sortMode))
					return m, nil
				}

				path := filepath.Join(vaultDir, name+".md")
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					// Handle error? For now assuming it works or os.Create will fail
				}
				f, _ := os.Create(path)
				f.Close()

				m.inputMode = false
				m.input.SetValue("")
				return m, openInEditor(path)

			case "esc":
				m.inputMode = false
				m.renameMode = false
				m.input.SetValue("")
				return m, nil
			}

			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}

		// NORMAL MODE
		switch {

		case key.Matches(msg, m.keys.Quit):
			// Clear any kitty images before quitting
			return m, tea.Sequence(clearKittyGraphics(), tea.Quit)

		case key.Matches(msg, m.keys.New):
			m.inputMode = true
			m.input.Focus()
			return m, nil

		case key.Matches(msg, m.keys.Delete):
			if m.list.SelectedItem() != nil {
				m.deleting = true
			}
			return m, nil

		case key.Matches(msg, m.keys.Rename):
			if it, ok := m.list.SelectedItem().(item); ok {
				m.renameMode = true
				m.renameTarget = it.title
				m.inputMode = true
				m.input.SetValue(it.title)
				m.input.Focus()
			}
			return m, nil

		case key.Matches(msg, m.keys.CycleSort):
			m.sortMode = (m.sortMode + 1) % 4
			m.list.SetItems(listFiles(m.sortMode))
			return m, nil

		case key.Matches(msg, m.keys.TogglePreview):
			m.showPreview = !m.showPreview
			return m.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})

		case msg.String() == "enter":
			// Don't open editor if list is filtering — let Enter exit filter mode
			if m.list.FilterState() == list.Filtering {
				break
			}
			if it, ok := m.list.SelectedItem().(item); ok {
				path := filepath.Join(vaultDir, it.title)
				return m, openInEditor(path)
			}
		}
	}

	var cmdList tea.Cmd
	m.list, cmdList = m.list.Update(msg)

	// Check for selection change
	var cmdRead tea.Cmd
	if m.list.SelectedItem() != nil {
		i := m.list.SelectedItem().(item)
		if i.title != m.selectedFile {
			m.selectedFile = i.title
			cmdRead = m.loadFileOrImage(filepath.Join(vaultDir, i.title))
		}
	}

	var cmdViewport tea.Cmd
	m.viewport, cmdViewport = m.viewport.Update(msg)

	return m, tea.Batch(cmd, cmdList, cmdViewport, cmdRead)
}

// View

var titleStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("230")).
	Background(lipgloss.Color("62")).
	Background(lipgloss.Color("62")).
	Padding(0, 1)

var statusStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("241")).
	MarginLeft(2)

func (m model) View() string {

	title := titleStyle.Render("YapPad")
	sortStatus := statusStyle.Render(fmt.Sprintf("Sort: %s", m.sortMode))
	header := lipgloss.JoinHorizontal(lipgloss.Center, title, sortStatus)

	if m.deleting {
		return fmt.Sprintf(
			"\n%s\n\n  Are you sure you want to delete this file? (y/n)\n",
			header,
		)
	}

	if m.inputMode {
		return fmt.Sprintf(
			"\n%s\n\n%s\n",
			header,
			m.input.View(),
		)
	}

	if m.showPreview {
		return fmt.Sprintf(
			"\n%s\n\n%s\n\n%s",
			header,
			lipgloss.JoinHorizontal(lipgloss.Top, m.list.View(), "  ", m.viewport.View()),
			m.help.View(m.keys),
		)
	}

	return fmt.Sprintf(
		"\n%s\n\n%s\n\n%s",
		header,
		m.list.View(),
		m.help.View(m.keys),
	)
}

// File listing

func listFiles(mode sortMode) []list.Item {
	var items []list.Item

	filepath.WalkDir(vaultDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip hidden files/directories (starting with .) but NOT the vault root
		if d.Name()[0] == '.' && path != vaultDir {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		modTime := info.ModTime()
		var creTime time.Time

		// Attempt to get creation time (best effort)
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			sec := stat.Ctim.Sec
			nsec := stat.Ctim.Nsec
			creTime = time.Unix(sec, nsec)
		} else {
			creTime = modTime
		}

		modStr := modTime.Format(time.RFC822)

		relPath, _ := filepath.Rel(vaultDir, path)

		items = append(items, item{
			title:   relPath,
			desc:    "Modified: " + modStr,
			modTime: modTime,
			creTime: creTime,
		})
		return nil
	})

	sort.Slice(items, func(i, j int) bool {
		itemI := items[i].(item)
		itemJ := items[j].(item)

		switch mode {
		case sortModifiedDesc:
			return itemI.modTime.After(itemJ.modTime)
		case sortModifiedAsc:
			return itemI.modTime.Before(itemJ.modTime)
		case sortCreatedDesc:
			return itemI.creTime.After(itemJ.creTime)
		case sortCreatedAsc:
			return itemI.creTime.Before(itemJ.creTime)
		default:
			return itemI.modTime.After(itemJ.modTime)
		}
	})

	return items
}

func main() {
	if len(os.Args) > 1 {
		vaultDir = os.Args[1]
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		vaultDir = filepath.Join(home, ".YapPad")
	}

	p := tea.NewProgram(initialModel(), tea.WithAltScreen(), tea.WithMouseAllMotion())

	if _, err := p.Run(); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}

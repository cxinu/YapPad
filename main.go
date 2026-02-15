package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type model struct {
	msg  string
	keys keyMap
	help help.Model
}

type keyMap struct {
	Quit key.Binding
	New  key.Binding
	List key.Binding
	Save key.Binding
	Back key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.New, k.List, k.Save, k.Back, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.New, k.List, k.Save},
		{k.Back, k.Quit},
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m model) View() string {
	style := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("255")).
		Background(lipgloss.Color("147")).
		Width(80).
		Align(lipgloss.Center)

	welcome := style.Render("Welcome to Note Maker twin :D")
	helpView := m.help.View(m.keys)
	view := ""

	return fmt.Sprintf("\n%s\n\n%s\n\n%s", welcome, view, helpView)
}

func initialModel() model {
	keys := keyMap{
		New: key.NewBinding(
			key.WithKeys("Ctrl+N"),
			key.WithHelp("Ctrl+N", "Create New File 🗒"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "Quit ⏻"),
		),
		List: key.NewBinding(
			key.WithKeys("ctrl+l"),
			key.WithHelp("ctrl+l", "list files ☰"),
		),
		Save: key.NewBinding(
			key.WithKeys("ctrl+s"),
			key.WithHelp("ctrl+s", "save ⎙"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back ➜]"),
		),
	}
	return model{
		msg:  "Welcome to Note Maker twin",
		keys: keys,
		help: help.New(),
	}
}

func main() {
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}

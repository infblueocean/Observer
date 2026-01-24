package sources

import (
	"fmt"
	"sort"
	"strings"

	"github.com/abelbrown/observer/internal/curation"
	"github.com/abelbrown/observer/internal/feeds"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Model is the /sources view - simple list with mode toggles
type Model struct {
	list          list.Model
	manager       *curation.SourceManager
	feedConfigs   []feeds.RSSFeedConfig
	width, height int
	quitting      bool
}

type sourceItem struct {
	name    string
	cfg     *curation.SourceConfig
	feedCfg *feeds.RSSFeedConfig
}

func (i sourceItem) Title() string {
	icon := map[curation.SourceMode]string{
		curation.ModeLive:   "●",
		curation.ModeSample: "◐",
		curation.ModeAuto:   "○",
		curation.ModeOff:    "×",
	}[i.cfg.Mode]

	exp := ""
	if i.cfg.Mode == curation.ModeSample {
		exp = fmt.Sprintf(" %.0f%%", i.cfg.Exposure*100)
	}
	return fmt.Sprintf("%s %s%s", icon, i.name, exp)
}

func (i sourceItem) Description() string {
	if i.feedCfg != nil {
		return i.feedCfg.Category
	}
	return ""
}

func (i sourceItem) FilterValue() string { return i.name }

// New creates a sources view
func New(manager *curation.SourceManager, feedConfigs []feeds.RSSFeedConfig) Model {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(lipgloss.Color("#58a6ff"))

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.Title = "Sources"
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)

	m := Model{list: l, manager: manager, feedConfigs: feedConfigs}
	m.refresh()
	return m
}

func (m *Model) refresh() {
	lookup := make(map[string]*feeds.RSSFeedConfig)
	for i := range m.feedConfigs {
		lookup[m.feedConfigs[i].Name] = &m.feedConfigs[i]
	}

	var items []list.Item
	for _, fc := range m.feedConfigs {
		items = append(items, sourceItem{
			name:    fc.Name,
			cfg:     m.manager.Get(fc.Name),
			feedCfg: &fc,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].(sourceItem).name < items[j].(sourceItem).name
	})

	m.list.SetItems(items)
}

func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	m.list.SetSize(w-4, h-4)
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok && m.list.FilterState() != list.Filtering {
		if item, ok := m.list.SelectedItem().(sourceItem); ok {
			switch msg.String() {
			case "l":
				m.manager.Set(item.name, curation.ModeLive, 1.0)
				m.refresh()
			case "s":
				m.manager.Set(item.name, curation.ModeSample, 0.5)
				m.refresh()
			case "a":
				m.manager.Set(item.name, curation.ModeAuto, 0)
				m.refresh()
			case "x":
				m.manager.Set(item.name, curation.ModeOff, 0)
				m.refresh()
			case "+", "=":
				newExp := item.cfg.Exposure + 0.1
				m.manager.Set(item.name, curation.ModeSample, newExp)
				m.refresh()
			case "-":
				newExp := item.cfg.Exposure - 0.1
				m.manager.Set(item.name, curation.ModeSample, newExp)
				m.refresh()
			}
		}
		if msg.String() == "q" || msg.String() == "esc" {
			m.quitting = true
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	live, sample, auto, off := m.manager.Stats()
	unconfigured := len(m.feedConfigs) - live - sample - auto - off

	header := lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e")).Render(
		fmt.Sprintf("  ● %d live  ◐ %d sample  ○ %d auto  × %d off  (%d default)",
			live, sample, auto, off, unconfigured))

	help := lipgloss.NewStyle().Foreground(lipgloss.Color("#484f58")).Render(
		"  [l]ive [s]ample [a]uto [x]off  [+/-]exposure  [/]search  [q]back")

	return strings.Join([]string{header, "", m.list.View(), help}, "\n")
}

func (m Model) IsQuitting() bool    { return m.quitting }
func (m *Model) ResetQuitting()     { m.quitting = false }

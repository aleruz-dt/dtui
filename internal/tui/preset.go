package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
	"github.com/dynatrace-oss/dtui/internal/tui/theme"
	"github.com/dynatrace-oss/dtui/internal/workspace"
)

// NamedPreset is one saved facet/search set for a catalog view. The TUI
// keeps these in memory; persistence is Options.SavePreset (personal YAML).
type NamedPreset struct {
	Name      string
	View      string
	Timeframe string
	Facets    []catalog.Facet
	Searches  []string
}

// requestSavePresetMsg asks the app to prompt for a name and persist the
// current table's server-side narrowing.
type requestSavePresetMsg struct {
	view     string
	tf       string
	facets   []catalog.Facet
	searches []string
}

type presetSaveOverlay struct {
	app      *app
	input    textinput.Model
	view     string
	tf       string
	facets   []catalog.Facet
	searches []string
	err      string
}

func (a *app) openPresetSave(msg requestSavePresetMsg) tea.Cmd {
	if a.opts.SavePreset == nil {
		return statusErr("preset save is not wired in this session")
	}
	if len(msg.facets)+len(msg.searches) == 0 {
		return statusErr("nothing to save — add a facet or server search first")
	}
	ti := newTextInput()
	ti.Prompt = "name: "
	ti.PromptStyle = theme.Crumb
	ti.CharLimit = 48
	ti.Focus()
	a.overlay = &presetSaveOverlay{
		app:      a,
		input:    ti,
		view:     msg.view,
		tf:       msg.tf,
		facets:   append([]catalog.Facet{}, msg.facets...),
		searches: append([]string{}, msg.searches...),
	}
	return textinput.Blink
}

func (o *presetSaveOverlay) Hints() []keyHint {
	return []keyHint{{"enter", "save"}, {"esc", "cancel"}}
}

func (o *presetSaveOverlay) HandleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		o.app.overlay = nil
		return nil
	case "enter":
		name := strings.TrimSpace(o.input.Value())
		if !workspace.ValidPresetName(name) {
			o.err = "name must be kebab-case (a-z, digits, hyphens)"
			return nil
		}
		p := NamedPreset{
			Name:      name,
			View:      o.view,
			Timeframe: o.tf,
			Facets:    o.facets,
			Searches:  o.searches,
		}
		if err := o.app.opts.SavePreset(p); err != nil {
			o.err = err.Error()
			return nil
		}
		if o.app.presets == nil {
			o.app.presets = map[string]NamedPreset{}
		}
		o.app.presets[name] = p
		o.app.overlay = nil
		return status(fmt.Sprintf("saved preset %s — dtui --preset %s", name, name))
	}
	var cmd tea.Cmd
	o.input, cmd = o.input.Update(msg)
	o.err = ""
	return cmd
}

func (o *presetSaveOverlay) View(width, height int) string {
	var b strings.Builder
	b.WriteString(theme.OverlayTitle.Render("save preset — "+o.view) + "\n\n")
	b.WriteString(" " + o.input.View() + "\n")
	if o.err != "" {
		b.WriteString("\n " + theme.Error.Render(o.err) + "\n")
	}
	var parts []string
	for _, f := range o.facets {
		parts = append(parts, f.Label())
	}
	for _, s := range o.searches {
		parts = append(parts, "⌕"+s)
	}
	b.WriteString("\n " + theme.Dim.Render(strings.Join(parts, "  ")) + "\n")
	b.WriteString("\n" + theme.Dim.Render("writes ~/.local/state/dtui/presets.yaml — not .dtui.yaml"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Position(0.2),
		theme.OverlayBox.Render(b.String()))
}

func (a *app) applyNamedPreset(name string) tea.Cmd {
	name = strings.TrimSpace(name)
	if name == "" {
		names := a.presetNames()
		if len(names) == 0 {
			return statusErr("no presets — open f and press W to save one")
		}
		return status("presets: " + strings.Join(names, ", ") + " — :preset <name>")
	}
	p, ok := a.presets[name]
	if !ok {
		names := a.presetNames()
		if len(names) == 0 {
			return statusErr(fmt.Sprintf("unknown preset %q", name))
		}
		return statusErr(fmt.Sprintf("unknown preset %q (available: %s)", name, strings.Join(names, ", ")))
	}
	spec := catalog.Lookup(p.View)
	if spec == nil {
		return statusErr(fmt.Sprintf("preset %q: unknown view %q", name, p.View))
	}
	tf := a.tf
	if p.Timeframe != "" {
		if parsed, ok := catalog.ParseTimeframe(p.Timeframe); ok {
			tf = parsed
			a.tf = tf
		}
	}
	return func() tea.Msg {
		return pushViewMsg{spec: spec, scope: catalog.Scope{Timeframe: tf}, replace: true, facets: p.Facets, searches: p.Searches}
	}
}

func (a *app) presetNames() []string {
	if len(a.presets) == 0 {
		return nil
	}
	names := make([]string, 0, len(a.presets))
	for n := range a.presets {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

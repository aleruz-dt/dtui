package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func addServerSearch(a *app, term string) {
	press(a, key("/"))
	typeText(a, term)
	press(a, key("enter"))
}

func TestJumpRestoresLastServerFilter(t *testing.T) {
	a := testApp(t, "logs")
	seedRows(t, a, []map[string]any{{"content": "timeout", "loglevel": "ERROR"}})
	addServerSearch(a, "timeout")
	if tv := a.top().(*tableView); len(tv.searches) != 1 || tv.searches[0] != "timeout" {
		t.Fatalf("setup searches = %v", a.top().(*tableView).searches)
	}

	press(a, key("6")) // traces
	press(a, key("5")) // logs
	tv, ok := a.top().(*tableView)
	if !ok || tv.spec.Name != "logs" {
		t.Fatalf("hotkey 5 → %v", a.top().Crumb())
	}
	if len(tv.searches) != 1 || tv.searches[0] != "timeout" {
		t.Fatalf("jump should restore last search, got %v", tv.searches)
	}
	if !strings.Contains(tv.dql, "timeout") {
		t.Errorf("restored search missing from dql:\n%s", tv.dql)
	}
}

func TestJumpExplicitArgSkipsLastFilter(t *testing.T) {
	a := testApp(t, "logs")
	seedRows(t, a, []map[string]any{{"content": "timeout"}})
	addServerSearch(a, "timeout")

	deliver(a, a.jumpTo("logs", "foo"))
	tv := a.top().(*tableView)
	if tv.spec.Name != "logs" || tv.filter != "foo" {
		t.Fatalf("jumpTo logs foo → crumb %v filter %q", a.top().Crumb(), tv.filter)
	}
	if len(tv.searches) != 0 {
		t.Fatalf("explicit arg must not apply last search, got %v", tv.searches)
	}

	deliver(a, a.jumpTo("logs", ""))
	tv = a.top().(*tableView)
	if len(tv.searches) != 1 || tv.searches[0] != "timeout" || tv.filter != "" {
		t.Fatalf("bare :logs should restore last search, searches=%v filter=%q", tv.searches, tv.filter)
	}
}

func TestClearFacetsForgetsLastFilter(t *testing.T) {
	a := testApp(t, "logs")
	seedRows(t, a, []map[string]any{{"content": "timeout"}})
	addServerSearch(a, "timeout")
	press(a, key("F"))
	press(a, key("6"))
	press(a, key("5"))
	tv := a.top().(*tableView)
	if len(tv.searches) != 0 || len(tv.facets) != 0 {
		t.Fatalf("cleared filter must not return on jump, searches=%v facets=%v", tv.searches, tv.facets)
	}
}

func TestLastFilterIsLatestOnlyAndPerView(t *testing.T) {
	a := testApp(t, "logs")
	seedRows(t, a, []map[string]any{{"content": "timeout"}})
	addServerSearch(a, "timeout")
	press(a, key("/"))
	typeText(a, "error")
	press(a, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})

	press(a, key("6")) // traces
	seedRows(t, a, []map[string]any{{"name": "checkout"}})
	addServerSearch(a, "checkout")

	press(a, key("5"))
	if tv := a.top().(*tableView); tv.spec.Name != "logs" || len(tv.searches) != 1 || tv.searches[0] != "error" {
		t.Fatalf("logs last filter = %v, want [error]", a.top().(*tableView).searches)
	}
	press(a, key("6"))
	if tv := a.top().(*tableView); tv.spec.Name != "traces" || len(tv.searches) != 1 || tv.searches[0] != "checkout" {
		t.Fatalf("traces last filter = %v, want [checkout]", a.top().(*tableView).searches)
	}
}

func TestLastFilterIsNotANamedPreset(t *testing.T) {
	a := testApp(t, "logs")
	seedRows(t, a, []map[string]any{{"content": "timeout"}})
	addServerSearch(a, "timeout")
	if len(a.presets) != 0 {
		t.Fatalf("last filter leaked into :preset: %v", a.presets)
	}
	a.presets = map[string]NamedPreset{"payments-errors": {Name: "payments-errors", View: "logs", Searches: []string{"preset-term"}, Personal: true}}
	press(a, key("6"))
	press(a, key("5"))
	tv := a.top().(*tableView)
	if len(tv.searches) != 1 || tv.searches[0] != "timeout" {
		t.Fatalf("named preset must not replace last filter, got %v", tv.searches)
	}
}

func TestLaunchNarrowingBecomesLastFilter(t *testing.T) {
	a, err := newApp(Options{
		ContextName: "test",
		SafetyLevel: "readonly",
		Launch: NavigateMsg{Target: "logs",
			Facets:   []NavFacet{{Field: "loglevel", Value: "ERROR"}},
			Searches: []string{"timeout"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	a.ds.runFn = func(string) ([]map[string]any, error) { return nil, nil }
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	press(a, key("6"))
	press(a, key("5"))
	tv := a.top().(*tableView)
	if len(tv.searches) != 1 || tv.searches[0] != "timeout" {
		t.Errorf("searches = %v, want [timeout]", tv.searches)
	}
	if len(tv.facets) != 1 || tv.facets[0].Field != "loglevel" || tv.facets[0].Value != "ERROR" {
		t.Errorf("facets = %+v", tv.facets)
	}
}

func TestContextSwitchDropsLastFilter(t *testing.T) {
	a := testApp(t, "logs")
	seedRows(t, a, []map[string]any{{"content": "timeout"}})
	addServerSearch(a, "timeout")
	a.opts.SwitchContext = func(name string) (*ContextWiring, error) {
		return &ContextWiring{ContextName: name, Environment: "https://" + name + ".example.invalid", SafetyLevel: "readonly"}, nil
	}
	deliver(a, a.switchContext("beta"))
	if len(a.lastFilter) != 0 {
		t.Fatalf("last filter survived :ctx: %v", a.lastFilter)
	}
	deliver(a, a.jumpTo("logs", ""))
	tv := a.top().(*tableView)
	if len(tv.searches) != 0 {
		t.Fatalf("logs after :ctx should start unfiltered, got %v", tv.searches)
	}
}

package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
	"github.com/dynatrace-oss/dtui/internal/workspace"
)

func TestSavePresetFromFacetManager(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.yaml")
	a, err := newApp(Options{
		ContextName: "test",
		SafetyLevel: "readonly",
		InitialView: "logs",
		SavePreset: func(p NamedPreset) error {
			facets := make([]workspace.PresetFacet, 0, len(p.Facets))
			for _, f := range p.Facets {
				facets = append(facets, workspace.PresetFacet{Field: f.Field, Value: f.Value})
			}
			return workspace.SavePreset(path, p.Name, workspace.Preset{
				View: p.View, Timeframe: p.Timeframe, Facets: facets, Searches: p.Searches,
			})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	a.ds.runFn = func(string) ([]map[string]any, error) { return nil, nil }
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	tv := a.top().(*tableView)
	tv.facets = []catalog.Facet{{Field: "loglevel", Value: "ERROR"}}
	tv.searches = []string{"timeout"}

	press(a, key("f"))
	if tv.facetMode != facetFieldStage {
		t.Fatalf("facet mode = %d, want manager", tv.facetMode)
	}
	press(a, key("W"))
	overlayAs[*presetSaveOverlay](t, a)
	typeText(a, "payments-errors")
	press(a, key("enter"))
	if a.overlay != nil {
		t.Fatalf("overlay still open: %T", a.overlay)
	}
	if _, ok := a.presets["payments-errors"]; !ok {
		t.Fatal("in-memory preset missing")
	}
	w, err := workspace.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := w.LookupPreset("payments-errors")
	if !ok || p.View != "logs" || len(p.Facets) != 1 || p.Facets[0].Value != "ERROR" {
		t.Errorf("file preset = %+v", p)
	}

	tv.facets = nil
	tv.searches = nil
	deliver(a, a.applyNamedPreset("payments-errors"))
	tv = a.top().(*tableView)
	if tv.spec.Name != "logs" || len(tv.facets) != 1 || tv.facets[0].Value != "ERROR" || len(tv.searches) != 1 {
		t.Errorf("applied view=%s facets=%+v searches=%v", tv.spec.Name, tv.facets, tv.searches)
	}
}

func TestSavePresetFromAnyFacetedTable(t *testing.T) {
	a := testApp(t, "pods")
	saved := make(chan NamedPreset, 1)
	a.opts.SavePreset = func(p NamedPreset) error {
		saved <- p
		return nil
	}
	tv := a.top().(*tableView)
	tv.facets = []catalog.Facet{{Field: "k8s.namespace.name", Value: "checkout"}}
	press(a, key("W"))
	overlayAs[*presetSaveOverlay](t, a)
	typeText(a, "checkout-pods")
	press(a, key("enter"))
	select {
	case p := <-saved:
		if p.Name != "checkout-pods" || p.View != "pods" || p.Facets[0].Value != "checkout" || p.Timeframe != "" {
			t.Errorf("saved %+v", p)
		}
	default:
		t.Fatal("SavePreset was not called")
	}
}

func TestPresetPickerShowsViewAndOpens(t *testing.T) {
	a := testApp(t, "home")
	a.presets = map[string]NamedPreset{
		"payments-errors": {
			Name:   "payments-errors",
			View:   "logs",
			Facets: []catalog.Facet{{Field: "loglevel", Value: "ERROR"}},
		},
		"checkout-pods": {
			Name:   "checkout-pods",
			View:   "pods",
			Facets: []catalog.Facet{{Field: "k8s.namespace.name", Value: "checkout"}},
		},
	}
	deliver(a, a.applyNamedPreset(""))
	p := overlayAs[*presetPicker](t, a)
	body := p.View(120, 40)
	if !strings.Contains(body, "payments-errors") || !strings.Contains(body, "logs") {
		t.Errorf("picker missing logs preset:\n%s", body)
	}
	if !strings.Contains(body, "checkout-pods") || !strings.Contains(body, "pods") {
		t.Errorf("picker missing pods preset:\n%s", body)
	}
	// Sorted names: checkout-pods, payments-errors — enter opens the first.
	press(a, key("enter"))
	if a.overlay != nil {
		t.Fatalf("overlay still open: %T", a.overlay)
	}
	tv := a.top().(*tableView)
	if tv.spec.Name != "pods" || len(tv.facets) != 1 || tv.facets[0].Value != "checkout" {
		t.Errorf("enter opened view=%s facets=%+v", tv.spec.Name, tv.facets)
	}
}

func TestSavePresetOmitsSessionTimeframe(t *testing.T) {
	a := testApp(t, "logs")
	saved := make(chan NamedPreset, 1)
	a.opts.SavePreset = func(p NamedPreset) error {
		saved <- p
		return nil
	}
	tv := a.top().(*tableView)
	tv.facets = []catalog.Facet{{Field: "loglevel", Value: "ERROR"}}
	tv.scope.Timeframe.Label = "problem"
	press(a, key("W"))
	overlayAs[*presetSaveOverlay](t, a)
	typeText(a, "payments-errors")
	press(a, key("enter"))
	select {
	case p := <-saved:
		if p.Timeframe != "" {
			t.Errorf("W must not pin timeframe, got %q", p.Timeframe)
		}
	default:
		t.Fatal("SavePreset was not called")
	}
}

func TestSavePresetRefusesEntityScope(t *testing.T) {
	a := testApp(t, "logs")
	a.opts.SavePreset = func(p NamedPreset) error {
		t.Errorf("SavePreset called with %+v", p)
		return nil
	}
	tv := a.top().(*tableView)
	tv.facets = []catalog.Facet{{Field: "loglevel", Value: "ERROR"}}
	tv.scope.Entity = &catalog.Entity{ID: "K8S_POD-0000000000000001", Name: "checkout-1", Type: "K8S_POD"}
	press(a, key("W"))
	if a.overlay != nil {
		t.Fatalf("save overlay opened despite entity scope: %T", a.overlay)
	}
}

func TestPresetPickerDeletesPersonal(t *testing.T) {
	a := testApp(t, "home")
	deleted := make(chan string, 1)
	a.opts.DeletePreset = func(name string) error {
		deleted <- name
		return nil
	}
	a.workspacePresets = map[string]NamedPreset{
		"payments-errors": {
			Name:   "payments-errors",
			View:   "logs",
			Facets: []catalog.Facet{{Field: "loglevel", Value: "ERROR"}},
		},
	}
	a.presets = map[string]NamedPreset{
		"payments-errors": {
			Name:     "payments-errors",
			View:     "logs",
			Facets:   []catalog.Facet{{Field: "loglevel", Value: "WARN"}},
			Personal: true,
		},
		"checkout-pods": {
			Name:     "checkout-pods",
			View:     "pods",
			Facets:   []catalog.Facet{{Field: "k8s.namespace.name", Value: "checkout"}},
			Personal: true,
		},
	}
	deliver(a, a.applyNamedPreset(""))
	overlayAs[*presetPicker](t, a)
	press(a, key("d")) // sorted: checkout-pods first
	select {
	case name := <-deleted:
		if name != "checkout-pods" {
			t.Errorf("deleted %q, want checkout-pods", name)
		}
	default:
		t.Fatal("DeletePreset was not called")
	}
	if _, ok := a.presets["checkout-pods"]; ok {
		t.Fatal("checkout-pods still in memory")
	}
	press(a, key("d")) // remaining personal overlay
	if got := a.presets["payments-errors"]; got.Personal || got.Facets[0].Value != "ERROR" {
		t.Errorf("expected workspace restore, got %+v", got)
	}
}

func TestPresetPickerDoesNotDeleteWorkspace(t *testing.T) {
	a := testApp(t, "home")
	a.opts.DeletePreset = func(name string) error {
		t.Errorf("DeletePreset(%q) must not run for workspace presets", name)
		return nil
	}
	a.presets = map[string]NamedPreset{
		"payments-errors": {
			Name:   "payments-errors",
			View:   "logs",
			Facets: []catalog.Facet{{Field: "loglevel", Value: "ERROR"}},
		},
	}
	deliver(a, a.applyNamedPreset(""))
	overlayAs[*presetPicker](t, a)
	press(a, key("d"))
	if _, ok := a.presets["payments-errors"]; !ok {
		t.Fatal("workspace preset was removed")
	}
	if _, ok := a.overlay.(*presetPicker); !ok {
		t.Fatalf("picker closed: %T", a.overlay)
	}
}

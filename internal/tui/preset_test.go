package tui

import (
	"path/filepath"
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

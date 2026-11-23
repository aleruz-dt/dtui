package tui

import (
	"slices"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
)

// viewFilter is the latest server-side narrowing for one catalog view this
// session (the same payload W saves, without a name or timeframe). Jumps
// restore it; named presets stay never auto-applied.
type viewFilter struct {
	facets   []catalog.Facet
	searches []string
}

func (lf viewFilter) label() string {
	return NamedPreset{Facets: lf.facets, Searches: lf.searches}.narrowing()
}

// rememberLastFilter snapshots the top table via presetDraft, so last-filter
// and W share the API-backed / entity-scoped refusals (ADR-0007, ADR-0017).
// Empty narrowing is recorded only when allowEmpty is set (the user cleared
// with F / esc) so a client-side `:logs foo` jump does not forget the last
// server filter.
func (a *app) rememberLastFilter(allowEmpty bool) {
	tv, ok := a.top().(*tableView)
	if !ok {
		return
	}
	draft, err := tv.presetDraft()
	if err == nil {
		if a.lastFilter == nil {
			a.lastFilter = map[string]viewFilter{}
		}
		a.lastFilter[draft.view] = viewFilter{
			facets:   slices.Clone(draft.facets),
			searches: slices.Clone(draft.searches),
		}
		return
	}
	if !allowEmpty || tv.spec.API != "" || tv.scope.Entity != nil {
		return
	}
	if a.lastFilter != nil {
		delete(a.lastFilter, tv.spec.Name)
	}
}

// applyLastFilter copies this session's last server narrowing onto a freshly
// built catalog table. jumpTo skips this when the command bar supplied an
// argument — that client-side filter wins. The returned status is empty when
// nothing applied, so the caller can say what the jump restored (ADR-0007).
func (a *app) applyLastFilter(tv *tableView) string {
	if tv.spec.API != "" || a.lastFilter == nil {
		return ""
	}
	lf, ok := a.lastFilter[tv.spec.Name]
	if !ok {
		return ""
	}
	tv.facets = slices.Clone(lf.facets)
	tv.searches = slices.Clone(lf.searches)
	return "restored last filter " + lf.label() + " — F clears"
}

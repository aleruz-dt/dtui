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

// rememberLastFilter snapshots the top table's facets and searches. Empty
// narrowing is recorded only when allowEmpty is set (the user cleared with
// F / esc) so a client-side `:logs foo` jump does not forget the last
// server filter. API-backed views have no DQL facets; skip them.
func (a *app) rememberLastFilter(allowEmpty bool) {
	tv, ok := a.top().(*tableView)
	if !ok || tv.spec.API != "" {
		return
	}
	if len(tv.facets)+len(tv.searches) == 0 {
		if allowEmpty && a.lastFilter != nil {
			delete(a.lastFilter, tv.spec.Name)
		}
		return
	}
	if a.lastFilter == nil {
		a.lastFilter = map[string]viewFilter{}
	}
	a.lastFilter[tv.spec.Name] = viewFilter{
		facets:   slices.Clone(tv.facets),
		searches: slices.Clone(tv.searches),
	}
}

// applyLastFilter copies this session's last server narrowing onto a freshly
// built catalog table. jumpTo skips this when the command bar supplied an
// argument — that client-side filter wins.
func (a *app) applyLastFilter(tv *tableView) {
	if tv.spec.API != "" || a.lastFilter == nil {
		return
	}
	lf, ok := a.lastFilter[tv.spec.Name]
	if !ok {
		return
	}
	tv.facets = slices.Clone(lf.facets)
	tv.searches = slices.Clone(lf.searches)
}

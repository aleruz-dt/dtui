package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func remoteNav(t *testing.T, a *app, nav NavigateMsg) remoteReply {
	t.Helper()
	reply := make(chan remoteReply, 1)
	_, cmd := a.Update(remoteNavigateMsg{nav: nav, reply: reply})
	deliver(a, cmd)
	select {
	case r := <-reply:
		return r
	default:
		t.Fatal("remote navigation did not reply")
		return remoteReply{}
	}
}

func TestRemoteNavigatePushesSoEscReturns(t *testing.T) {
	a := testApp(t, "pods")
	r := remoteNav(t, a, NavigateMsg{Target: "logs", Facets: []NavFacet{{Field: "loglevel", Value: "ERROR"}}, Searches: []string{"timeout"}})
	if r.err != nil || r.page == "" {
		t.Fatalf("reply = %+v, want a page and no error", r)
	}
	if len(a.stack) != 2 {
		t.Fatalf("stack depth = %d, want 2 (pushed on top of pods)", len(a.stack))
	}
	tv, ok := a.top().(*tableView)
	if !ok || tv.spec.Name != "logs" || len(tv.facets) != 1 || len(tv.searches) != 1 {
		t.Fatalf("top = %T %+v, want narrowed logs table", a.top(), tv)
	}
	if !strings.Contains(a.status, "agent opened") {
		t.Errorf("status = %q, want it to say an agent opened the page", a.status)
	}
}

func TestRemoteNavigateNeverSwitchesContext(t *testing.T) {
	a := testApp(t, "pods")
	r := remoteNav(t, a, NavigateMsg{Target: "logs", Context: "other"})
	if r.err == nil || !strings.Contains(r.err.Error(), "will not switch") {
		t.Fatalf("err = %v, want a refusal to switch context", r.err)
	}
	if len(a.stack) != 1 || a.pendingNav != nil {
		t.Fatalf("stack = %d, pendingNav = %v; want the session untouched", len(a.stack), a.pendingNav)
	}
}

func TestRemoteNavigateReportsErrorsAndWarnings(t *testing.T) {
	a := testApp(t, "pods")
	if r := remoteNav(t, a, NavigateMsg{Target: "nope"}); r.err == nil || r.page != "" {
		t.Fatalf("unknown view reply = %+v, want an error and no page", r)
	}
	r := remoteNav(t, a, NavigateMsg{Target: "logs", Facets: []NavFacet{{Field: "a`|b", Value: "x"}}})
	if r.page == "" || r.err == nil || !strings.Contains(r.err.Error(), "unsafe field") {
		t.Fatalf("unsafe facet reply = %+v, want the page opened with a warning", r)
	}
	if tv := a.top().(*tableView); len(tv.facets) != 0 {
		t.Errorf("facets = %+v, want the unsafe one dropped", tv.facets)
	}
}

func TestNavigateMsgJSONRoundTrip(t *testing.T) {
	from := time.Date(2026, 1, 2, 14, 0, 0, 0, time.UTC)
	in := NavigateMsg{Target: "logs", Facets: []NavFacet{{Field: "loglevel", Value: "ERROR", Tokens: true}},
		Searches: []string{"timeout"}, Lens: "genai", From: from, DQL: "fetch logs"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"to"`) || strings.Contains(string(b), `"context"`) {
		t.Errorf("zero fields should be omitted: %s", b)
	}
	var out NavigateMsg
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Target != in.Target || !out.From.Equal(from) || out.Facets[0] != in.Facets[0] || out.DQL != in.DQL || out.Lens != in.Lens {
		t.Errorf("round trip = %+v, want %+v", out, in)
	}
}

package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// RemoteNavigate asks the running TUI to open a page and waits for the
// outcome: the breadcrumb of the page it opened, or why it could not.
// Options.Listen receives one; the main package serves it on a local socket
// so an agent can drive the TUI the user is watching (ADR-0019).
type RemoteNavigate func(NavigateMsg) (page string, err error)

// remoteReplyTimeout bounds how long a remote caller waits for the app to
// take the message — a TUI blocked behind a modal must not hang the agent.
const remoteReplyTimeout = 10 * time.Second

// remoteNavigateMsg is a NavigateMsg from outside the process. Unlike a deep
// link it pushes rather than replaces, so esc returns the user to where they
// were, and it reports back on reply (buffered, never blocks Update).
type remoteNavigateMsg struct {
	nav   NavigateMsg
	reply chan remoteReply
}

type remoteReply struct {
	page string
	err  error
}

func remoteNavigator(p *tea.Program) RemoteNavigate {
	return func(nav NavigateMsg) (string, error) {
		reply := make(chan remoteReply, 1)
		go p.Send(remoteNavigateMsg{nav: nav, reply: reply})
		select {
		case r := <-reply:
			return r.page, r.err
		case <-time.After(remoteReplyTimeout):
			return "", errors.New("dtui did not answer in time")
		}
	}
}

// navigateRemote opens an agent-requested page on top of the stack. It never
// switches context: the user chose the tenant this TUI shows, and an agent
// silently repointing it would attribute data to the wrong environment.
func (a *app) navigateRemote(msg remoteNavigateMsg) tea.Cmd {
	respond := func(page string, err error) {
		if msg.reply != nil {
			msg.reply <- remoteReply{page: page, err: err}
		}
	}
	if c := strings.TrimSpace(msg.nav.Context); c != "" && c != a.opts.ContextName {
		err := fmt.Errorf("this dtui is on context %q; it will not switch to %q", a.opts.ContextName, c)
		respond("", err)
		return statusErr("agent: " + err.Error())
	}
	prevTF := a.tf
	view, warns, err := a.launchView(msg.nav)
	if err != nil {
		respond("", err)
		return statusErr("agent: " + err.Error())
	}
	cmd := a.navigate(view, false)
	if a.tf != prevTF {
		a.prev = nil
	}
	page := view.Crumb()
	if len(warns) > 0 {
		w := strings.Join(warns, "; ")
		respond(page, fmt.Errorf("opened with warnings: %s", w))
		return tea.Batch(cmd, statusErr("agent opened "+page+" — "+w))
	}
	respond(page, nil)
	return tea.Batch(cmd, status("agent opened "+page+" (esc returns)"))
}

package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/rivo/tview"

	"github.com/Cesarsk/ike/internal/data"
)

// showTeamMembers opens the members panel for a team row (enter on :teams):
// the team's people and their roles, from one bounded fetch. Read-only.
// o opens the team's page, ctrl-r re-fetches.
func (a *App) showTeamMembers(row data.Row) {
	a.pushNav()
	a.teamRow = row
	cur := a.current
	a.teamMembers.SetTitle(" Team ")
	a.teamMembers.SetText(a.theme.recolor("\n  [gray]fetching members…")).ScrollToBeginning()
	a.showPage("teammembers")
	prov := a.providerFor(row) // route to the row's origin org
	go func() {
		members, err := prov.TeamMembers(context.Background(), row.ID)
		a.QueueUpdateDraw(func() {
			if a.page != "teammembers" || a.current != cur || a.teamRow.ID != row.ID {
				return // navigated away
			}
			team := row.Cells[0]
			if err != nil {
				a.teamMembers.SetText(a.theme.recolor("\n  [red]✗ " + tview.Escape(err.Error())))
				return
			}
			a.teamMembers.SetText(a.theme.recolor(renderTeamMembers(team, members)))
			a.teamMembers.SetTitle(fmt.Sprintf(" Team · %s ", team))
		})
	}()
}

// openTeamURL opens the team's page in the Datadog web UI.
func (a *App) openTeamURL() {
	if a.teamRow.URL != "" {
		a.openURL(a.teamRow.URL)
	}
}

// renderTeamMembers draws a team's roster: name, handle/email, and role.
func renderTeamMembers(team string, members []data.TeamMember) string {
	var b strings.Builder
	count := ""
	if len(members) > 0 {
		count = fmt.Sprintf(" · %d", len(members))
	}
	fmt.Fprintf(&b, " [orange::b]%s[-:-:-] [gray]members%s · read-only[-]\n\n", tview.Escape(team), count)

	if len(members) == 0 {
		b.WriteString("  [gray]no members, or membership isn't readable with this login[-]\n")
		b.WriteString("\n [gray]<o> open in Datadog · <esc> back[-]\n")
		return b.String()
	}

	nameW := len("NAME")
	for _, m := range members {
		if n := len(m.Name); n > nameW {
			nameW = n
		}
	}
	fmt.Fprintf(&b, "  [gray]%-*s  %-22s  %s[-]\n", nameW, "NAME", "CONTACT", "ROLE")
	for _, m := range members {
		name := m.Name
		if name == "" {
			name = m.Handle
		}
		contact := teamMemberContact(m)
		fmt.Fprintf(&b, "  [white]%-*s[-]  [gray]%-22s[-]  %s\n",
			nameW, tview.Escape(name), tview.Escape(contact), teamRoleTag(m.Role))
	}
	b.WriteString("\n [gray]<o> open in Datadog · <ctrl-r> refresh · <esc> back[-]\n")
	return b.String()
}

// teamMemberContact prefers the handle, then the email.
func teamMemberContact(m data.TeamMember) string {
	switch {
	case m.Handle != "":
		return "@" + m.Handle
	case m.Email != "":
		return m.Email
	}
	return ""
}

// teamRoleTag colors the admin role so it stands out from plain members.
func teamRoleTag(role string) string {
	if role == "" {
		return ""
	}
	if strings.EqualFold(role, "admin") {
		return "[orange]" + tview.Escape(role) + "[-]"
	}
	return "[aqua]" + tview.Escape(role) + "[-]"
}

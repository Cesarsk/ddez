package ui

import (
	"sort"
	"strings"

	"github.com/rivo/tview"
)

// openFuzzy opens the fuzzy row finder over the current table: type to match
// (subsequence, case-insensitive, across every cell), enter jumps the table
// selection to the picked row, esc cancels. Client-side only — no API cost.
func (a *App) openFuzzy() {
	a.fuzzyInput.SetText("")
	a.fuzzyFlex.SetTitle(" Fuzzy find · " + a.res.Title + " ")
	a.showPage("fuzzy")
	a.renderFuzzy()
}

// fuzzyMatch reports whether query is a subsequence of s (case-insensitive)
// and how tight the match is (smaller = better: total gap between matched
// runes, with a bonus for matching at the start).
func fuzzyMatch(query, s string) (int, bool) {
	if query == "" {
		return 1 << 20, true
	}
	q, t := strings.ToLower(query), strings.ToLower(s)
	score, last := 0, -1
	for _, qr := range q {
		i := strings.IndexRune(t[last+1:], qr)
		if i < 0 {
			return 0, false
		}
		pos := last + 1 + i
		if last >= 0 {
			score += pos - last - 1 // gap penalty
		} else {
			score += pos // distance from the start
		}
		last = pos
	}
	return score, true
}

// renderFuzzy ranks the current view's rows against the query and shows the
// best matches (capped — the point is jumping, not paging).
func (a *App) renderFuzzy() {
	const maxHits = 15
	query := a.fuzzyInput.GetText()
	type hit struct {
		row   int
		score int
	}
	var hits []hit
	for i, r := range a.rows {
		line := strings.Join(r.Cells, " ")
		if r.Ctx != "" {
			line = r.Ctx + " " + line
		}
		if score, ok := fuzzyMatch(query, line); ok {
			hits = append(hits, hit{i, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score < hits[j].score })
	if len(hits) > maxHits {
		hits = hits[:maxHits]
	}
	a.fuzzyList.Clear()
	a.fuzzyRows = a.fuzzyRows[:0]
	for _, h := range hits {
		r := a.rows[h.row]
		label := strings.TrimSpace(strings.Join(r.Cells, "  "))
		if r.Ctx != "" {
			label = r.Ctx + " · " + label
		}
		if len(label) > 120 {
			label = label[:117] + "…"
		}
		a.fuzzyList.AddItem(tview.Escape(label), "", 0, nil)
		a.fuzzyRows = append(a.fuzzyRows, h.row)
	}
	if len(hits) == 0 {
		a.fuzzyList.AddItem(tview.Escape("(no matches)"), "", 0, nil)
	}
	a.fuzzyList.SetCurrentItem(0)
}

// fuzzyMove shifts the highlighted match (arrows; the input keeps focus).
func (a *App) fuzzyMove(delta int) {
	n := a.fuzzyList.GetItemCount()
	if n == 0 {
		return
	}
	i := a.fuzzyList.GetCurrentItem() + delta
	if i < 0 {
		i = 0
	}
	if i >= n {
		i = n - 1
	}
	a.fuzzyList.SetCurrentItem(i)
}

// closeFuzzy returns to the table; when picked, it clears any active filter
// (the target row must be visible) and selects the chosen row.
func (a *App) closeFuzzy(pick bool) {
	if !pick {
		a.showPage("table")
		return
	}
	i := a.fuzzyList.GetCurrentItem()
	if i < 0 || i >= len(a.fuzzyRows) {
		a.showPage("table")
		return
	}
	target := a.fuzzyRows[i]
	a.filter = ""
	a.colFilterCol, a.colFilterVal = -1, ""
	a.showPage("table")
	a.applyFilter()
	for pos, idx := range a.filtered {
		if idx == target {
			a.table.Select(pos+1, 0) // +1: header row
			break
		}
	}
}

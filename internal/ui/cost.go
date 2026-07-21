package ui

import (
	"context"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/rivo/tview"

	"github.com/Cesarsk/ike/internal/data"
)

// A month-over-month move is flagged as an anomaly only when it is both large
// in relative terms and large in absolute terms — a 3x jump on a $5 line is
// noise, not an anomaly.
const (
	costAnomalyPct = 30.0
	costAnomalyAbs = 100.0
)

// costRender bundles the app-side state the pure cost renderers need.
type costRender struct {
	view     *data.CostView
	sel      int    // selected month index
	filter   string // client-side substring filter over org/product
	orgFocus string // sub-org focus ("" = all orgs)
	subOrgs  bool
}

// costLineDelta pairs a breakdown line with its change vs the same line one
// month earlier. For the current month the projection is compared (a partial
// month against a full one would always look like a drop).
type costLineDelta struct {
	line      data.CostLine
	pct       float64
	hasPrev   bool
	isNew     bool // product had no line the month before
	anomalous bool
}

// showCost opens the :cost panel for the current context's org and fetches
// the selected range (a.costMonths, a.costSubOrg). Read-only, at most three
// bounded API calls. ctrl-r re-fetches; 1/3/6/y change the range; s toggles
// the sub-org breakdown; enter cycles the sub-org focus; [ and ] pick the
// month; / filters lines client-side; o opens the billing page.
func (a *App) showCost() {
	if a.page != "cost" {
		a.pushNav()
	}
	if a.costMonths < 1 {
		a.costMonths = 1
	}
	cur := a.current
	a.costView = nil
	a.costSel = 0
	a.costOrgFocus = ""
	a.cost.SetTitle(" Cost ")
	a.cost.SetText(a.theme.recolor("\n  [gray]fetching cost…")).ScrollToBeginning()
	a.showPage("cost")
	prov := a.provider // the current org's provider
	opts := data.CostOptions{Months: a.costMonths, SubOrgs: a.costSubOrg}
	go func() {
		v, err := prov.Cost(context.Background(), opts)
		a.QueueUpdateDraw(func() {
			if a.page != "cost" || a.current != cur {
				return // navigated away
			}
			if err != nil {
				a.cost.SetText(a.theme.recolor(renderCostError(cur, err)))
				a.cost.SetTitle(" Cost ")
				return
			}
			a.costView = v
			a.costSel = 0
			a.renderCostPage()
		})
	}()
}

// setCostRange switches the month range and re-fetches.
func (a *App) setCostRange(months int) {
	if a.costMonths == months && a.costView != nil {
		return
	}
	a.costMonths = months
	a.showCost()
}

// moveCostMonth shifts the selected month by delta (]=older, [=newer) and
// re-renders locally — the data is already loaded.
func (a *App) moveCostMonth(delta int) {
	if a.costView == nil {
		return
	}
	sel := a.costSel + delta
	if sel < 0 || sel >= len(a.costView.Months) {
		return
	}
	a.costSel = sel
	a.renderCostPage()
}

// cycleCostOrg steps the sub-org focus through all → each org → all, so one
// child org's breakdown can be looked at in isolation.
func (a *App) cycleCostOrg() {
	if a.costView == nil || !a.costSubOrg || len(a.costView.Months) == 0 {
		return
	}
	orgs := costOrgs(a.costView.Months[a.costSel])
	if len(orgs) == 0 {
		return
	}
	next := ""
	if a.costOrgFocus == "" {
		next = orgs[0]
	} else {
		for i, o := range orgs {
			if o == a.costOrgFocus && i+1 < len(orgs) {
				next = orgs[i+1]
				break
			}
		}
	}
	a.costOrgFocus = next
	a.renderCostPage()
}

// openCostURL opens the org's billing/usage page in the Datadog web UI.
func (a *App) openCostURL() {
	if a.costView == nil {
		return
	}
	a.openURL(a.costView.URL)
}

// renderCostPage redraws the panel from the loaded view plus the local
// selection/filter state.
func (a *App) renderCostPage() {
	v := a.costView
	if v == nil {
		return
	}
	if a.costSel >= len(v.Months) {
		a.costSel = 0
	}
	r := costRender{view: v, sel: a.costSel, filter: a.costFilter, orgFocus: a.costOrgFocus, subOrgs: a.costSubOrg}
	a.cost.SetText(a.theme.recolor(renderCost(r))).ScrollToBeginning()
	sel := "—"
	if len(v.Months) > 0 {
		sel = v.Months[a.costSel].Month
	}
	a.cost.SetTitle(fmt.Sprintf(" Cost · %s · %s ", v.OrgName, sel))
}

// renderCostError explains a failed cost fetch. The usage/billing endpoints
// are admin-scoped, so the common case is a permission denial — say so plainly
// instead of dumping a raw 403.
func renderCostError(ctxName string, err error) string {
	msg := err.Error()
	low := strings.ToLower(msg)
	if strings.Contains(low, "403") || strings.Contains(low, "forbidden") ||
		strings.Contains(low, "not authoriz") || strings.Contains(low, "permission") {
		return fmt.Sprintf("\n  [orange]Datadog cost is admin-scoped[-]\n\n"+
			"  The usage/billing API needs the [aqua]usage_read[-] permission, which is\n"+
			"  usually limited to org admins. Context %q can't read it.\n\n"+
			"  [gray]%s[-]", ctxName, tview.Escape(msg))
	}
	return "\n  [red]✗ " + tview.Escape(msg)
}

// renderCost draws the spend panel: header totals for the selected month, an
// anomaly summary, a month-trend section when history is loaded, and the
// selected month's per-product (or per-org) breakdown with proportional bars
// and month-over-month deltas.
func renderCost(r costRender) string {
	v := r.view
	var b strings.Builder
	scope := "summary"
	if r.subOrgs {
		scope = "sub-orgs"
	}
	fmt.Fprintf(&b, " [orange::b]Datadog spend[-:-:-] [gray]%s · %s · read-only, updates daily[-]\n\n",
		tview.Escape(v.OrgName), scope)
	if len(v.Months) == 0 {
		b.WriteString("  [gray]no billing data for this range[-]\n")
		return b.String()
	}
	m := v.Months[r.sel]
	var prev *data.CostMonth
	if r.sel+1 < len(v.Months) {
		prev = &v.Months[r.sel+1]
	}
	deltas := costDeltas(m, prev, r.filter, r.orgFocus)

	label := "month total"
	if m.Current {
		label = "estimated so far"
	}
	fmt.Fprintf(&b, "  [aqua]%-16s[-]  %s   [gray](%s)[-]\n", label, money(v.Currency, m.Total), m.Month)
	if m.Current && m.Projected > 0 {
		fmt.Fprintf(&b, "  [aqua]%-16s[-]  %s\n", "projected month", money(v.Currency, m.Projected))
	}
	if n := countAnomalies(deltas); n > 0 {
		plural := ""
		if n != 1 {
			plural = "s"
		}
		fmt.Fprintf(&b, "  [orange]⚠ %d unusual move%s vs previous month[-]\n", n, plural)
	}
	if r.subOrgs {
		renderCostScope(&b, v.Currency, m, r.orgFocus)
	}
	b.WriteString("\n")

	if len(v.Months) > 1 {
		renderCostTrend(&b, v, r.sel)
	}
	renderCostLines(&b, v.Currency, m, deltas, r)

	b.WriteString("\n [gray]<1/3/6/y> range · <[/]> month · </> filter · <s> sub-orgs · <enter> focus org[-]\n")
	b.WriteString(" [gray]<o> open in Datadog · <ctrl-r> refresh · <esc> back[-]\n")
	return b.String()
}

// renderCostScope draws the sub-org focus line, and — when the response has
// no child orgs to show — explains that the breakdown needs the root org.
func renderCostScope(b *strings.Builder, currency string, m data.CostMonth, orgFocus string) {
	if orgFocus == "" {
		b.WriteString("  [gray]sub-orgs:[-] [aqua]all[-] [gray](enter focuses one)[-]\n")
	} else {
		var sub float64
		for _, l := range m.Lines {
			if l.Org == orgFocus {
				sub += l.Total
			}
		}
		fmt.Fprintf(b, "  [gray]sub-orgs:[-] [aqua]%s[-] · %s [gray](enter cycles)[-]\n",
			tview.Escape(orgFocus), money(currency, sub))
	}
	if len(costOrgs(m)) <= 1 {
		b.WriteString("\n  [orange]only one org visible[-] — either this org has no sub-orgs, or this\n" +
			"  context is signed into a sub-org. The sub-org breakdown is served from\n" +
			"  the root organization: add a context for it in [aqua]:ctx[-] and switch there.\n")
	}
}

// renderCostTrend draws one row per loaded month — total, bar, and the change
// vs the month before it — marking the selected month and flagging months
// whose total moved anomalously.
func renderCostTrend(b *strings.Builder, v *data.CostView, sel int) {
	maxTotal := 0.0
	for _, m := range v.Months {
		if m.Total > maxTotal {
			maxTotal = m.Total
		}
	}
	fmt.Fprintf(b, "  [gray]%-9s  %12s[-]\n", "MONTH", "TOTAL")
	for i, m := range v.Months {
		mark, color := " ", "[white]"
		if i == sel {
			mark, color = "▶", "[aqua]"
		}
		suffix := ""
		switch {
		case m.Current:
			suffix = " [gray](in progress)[-]"
		case i+1 < len(v.Months) && v.Months[i+1].Total > 0:
			prev := v.Months[i+1].Total
			pct := (m.Total - prev) / prev * 100
			c := "[green]"
			if pct > 0 {
				c = "[red]" // cost going up is the bad direction
			}
			suffix = fmt.Sprintf(" %s%+.0f%%[-]", c, pct)
			if math.Abs(pct) >= costAnomalyPct && math.Abs(m.Total-prev) >= costAnomalyAbs {
				suffix += "[orange]⚠[-]"
			}
		}
		fmt.Fprintf(b, " %s%s%-9s[-]  %12s  %s%s\n",
			mark, color, m.Month, money(v.Currency, m.Total), costBar(m.Total, maxTotal), suffix)
	}
	b.WriteString("\n")
}

// renderCostLines draws the selected month's breakdown table with the
// month-over-month delta column (when a previous month is loaded).
func renderCostLines(b *strings.Builder, currency string, m data.CostMonth, deltas []costLineDelta, r costRender) {
	if r.filter != "" {
		fmt.Fprintf(b, "  [gray]filter:[-] [aqua]%s[-]  [gray](%d match", tview.Escape(r.filter), len(deltas))
		if len(deltas) != 1 {
			b.WriteString("es")
		}
		b.WriteString(")[-]\n\n")
	}
	if len(deltas) == 0 {
		switch {
		case r.filter != "" || r.orgFocus != "":
			b.WriteString("  [gray]nothing to show — a filter or org focus is active; <esc> clears the filter[-]\n")
		default:
			b.WriteString("  [gray]no billable usage this month[-]\n")
		}
		return
	}

	hasDelta := false
	maxCost, prodW, orgW := 0.0, len("PRODUCT"), len("ORG")
	for _, d := range deltas {
		if d.hasPrev || d.isNew {
			hasDelta = true
		}
		if d.line.Total > maxCost {
			maxCost = d.line.Total
		}
		if n := len(d.line.Product); n > prodW {
			prodW = n
		}
		if n := len(d.line.Org); n > orgW {
			orgW = n
		}
	}
	amount := "TOTAL"
	if m.Current {
		amount = "ESTIMATED"
	}
	const deltaW = 7
	b.WriteString("  [gray]")
	if r.subOrgs {
		fmt.Fprintf(b, "%-*s  ", orgW, "ORG")
	}
	fmt.Fprintf(b, "%-*s  %12s  %12s", prodW, "PRODUCT", amount, "PROJECTED")
	if hasDelta {
		fmt.Fprintf(b, "  %s", padLeftCells("Δ PREV", deltaW))
	}
	b.WriteString("[-]\n")
	for _, d := range deltas {
		l := d.line
		proj := ""
		if l.Projected > 0 {
			proj = money(currency, l.Projected)
		}
		b.WriteString("  ")
		if r.subOrgs {
			fmt.Fprintf(b, "%-*s  ", orgW, tview.Escape(l.Org))
		}
		fmt.Fprintf(b, "%-*s  %12s  %12s", prodW, tview.Escape(l.Product), money(currency, l.Total), proj)
		if hasDelta {
			fmt.Fprintf(b, "  %s", costDeltaCell(d, deltaW))
		}
		fmt.Fprintf(b, "  %s\n", costBar(l.Total, maxCost))
	}
}

// costDeltas computes each visible line's change vs the previous month,
// applying the org focus and client-side filter. A move is anomalous when it
// clears both the relative and absolute thresholds; a product with no line
// the month before is "new" (anomalous if it is already material).
func costDeltas(m data.CostMonth, prev *data.CostMonth, filter, orgFocus string) []costLineDelta {
	prevTotals := map[string]float64{}
	if prev != nil {
		for _, l := range prev.Lines {
			prevTotals[l.Org+"\x00"+l.Product] = l.Total
		}
	}
	f := strings.ToLower(filter)
	out := make([]costLineDelta, 0, len(m.Lines))
	for _, l := range m.Lines {
		if orgFocus != "" && l.Org != orgFocus {
			continue
		}
		if f != "" && !strings.Contains(strings.ToLower(l.Product), f) &&
			!strings.Contains(strings.ToLower(l.Org), f) {
			continue
		}
		d := costLineDelta{line: l}
		cur := l.Total
		if m.Current && l.Projected > 0 {
			cur = l.Projected // compare full-month projection, not a partial accrual
		}
		if prev != nil {
			pt, ok := prevTotals[l.Org+"\x00"+l.Product]
			switch {
			case ok && pt > 0:
				d.hasPrev = true
				d.pct = (cur - pt) / pt * 100
				d.anomalous = math.Abs(d.pct) >= costAnomalyPct && math.Abs(cur-pt) >= costAnomalyAbs
			case !ok:
				d.isNew = true
				d.anomalous = cur >= costAnomalyAbs
			}
		}
		out = append(out, d)
	}
	return out
}

// countAnomalies counts the flagged lines in a delta set.
func countAnomalies(deltas []costLineDelta) int {
	n := 0
	for _, d := range deltas {
		if d.anomalous {
			n++
		}
	}
	return n
}

// costDeltaCell renders one line's delta, right-aligned to w screen cells.
func costDeltaCell(d costLineDelta, w int) string {
	plain, color := "", "[gray]"
	switch {
	case d.isNew:
		plain, color = "new", "[orange]"
	case d.hasPrev:
		plain = fmt.Sprintf("%+.0f%%", d.pct)
		color = "[green]"
		if d.pct > 0 {
			color = "[red]" // cost going up is the bad direction
		}
	}
	if d.anomalous {
		plain += "⚠"
	}
	return color + padLeftCells(plain, w) + "[-]"
}

// costOrgs lists the distinct org names in a month's lines, in line order
// (i.e. highest-cost first). Empty in summary view.
func costOrgs(m data.CostMonth) []string {
	seen := map[string]bool{}
	var orgs []string
	for _, l := range m.Lines {
		if l.Org == "" || seen[l.Org] {
			continue
		}
		seen[l.Org] = true
		orgs = append(orgs, l.Org)
	}
	return orgs
}

// padLeftCells right-aligns s to w screen cells, counting runes — fmt's %*s
// counts bytes, which misaligns multi-byte runes like ⚠.
func padLeftCells(s string, w int) string {
	if n := w - utf8.RuneCountInString(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

// costBar is a proportional bar for one line's cost, up to 24 cells.
func costBar(v, max float64) string {
	if max <= 0 {
		return ""
	}
	n := int(v / max * 24)
	if n < 1 && v > 0 {
		n = 1
	}
	return "[aqua]" + strings.Repeat("█", n) + "[-]"
}

// money formats an amount with a thousands-separated whole part and a currency
// symbol ($ for USD, else the code).
func money(currency string, v float64) string {
	sym := "$"
	if currency != "" && currency != "USD" {
		sym = currency + " "
	}
	whole := fmt.Sprintf("%.0f", v)
	neg := strings.HasPrefix(whole, "-")
	whole = strings.TrimPrefix(whole, "-")
	var out []byte
	for i, c := range []byte(whole) {
		if i > 0 && (len(whole)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	s := sym + string(out)
	if neg {
		s = "-" + s
	}
	return s
}

package data

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV1"
)

// sloStatus fetches an SLO's recent history and computes its live attainment
// and error budget — the numbers the list can't show. One API call per open
// (bounded), so it's a detail action, never a per-row list fetch.
func (l *Live) sloStatus(ctx context.Context, id string) (any, error) {
	api := datadogV1.NewServiceLevelObjectivesApi(l.client)
	slo, resp, err := api.GetSLO(ctx, id, *datadogV1.NewGetSLOOptionalParameters())
	l.track(resp)
	if err != nil {
		return nil, apiErr("slo detail", err)
	}
	data := slo.GetData()
	// Window: the first threshold's timeframe (7d/30d/90d), default 30d.
	days := 30
	var target float64
	if th := data.GetThresholds(); len(th) > 0 {
		target = th[0].GetTarget()
		switch th[0].GetTimeframe() {
		case datadogV1.SLOTIMEFRAME_SEVEN_DAYS:
			days = 7
		case datadogV1.SLOTIMEFRAME_NINETY_DAYS:
			days = 90
		}
	}
	to := time.Now().Unix()
	from := to - int64(days*86400)
	hist, hresp, err := api.GetSLOHistory(ctx, id, from, to,
		*datadogV1.NewGetSLOHistoryOptionalParameters())
	l.track(hresp)
	out := &SLODetail{
		Name: data.GetName(), Type: string(data.GetType()),
		TargetPct: target, TimeframeDays: days,
	}
	if err != nil {
		// Config still worth showing even if history is unavailable.
		out.Note = "history unavailable: " + err.Error()
		return out, nil
	}
	hd := hist.GetData()
	overall := hd.GetOverall()
	attained := overall.GetSliValue()
	out.AttainmentPct = attained
	out.Burndown = sloBurndown(hd, target)
	if len(out.Burndown) == 0 {
		out.Note = "no history series for this SLO type — burndown unavailable"
	}
	if target > 0 && target < 100 {
		out.BurnRate = (100 - attained) / (100 - target)
	}
	if target > 0 {
		// Error budget consumed = (target-attained)/(100-target); >100% = breached.
		if attained >= target {
			out.BudgetRemainingPct = 100.0
		} else {
			consumed := (target - attained) / (100 - target) * 100
			if consumed > 100 {
				consumed = 100
			}
			out.BudgetRemainingPct = 100 - consumed
		}
	}
	return out, nil
}

// sloBurndown derives the error-budget-remaining series (oldest → newest) from
// an SLO's history: the overall SLI history where present (monitor/time-slice
// SLOs), else the cumulative numerator/denominator ratio (metric SLOs).
func sloBurndown(hd datadogV1.SLOHistoryResponseData, target float64) []float64 {
	if target <= 0 || target >= 100 {
		return nil
	}
	remaining := func(sli float64) float64 {
		if sli >= target {
			return 100
		}
		consumed := (target - sli) / (100 - target) * 100
		if consumed > 100 {
			consumed = 100
		}
		return 100 - consumed
	}
	overall := hd.GetOverall()
	if h := overall.GetHistory(); len(h) > 1 {
		// Cumulative average of the instantaneous SLI approximates attainment
		// over the window so far — the burndown of the budget.
		var sum float64
		out := make([]float64, 0, len(h))
		for i, p := range h {
			if len(p) < 2 {
				continue
			}
			sum += p[1]
			out = append(out, remaining(sum/float64(i+1)))
		}
		return out
	}
	series := hd.GetSeries()
	numS, denS := series.GetNumerator(), series.GetDenominator()
	num, den := numS.GetValues(), denS.GetValues()
	if len(num) > 1 && len(num) == len(den) {
		var cn, cd float64
		out := make([]float64, 0, len(num))
		for i := range num {
			cn += num[i]
			cd += den[i]
			if cd == 0 {
				continue
			}
			out = append(out, remaining(cn/cd*100))
		}
		return out
	}
	return nil
}

func (l *Live) slos(ctx context.Context) ([]Row, error) {
	api := datadogV1.NewServiceLevelObjectivesApi(l.client)
	resp, httpresp, err := api.ListSLOs(ctx,
		*datadogV1.NewListSLOsOptionalParameters().WithLimit(1000))
	l.track(httpresp)
	if err != nil {
		return nil, apiErr("slos", err)
	}
	data := resp.GetData()
	// Follow offsets if the org has more SLOs than one page.
	for page := int64(1); page < maxSLOPages && int64(len(data)) == page*sloPageSize; page++ {
		more, httpresp2, err := api.ListSLOs(ctx,
			*datadogV1.NewListSLOsOptionalParameters().WithLimit(sloPageSize).WithOffset(page * sloPageSize))
		l.track(httpresp2)
		if err != nil {
			return nil, apiErr("slos", err)
		}
		data = append(data, more.GetData()...)
		if page == maxSLOPages-1 && int64(len(more.GetData())) == sloPageSize {
			slog.Warn("slo list truncated", "cap", maxSLOPages*sloPageSize)
		}
	}
	rows := make([]Row, 0, len(data))
	for _, s := range data {
		target, timeframe := "", ""
		if th := s.GetThresholds(); len(th) > 0 {
			target = fmt.Sprintf("%.2f%%", th[0].GetTarget())
			timeframe = string(th[0].GetTimeframe())
		}
		rows = append(rows, Row{
			ID:    s.GetId(),
			Cells: []string{s.GetName(), string(s.GetType()), target, timeframe, strings.Join(s.GetTags(), ",")},
			Raw:   s,
			URL:   l.web + "/slo?slo_id=" + s.GetId(),
		})
	}
	return rows, nil
}

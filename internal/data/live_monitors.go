package data

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV1"
)

// SetMonitorMute mutes (indefinitely) or unmutes a monitor. It is a
// read-modify-write on the monitor's options so muting never clobbers
// thresholds, renotify, or any other option: fetch the monitor, flip only
// options.silenced ({"*":0} = mute all scopes; {} = unmute), write back.
func (l *Live) SetMonitorMute(ctx context.Context, id string, mute bool) error {
	ctx = l.authCtx(ctx)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	mid, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return fmt.Errorf("monitor id %q: %w", id, err)
	}
	api := datadogV1.NewMonitorsApi(l.client)
	mon, resp, err := api.GetMonitor(ctx, mid, *datadogV1.NewGetMonitorOptionalParameters())
	l.track(resp)
	if err != nil {
		return apiErr("mute: read monitor", err)
	}
	opts := mon.GetOptions()
	if mute {
		opts.Silenced = map[string]int64{"*": 0} // 0 = mute with no end time
	} else {
		opts.Silenced = map[string]int64{}
	}
	body := datadogV1.NewMonitorUpdateRequest()
	body.SetOptions(opts)
	_, resp2, err := api.UpdateMonitor(ctx, mid, *body)
	l.track(resp2)
	if err != nil {
		return apiErr("mute: update monitor", err)
	}
	slog.Info("monitor mute changed", "id", id, "muted", mute)
	return nil
}

// firstSeriesPoints extracts the value series from the first returned metric
// series, dropping null points.
func firstSeriesPoints(mq datadogV1.MetricsQueryResponse) []float64 {
	series := mq.GetSeries()
	if len(series) == 0 {
		return nil
	}
	var pts []float64
	for _, pair := range series[0].GetPointlist() {
		if len(pair) == 2 && pair[1] != nil {
			pts = append(pts, *pair[1])
		}
	}
	return pts
}

func (l *Live) monitors(ctx context.Context) ([]Row, error) {
	api := datadogV1.NewMonitorsApi(l.client)
	var rows []Row
	for page := int64(0); page < maxMonitorPages; page++ {
		mons, resp, err := api.ListMonitors(ctx,
			*datadogV1.NewListMonitorsOptionalParameters().WithPageSize(monitorPageSize).WithPage(page))
		l.track(resp)
		if err != nil {
			return nil, apiErr("monitors", err)
		}
		for _, m := range mons {
			prio := ""
			if p, ok := m.GetPriorityOk(); ok && p != nil {
				prio = fmt.Sprintf("P%d", *p)
			}
			muted := monitorMuted(m.GetOptions())
			rows = append(rows, Row{
				ID:       fmt.Sprintf("%d", m.GetId()),
				Cells:    []string{string(m.GetOverallState()), mutedCell(muted), m.GetName(), string(m.GetType()), prio, strings.Join(m.GetTags(), ",")},
				Raw:      m,
				URL:      fmt.Sprintf("%s/monitors/%d", l.web, m.GetId()),
				LogQuery: monitorLogQuery(m),
				Muted:    muted,
			})
		}
		if len(mons) < monitorPageSize {
			SortMonitors(rows)
			return rows, nil
		}
	}
	slog.Warn("monitor list truncated", "cap", maxMonitorPages*monitorPageSize)
	SortMonitors(rows)
	return rows, nil
}

// monitorMuted reports whether a monitor is currently silenced. Datadog's
// options.silenced maps a scope ("*" or a tag scope) to an end timestamp:
// 0 means muted with no end, a future unix time means muted until then, a
// past time is an expired (ineffective) entry. Muted iff any entry is 0 or
// in the future.
func monitorMuted(opts datadogV1.MonitorOptions) bool {
	now := time.Now().Unix()
	for _, end := range opts.GetSilenced() {
		if end == 0 || end > now {
			return true
		}
	}
	return false
}

func mutedCell(muted bool) string {
	if muted {
		return "muted"
	}
	return ""
}

// monitorLogQuery derives a Datadog logs search query for the monitor →
// logs drill-down ('l'). Log monitors carry their exact query inside
// logs("…"); for everything else, service:/env: tags are a good heuristic.
func monitorLogQuery(m datadogV1.Monitor) string {
	if string(m.GetType()) == "log alert" {
		q := m.GetQuery()
		if i := strings.Index(q, `logs("`); i >= 0 {
			rest := q[i+len(`logs("`):]
			if j := strings.Index(rest, `")`); j >= 0 && rest[:j] != "" {
				return rest[:j]
			}
		}
	}
	var parts []string
	for _, tag := range m.GetTags() {
		if strings.HasPrefix(tag, "service:") || strings.HasPrefix(tag, "env:") {
			parts = append(parts, tag)
		}
	}
	return strings.Join(parts, " ")
}

// MonitorMetric fetches a monitor's evaluated metric over the last hour.
// The runnable metric query is extracted best-effort from the monitor's
// alert query; non-metric monitors (service checks, log/query alerts that
// don't map to a single timeseries) return a Note instead of points.
func (l *Live) MonitorMetric(ctx context.Context, id string) (*MetricSeries, error) {
	ctx = l.authCtx(ctx)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	mid, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("monitor id %q: %w", id, err)
	}
	m, resp, err := datadogV1.NewMonitorsApi(l.client).GetMonitor(ctx, mid,
		*datadogV1.NewGetMonitorOptionalParameters())
	l.track(resp)
	if err != nil {
		return nil, apiErr("monitor metric", err)
	}
	mq := extractMonitorMetricQuery(m.GetQuery())
	if mq == "" {
		return &MetricSeries{Note: "no single metric query to chart (service check / log / composite monitor)"}, nil
	}
	from := time.Now().Add(-time.Hour).Unix()
	to := time.Now().Unix()
	res, mresp, err := datadogV1.NewMetricsApi(l.client).QueryMetrics(ctx, from, to, mq)
	l.track(mresp)
	if err != nil {
		return &MetricSeries{Query: mq, Note: "query failed: " + err.Error()}, nil
	}
	pts := firstSeriesPoints(res)
	ms := &MetricSeries{Query: mq, Points: pts}
	if len(pts) == 0 {
		ms.Note = "no data in the last 1h"
	} else {
		ms.Last = pts[len(pts)-1]
	}
	return ms, nil
}

// extractMonitorMetricQuery pulls the runnable metric query out of a monitor
// alert query like "avg(last_5m):avg:system.cpu.user{*} > 90" → the middle
// "avg:system.cpu.user{*}". Returns "" when there's no "):"-delimited metric
// body (service checks, log alerts, event alerts, etc.).
func extractMonitorMetricQuery(q string) string {
	i := strings.Index(q, "):")
	if i < 0 {
		return ""
	}
	body := q[i+2:]
	// Trim a trailing comparison "... > 90" / ">= 0.9" etc.
	for _, op := range []string{" >= ", " <= ", " > ", " < ", " == ", " != "} {
		if j := strings.LastIndex(body, op); j >= 0 {
			body = body[:j]
			break
		}
	}
	body = strings.TrimSpace(body)
	// A metric query needs a scope; bail on anything that doesn't look like one.
	if body == "" || !strings.ContainsAny(body, "{:") {
		return ""
	}
	return body
}

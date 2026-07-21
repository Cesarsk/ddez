package data

import (
	"context"
	"strings"
	"time"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV1"
)

// synthetics lists the org's synthetic tests — one call, inventory only
// (name, type, live/paused, locations, tags). Pass/fail is fetched per test
// on enter, and failing synthetics already alert through :monitors.
func (l *Live) synthetics(ctx context.Context) ([]Row, error) {
	resp, httpresp, err := datadogV1.NewSyntheticsApi(l.client).ListTests(ctx,
		*datadogV1.NewListTestsOptionalParameters())
	l.track(httpresp)
	if err != nil {
		return nil, apiErr("synthetics list", err)
	}
	tests := resp.GetTests()
	rows := make([]Row, 0, len(tests))
	for _, t := range tests {
		rows = append(rows, Row{
			ID: t.GetPublicId(),
			Cells: []string{
				string(t.GetStatus()), t.GetName(), string(t.GetType()),
				strings.Join(t.GetLocations(), ","), strings.Join(t.GetTags(), ","),
			},
			// The test type drives which latest-results endpoint the detail
			// uses; carried in Raw so the row stays self-contained.
			Raw: map[string]any{
				"public_id": t.GetPublicId(), "name": t.GetName(),
				"type": string(t.GetType()), "status": string(t.GetStatus()),
			},
			URL: l.web + "/synthetics/details/" + t.GetPublicId(),
		})
	}
	return rows, nil
}

// synthDetail fetches a synthetic test's most recent results (API or browser
// endpoint by test type — resolved with one ListTests-independent probe: try
// API first, fall back to browser).
func (l *Live) synthDetail(ctx context.Context, publicID string) (any, error) {
	api := datadogV1.NewSyntheticsApi(l.client)
	out := &SynthDetail{}
	if r, httpresp, err := api.GetAPITestLatestResults(ctx, publicID,
		*datadogV1.NewGetAPITestLatestResultsOptionalParameters()); err == nil {
		l.track(httpresp)
		for _, res := range r.GetResults() {
			rr := res.GetResult()
			out.Results = append(out.Results, SynthResult{
				CheckTime: time.UnixMilli(int64(res.GetCheckTime())).Format(time.RFC3339),
				Location:  res.GetProbeDc(),
				Passed:    rr.GetPassed(),
			})
		}
	} else if r, httpresp, err := api.GetBrowserTestLatestResults(ctx, publicID,
		*datadogV1.NewGetBrowserTestLatestResultsOptionalParameters()); err == nil {
		l.track(httpresp)
		for _, res := range r.GetResults() {
			// Browser results carry no passed flag; zero errors = a pass.
			rr := res.GetResult()
			out.Results = append(out.Results, SynthResult{
				CheckTime: time.UnixMilli(int64(res.GetCheckTime())).Format(time.RFC3339),
				Location:  res.GetProbeDc(),
				Passed:    rr.GetErrorCount() == 0,
			})
		}
	} else {
		out.Note = "latest results unavailable: " + err.Error()
		return out, nil
	}
	passed := 0
	for _, r := range out.Results {
		if r.Passed {
			passed++
		}
	}
	if n := len(out.Results); n > 0 {
		out.PassRatePct = float64(passed) / float64(n) * 100
	} else {
		out.Note = "no recent results"
	}
	return out, nil
}

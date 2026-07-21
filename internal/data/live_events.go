package data

import (
	"context"
	"strings"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV2"
)

// events lists the Datadog event stream — the "what changed" feed (deploys,
// alerts, config changes, comments). Server-side query like logs/traces.
func (l *Live) events(ctx context.Context, query, timeRange string) ([]Row, error) {
	if strings.TrimSpace(query) == "" {
		query = "*"
	}
	if timeRange == "" {
		timeRange = "now-4h" // events are sparser than logs; a wider default helps
	}
	api := datadogV2.NewEventsApi(l.client)
	resp, httpresp, err := api.ListEvents(ctx,
		*datadogV2.NewListEventsOptionalParameters().
			WithFilterQuery(query).WithFilterFrom(timeRange).WithFilterTo("now").
			WithSort(datadogV2.EVENTSSORT_TIMESTAMP_DESCENDING).
			WithPageLimit(eventsPageLimit))
	l.track(httpresp)
	if err != nil {
		return nil, apiErr("events", err)
	}
	data := resp.GetData()
	rows := make([]Row, 0, len(data))
	for _, e := range data {
		ra := e.GetAttributes()  // EventResponseAttributes: message, tags, timestamp
		ea := ra.GetAttributes() // EventAttributes: title, status, source, service
		title := ea.GetTitle()
		if title == "" {
			title = firstLine(ra.GetMessage())
		}
		source := ea.GetSourceTypeName()
		if source == "" {
			source = ea.GetService()
		}
		rows = append(rows, Row{
			ID:    e.GetId(),
			Cells: []string{ra.GetTimestamp().Local().Format("2006-01-02 15:04"), string(ea.GetStatus()), source, title, strings.Join(ra.GetTags(), ",")},
			Raw:   e,
			URL:   l.web + "/event/explorer",
		})
	}
	return rows, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

package data

import (
	"context"
	"log/slog"
	"time"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV2"
)

// CancelDowntime cancels a scheduled/active downtime by id (v2 Downtimes API).
// A write; UI-gated behind confirmation.
func (l *Live) CancelDowntime(ctx context.Context, id string) error {
	ctx = l.authCtx(ctx)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := datadogV2.NewDowntimesApi(l.client).CancelDowntime(ctx, id)
	l.track(resp)
	if err != nil {
		return apiErr("cancel downtime", err)
	}
	slog.Info("downtime cancelled", "id", id)
	return nil
}

// downtimes lists scheduled/active mutes org-wide — the visibility partner
// to the per-monitor MUTED column and the m mute action.
func (l *Live) downtimes(ctx context.Context) ([]Row, error) {
	resp, httpresp, err := datadogV2.NewDowntimesApi(l.client).ListDowntimes(ctx,
		*datadogV2.NewListDowntimesOptionalParameters().WithPageLimit(100))
	l.track(httpresp)
	if err != nil {
		return nil, apiErr("downtimes", err)
	}
	data := resp.GetData()
	rows := make([]Row, 0, len(data))
	for _, d := range data {
		a := d.GetAttributes()
		rows = append(rows, Row{
			ID:    d.GetId(),
			Cells: []string{string(a.GetStatus()), a.GetScope(), firstLine(a.GetMessage()), a.GetCreated().Local().Format("2006-01-02 15:04")},
			Raw:   d,
			URL:   l.web + "/monitors/downtimes",
		})
	}
	return rows, nil
}

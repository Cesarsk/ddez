package data

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV1"
	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV2"
)

// Live talks to the real Datadog API for one organization. Credentials are
// injected per instance (resolved from a context's env vars by the caller),
// so several Live providers for different orgs can coexist.
type Live struct {
	client *datadog.APIClient
	site   string
	apiKey string
	appKey string
	token  string // bearer/access token — used instead of the key pair
	mu     sync.Mutex
	limits map[string]string
}

func newLive(site string) *Live {
	cfg := datadog.NewConfiguration()
	cfg.SetUnstableOperationEnabled("v2.ListIncidents", true)
	return &Live{
		client: datadog.NewAPIClient(cfg),
		site:   site,
		limits: map[string]string{},
	}
}

// NewLive authenticates with an API key + application key pair.
func NewLive(site, apiKey, appKey string) *Live {
	l := newLive(site)
	l.apiKey, l.appKey = apiKey, appKey
	return l
}

// NewLiveToken authenticates with a bearer/access token (OAuth2 access
// token or PAT) via the Authorization header.
func NewLiveToken(site, token string) *Live {
	l := newLive(site)
	l.token = token
	return l
}

// authCtx attaches this org's credentials and site to a request context.
func (l *Live) authCtx(parent context.Context) context.Context {
	var ctx context.Context
	if l.token != "" {
		ctx = context.WithValue(parent, datadog.ContextAccessToken, l.token)
	} else {
		ctx = context.WithValue(parent, datadog.ContextAPIKeys, map[string]datadog.APIKey{
			"apiKeyAuth": {Key: l.apiKey},
			"appKeyAuth": {Key: l.appKey},
		})
	}
	return context.WithValue(ctx, datadog.ContextServerVariables, map[string]string{
		"site": l.site,
	})
}

func (l *Live) Mode() string { return "live" }
func (l *Live) Site() string { return l.site }

// track records the rate-limit headers Datadog returns on every response,
// so the header widget can show real remaining budget per endpoint family.
func (l *Live) track(resp *http.Response) {
	if resp == nil {
		return
	}
	name := resp.Header.Get("X-Ratelimit-Name")
	if name == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.limits[name] = fmt.Sprintf("%s %s/%s per %ss",
		name,
		resp.Header.Get("X-Ratelimit-Remaining"),
		resp.Header.Get("X-Ratelimit-Limit"),
		resp.Header.Get("X-Ratelimit-Period"))
}

func (l *Live) Budget() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.limits))
	for _, v := range l.limits {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func (l *Live) Fetch(ctx context.Context, key, query string) ([]Row, error) {
	ctx = l.authCtx(ctx)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	switch key {
	case "monitors":
		return l.monitors(ctx)
	case "incidents":
		return l.incidents(ctx)
	case "slos":
		return l.slos(ctx)
	case "logs":
		return l.logs(ctx, query)
	case "dashboards":
		return l.dashboards(ctx)
	}
	return nil, fmt.Errorf("unknown resource %q", key)
}

func (l *Live) monitors(ctx context.Context) ([]Row, error) {
	api := datadogV1.NewMonitorsApi(l.client)
	mons, resp, err := api.ListMonitors(ctx,
		*datadogV1.NewListMonitorsOptionalParameters().WithPageSize(200))
	l.track(resp)
	if err != nil {
		return nil, apiErr("monitors", err)
	}
	rows := make([]Row, 0, len(mons))
	for _, m := range mons {
		prio := ""
		if p, ok := m.GetPriorityOk(); ok && p != nil {
			prio = fmt.Sprintf("P%d", *p)
		}
		rows = append(rows, Row{
			ID:    fmt.Sprintf("%d", m.GetId()),
			Cells: []string{string(m.GetOverallState()), m.GetName(), string(m.GetType()), prio, strings.Join(m.GetTags(), ",")},
			Raw:   m,
			URL:   fmt.Sprintf("%s/monitors/%d", WebBase(l.site), m.GetId()),
		})
	}
	SortMonitors(rows)
	return rows, nil
}

func (l *Live) incidents(ctx context.Context) ([]Row, error) {
	api := datadogV2.NewIncidentsApi(l.client)
	resp, httpresp, err := api.ListIncidents(ctx,
		*datadogV2.NewListIncidentsOptionalParameters().WithPageSize(50))
	l.track(httpresp)
	if err != nil {
		return nil, apiErr("incidents", err)
	}
	data := resp.GetData()
	rows := make([]Row, 0, len(data))
	for _, d := range data {
		a := d.GetAttributes()
		sev := incidentField(a.GetFields(), "severity")
		state := incidentField(a.GetFields(), "state")
		impact := ""
		if a.GetCustomerImpacted() {
			impact = "customer"
		}
		publicID := fmt.Sprintf("%d", a.GetPublicId())
		rows = append(rows, Row{
			ID:    d.GetId(),
			Cells: []string{"IR-" + publicID, sev, state, a.GetTitle(), impact, a.GetCreated().Local().Format("2006-01-02 15:04")},
			Raw:   d,
			URL:   WebBase(l.site) + "/incidents/" + publicID,
		})
	}
	return rows, nil
}

func incidentField(fields map[string]datadogV2.IncidentFieldAttributes, key string) string {
	f, ok := fields[key]
	if !ok {
		return ""
	}
	if sv := f.IncidentFieldAttributesSingleValue; sv != nil {
		return sv.GetValue()
	}
	return ""
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
			URL:   WebBase(l.site) + "/slo?slo_id=" + s.GetId(),
		})
	}
	return rows, nil
}

func (l *Live) logs(ctx context.Context, query string) ([]Row, error) {
	if strings.TrimSpace(query) == "" {
		query = "*"
	}
	api := datadogV2.NewLogsApi(l.client)
	body := datadogV2.LogsListRequest{
		Filter: &datadogV2.LogsQueryFilter{
			Query: datadog.PtrString(query),
			From:  datadog.PtrString("now-15m"),
			To:    datadog.PtrString("now"),
		},
		Sort: datadogV2.LOGSSORT_TIMESTAMP_DESCENDING.Ptr(),
		Page: &datadogV2.LogsListRequestPage{Limit: datadog.PtrInt32(100)},
	}
	resp, httpresp, err := api.ListLogs(ctx,
		*datadogV2.NewListLogsOptionalParameters().WithBody(body))
	l.track(httpresp)
	if err != nil {
		return nil, apiErr("logs", err)
	}
	data := resp.GetData()
	rows := make([]Row, 0, len(data))
	for _, lg := range data {
		a := lg.GetAttributes()
		msg := a.GetMessage()
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			msg = msg[:i]
		}
		rows = append(rows, Row{
			ID:    lg.GetId(),
			Cells: []string{a.GetTimestamp().Local().Format("15:04:05"), a.GetStatus(), a.GetService(), a.GetHost(), msg},
			Raw:   lg,
			URL:   WebBase(l.site) + "/logs?query=" + url.QueryEscape(query),
		})
	}
	return rows, nil
}

func (l *Live) dashboards(ctx context.Context) ([]Row, error) {
	api := datadogV1.NewDashboardsApi(l.client)
	resp, httpresp, err := api.ListDashboards(ctx)
	l.track(httpresp)
	if err != nil {
		return nil, apiErr("dashboards", err)
	}
	dashs := resp.GetDashboards()
	rows := make([]Row, 0, len(dashs))
	for _, d := range dashs {
		rows = append(rows, Row{
			ID:    d.GetId(),
			Cells: []string{d.GetTitle(), string(d.GetLayoutType()), d.GetAuthorHandle(), d.GetModifiedAt().Local().Format("2006-01-02 15:04")},
			Raw:   d,
			URL:   WebBase(l.site) + d.GetUrl(),
		})
	}
	return rows, nil
}

func apiErr(what string, err error) error {
	if oe, ok := err.(datadog.GenericOpenAPIError); ok && len(oe.Body()) > 0 {
		body := string(oe.Body())
		if len(body) > 200 {
			body = body[:200]
		}
		return fmt.Errorf("%s: %s — %s", what, err.Error(), body)
	}
	return fmt.Errorf("%s: %w", what, err)
}

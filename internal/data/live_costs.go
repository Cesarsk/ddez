package data

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV2"
)

// allProducts labels the per-month org-total row in the costs table.
const allProducts = "ALL PRODUCTS"

// costChargeType is the charge line kept per product: "total" folds the
// committed/on_demand split into the one number that answers "what does
// this product cost" — the split stays visible in the row's detail (Raw).
const costChargeType = "total"

// costs lists the org's estimated cost per product for the current and
// previous month (one GetEstimatedCostByOrg call), newest month first,
// biggest cost first, with an ALL PRODUCTS total row leading each month.
// Estimated cost data lags up to 72h; the endpoint is parent-org only.
func (l *Live) costs(ctx context.Context) ([]Row, error) {
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
	resp, httpresp, err := datadogV2.NewUsageMeteringApi(l.client).GetEstimatedCostByOrg(ctx,
		*datadogV2.NewGetEstimatedCostByOrgOptionalParameters().WithStartMonth(start))
	l.track(httpresp)
	if err != nil {
		return nil, apiErr("costs", err)
	}
	rows := make([]Row, 0, 32)
	for _, d := range resp.GetData() {
		rows = append(rows, costRows(d, l.web)...)
	}
	sortCostRows(rows)
	return rows, nil
}

// costRows flattens one org+month cost record into table rows: the org
// total first, then one row per product's "total" charge line.
func costRows(d datadogV2.CostByOrg, web string) []Row {
	at := d.GetAttributes()
	month := at.GetDate().UTC().Format("2006-01")
	org := at.GetOrgName()
	total := at.GetTotalCost()
	url := web + "/billing/usage"
	rows := []Row{{
		ID:    month + "/" + org + "/" + allProducts,
		Cells: []string{month, org, allProducts, money(total), share(total, total)},
		Raw:   d,
		URL:   url,
	}}
	for _, c := range at.GetCharges() {
		if c.GetChargeType() != costChargeType {
			continue
		}
		rows = append(rows, Row{
			ID:    month + "/" + org + "/" + c.GetProductName(),
			Cells: []string{month, org, c.GetProductName(), money(c.GetCost()), share(c.GetCost(), total)},
			Raw:   d,
			URL:   url,
		})
	}
	return rows
}

// sortCostRows orders the table for triage: newest month first, then org,
// the ALL PRODUCTS total leading, then products by cost descending.
func sortCostRows(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i].Cells, rows[j].Cells
		if a[0] != b[0] {
			return a[0] > b[0]
		}
		if a[1] != b[1] {
			return a[1] < b[1]
		}
		if (a[2] == allProducts) != (b[2] == allProducts) {
			return a[2] == allProducts
		}
		return parseMoney(a[3]) > parseMoney(b[3])
	})
}

func money(v float64) string {
	return fmt.Sprintf("%.2f", v)
}

func parseMoney(s string) float64 {
	var v float64
	_, _ = fmt.Sscanf(s, "%f", &v)
	return v
}

// share renders v as a percentage of the month's org total ("" when the
// total is zero — no meaningful share to report).
func share(v, total float64) string {
	if total <= 0 {
		return ""
	}
	return fmt.Sprintf("%.1f%%", v/total*100)
}

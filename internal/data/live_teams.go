package data

import (
	"context"
	"strconv"
	"time"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV2"
)

// listTeams fetches the org's teams (one page, sorted by name). Shared by the
// :teams and :oncall list views, which drill into different things.
func (l *Live) listTeams(ctx context.Context) ([]datadogV2.Team, error) {
	resp, httpresp, err := datadogV2.NewTeamsApi(l.client).ListTeams(ctx,
		*datadogV2.NewListTeamsOptionalParameters().WithPageSize(100).WithSort(datadogV2.LISTTEAMSSORT_NAME))
	l.track(httpresp)
	if err != nil {
		return nil, apiErr("teams", err)
	}
	return resp.GetData(), nil
}

// teams lists the org's teams with handle, member count and description.
// enter drills into a team's members.
func (l *Live) teams(ctx context.Context) ([]Row, error) {
	data, err := l.listTeams(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(data))
	for _, t := range data {
		a := t.GetAttributes()
		members := ""
		if n := a.GetUserCount(); n > 0 {
			members = strconv.Itoa(int(n))
		}
		rows = append(rows, Row{
			ID:    t.GetId(),
			Cells: []string{a.GetName(), a.GetHandle(), members, firstLine(a.GetDescription())},
			Raw:   t,
			URL:   l.web + "/organization-settings/teams/" + a.GetHandle(),
		})
	}
	return rows, nil
}

// TeamMembers resolves a team's members from one GetTeamMemberships call. The
// response is JSON:API: each membership carries a role and references a user,
// and the users themselves are in included[].
func (l *Live) TeamMembers(ctx context.Context, teamID string) ([]TeamMember, error) {
	ctx = l.authCtx(ctx)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, httpresp, err := datadogV2.NewTeamsApi(l.client).GetTeamMemberships(ctx, teamID,
		*datadogV2.NewGetTeamMembershipsOptionalParameters().WithPageSize(100))
	l.track(httpresp)
	if err != nil {
		return nil, apiErr("team members", err)
	}

	users := map[string]TeamMember{}
	for _, inc := range resp.GetIncluded() {
		if inc.User == nil {
			continue
		}
		a := inc.User.GetAttributes()
		users[inc.User.GetId()] = TeamMember{
			Name: a.GetName(), Handle: a.GetHandle(), Email: a.GetEmail(),
		}
	}

	data := resp.GetData()
	out := make([]TeamMember, 0, len(data))
	for _, m := range data {
		attrs := m.GetAttributes()
		rel := m.GetRelationships()
		userRel := rel.GetUser()
		userData := userRel.GetData()
		member := users[userData.GetId()] // zero value if the user wasn't included
		member.Role = string(attrs.GetRole())
		out = append(out, member)
	}
	return out, nil
}

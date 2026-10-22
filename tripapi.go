package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A trip is a channel made from feedql's "trip" template: one topic per stop (title = the stop, filter = the places within its radius and
// the days there), a lifecycle policy and an alert rule. Wayfeed reads and writes it only through the generic API (Story 40.10):
// channels, topics, lifecycle, items with group, alert rules and events. There is no trip route involved.

// tripName is the name wayfeed gives the channel it makes.
func tripName(stops []Stop) string {
	var n []string
	for _, s := range stops {
		n = append(n, s.Name)
	}
	if len(n) == 0 {
		return "Trip"
	}
	if len(n) > 3 {
		n = append(n[:3], "...")
	}
	return "Trip: " + strings.Join(n, ", ")
}

// templateStops are the stops as the trip template takes them.
func templateStops(stops []Stop) []map[string]any {
	out := make([]map[string]any, 0, len(stops))
	for _, s := range stops {
		m := map[string]any{"place": s.Name, "lat": s.Lat, "lon": s.Lon}
		if s.Country != "" {
			m["country"] = s.Country
		}
		if s.Admin != "" {
			m["admin"] = s.Admin
		}
		if s.RadiusKm > 0 {
			m["radius_km"] = s.RadiusKm
		}
		if s.From != "" {
			m["from"], m["to"] = s.From, s.To
		}
		out = append(out, m)
	}
	return out
}

func templateBody(stops []Stop, dry bool) map[string]any {
	return map[string]any{"template": "trip", "dry_run": dry, "params": map[string]any{"name": tripName(stops), "stops": templateStops(stops)}}
}

type templateOut struct {
	Plan struct {
		Name      string `json:"name"`
		Lifecycle *struct {
			ActiveFrom       string `json:"active_from"`
			ActiveTo         string `json:"active_to"`
			ArchiveAfterDays int    `json:"archive_after_days"`
			DeleteAfterDays  int    `json:"delete_after_days"`
		} `json:"lifecycle"`
		Words  []string `json:"in_words"`
		Topics []struct {
			Title string `json:"title"`
		} `json:"topics"`
	} `json:"plan"`
	Channel *struct {
		ID string `json:"id"`
	} `json:"channel"`
}

func addDays(day string, n int) string {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, n).Format("2006-01-02")
}

// previewTrip says what making the trip would do, from the template's own plan (nothing is written).
func (a *App) previewTrip(ctx context.Context, tok string, stops []Stop) (Preview, error) {
	var out templateOut
	if err := a.api.Do(ctx, tok, http.MethodPost, "/channels/from-template", templateBody(stops, true), &out); err != nil {
		return Preview{}, err
	}
	p := Preview{Name: out.Plan.Name, Stops: len(out.Plan.Topics), Collected: strings.Join(out.Plan.Words, " "),
		Refresh: "Collection speeds up around your dates.", WhoSees: "Only you. It is private and never public.", Undo: "You can archive or delete it at any time; deleting removes everything collected for it."}
	if l := out.Plan.Lifecycle; l != nil {
		p.State, p.CollectFrom, p.EndsOn = "pending", l.ActiveFrom, l.ActiveTo
		p.DeleteOn = addDays(l.ActiveTo, l.ArchiveAfterDays+l.DeleteAfterDays)
		p.WhenDeleted = "Deleted on " + p.DeleteOn + ", with everything collected for it."
	}
	return p, nil
}

// createTrip makes the channel from the template and returns its id.
func (a *App) createTrip(ctx context.Context, tok string, stops []Stop) (string, error) {
	var out templateOut
	if err := a.api.Do(ctx, tok, http.MethodPost, "/channels/from-template", templateBody(stops, false), &out); err != nil {
		return "", err
	}
	if out.Channel == nil {
		return "", errors.New("feedql made no channel")
	}
	return out.Channel.ID, nil
}

var (
	nearRe   = regexp.MustCompile(`near:(-?[0-9.]+),(-?[0-9.]+) (\d+)km`)
	duringRe = regexp.MustCompile(`during:(\d{4}-\d\d-\d\d)\.\.(\d{4}-\d\d-\d\d)`)
)

// stopOf reads a stop back from its topic: the title is the place, the filter says where and when.
func stopOf(title, filter string) Stop {
	s := Stop{Name: title}
	if m := nearRe.FindStringSubmatch(filter); m != nil {
		s.Lat, _ = strconv.ParseFloat(m[1], 64)
		s.Lon, _ = strconv.ParseFloat(m[2], 64)
		s.RadiusKm, _ = strconv.Atoi(m[3])
	}
	if m := duringRe.FindStringSubmatch(filter); m != nil {
		s.From, s.To = m[1], m[2]
	}
	return s
}

type topicRow struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Filter string `json:"filter"`
}

// tripStops lists the stops of a trip channel in the order the person gave them (the dates, then the creation order the API lists them in).
func (a *App) tripStops(ctx context.Context, tok, id string) ([]Stop, []string, error) {
	var out struct {
		Topics []topicRow `json:"topics"`
	}
	if err := a.api.Do(ctx, tok, http.MethodGet, "/channels/"+id+"/topics", nil, &out); err != nil {
		return nil, nil, err
	}
	var stops []Stop
	var ids []string
	for _, t := range out.Topics {
		if t.Filter == "" { // the channel's raw stream is not a stop
			continue
		}
		stops, ids = append(stops, stopOf(t.Title, t.Filter)), append(ids, t.ID)
	}
	idx := make([]int, len(stops))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(i, j int) bool { return stops[idx[i]].From < stops[idx[j]].From })
	os, oi := make([]Stop, len(stops)), make([]string, len(stops))
	for k, i := range idx {
		os[k], oi[k] = stops[i], ids[i]
	}
	return os, oi, nil
}

type lifecycleOut struct {
	State            string `json:"state"`
	ActiveFrom       string `json:"active_from"`
	ActiveTo         string `json:"active_to"`
	ArchiveAfterDays int    `json:"archive_after_days"`
	DeleteAfterDays  int    `json:"delete_after_days"`
	DeleteAt         string `json:"delete_at"`
	BoostNote        string `json:"boost_note"`
}

func day10(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// currentTrip is the person's live trip (or the newest archived one): the newest channel the app made for them, nil when there is none.
func (a *App) currentTrip(ctx context.Context, tok string) (*TripView, error) {
	var ov struct {
		Channels []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Main   bool   `json:"main"`
			Paused bool   `json:"paused"`
		} `json:"channels"`
	}
	if err := a.api.Do(ctx, tok, http.MethodGet, "/channels", nil, &ov); err != nil {
		return nil, err
	}
	pick := -1
	for i, c := range ov.Channels {
		if c.Main {
			continue
		}
		if !c.Paused {
			pick = i
			break
		}
		if pick < 0 {
			pick = i
		}
	}
	if pick < 0 {
		return nil, nil
	}
	c := ov.Channels[pick]
	stops, _, err := a.tripStops(ctx, tok, c.ID)
	if err != nil {
		return nil, err
	}
	tp := &TripPart{State: "active", Stops: stops, ArchiveDays: 7, DeleteDays: 30, LeadDays: 3}
	var lc lifecycleOut
	if err := a.api.Do(ctx, tok, http.MethodGet, "/channels/"+c.ID+"/lifecycle", nil, &lc); err == nil {
		tp.State = lc.State
		tp.CollectFrom, tp.EndsOn, tp.DeleteOn = day10(lc.ActiveFrom), day10(lc.ActiveTo), day10(lc.DeleteAt)
		tp.ArchiveDays, tp.DeleteDays, tp.Note = lc.ArchiveAfterDays, lc.DeleteAfterDays, lc.BoostNote
		if tp.State == "" || tp.State == "deleting" {
			tp.State = "archived"
		}
	}
	if c.Paused {
		tp.State = "archived"
	}
	if tp.State == "active" && len(stops) > 0 && stops[0].From != "" && time.Now().UTC().Format("2006-01-02") < stops[0].From {
		tp.State = "collecting"
	}
	return &TripView{ID: c.ID, Name: c.Name, Paused: c.Paused, Trip: tp}, nil
}

// tripZone is the zone the days of a stop are cut in.
func tripZone(s Stop) string {
	switch {
	case s.Country == "LK" || s.Lat > 5.8 && s.Lat < 10 && s.Lon > 79.4 && s.Lon < 82: // a stop read back from its topic has no country, only a point
		return "Asia/Colombo"
	case s.Country == "GE" || s.Lat > 41 && s.Lat < 43.7 && s.Lon > 40 && s.Lon < 46.8:
		return "Asia/Tbilisi"
	case s.Lon < -154:
		return "Pacific/Honolulu"
	case s.Lon < -115:
		return "America/Los_Angeles"
	case s.Lon < -102:
		return "America/Denver"
	case s.Lon < -87:
		return "America/Chicago"
	case s.Lon < -60:
		return "America/New_York"
	case s.Lon != 0:
		z := int((s.Lon + 7.5) / 15)
		if z == 0 {
			return "UTC"
		}
		return fmt.Sprintf("Etc/GMT%+d", -z)
	}
	return "UTC"
}

type groupedItems struct {
	Groups []struct {
		Day    string     `json:"day"`
		Bucket string     `json:"bucket"`
		Items  []tripItem `json:"items"`
	} `json:"groups"`
}

// tripItem is an item of a private channel read: the item and the places it is about (beside it, or under _feedql as the public feed has them).
type tripItem struct {
	Item
	Places []Place `json:"places"`
	FQ     struct {
		Places []Place `json:"places"`
	} `json:"_feedql"`
}

func (t tripItem) item() Item {
	ps := t.Places
	if len(ps) == 0 {
		ps = t.FQ.Places
	}
	t.Item.Pin, t.Item.Area = locate(ps)
	return t.Item
}

const privateBackgroundMax = 5

// tripBrief reads one stop's stories (its topic) grouped by day and bucket and lays them out as the trip page shows them.
func (a *App) tripBrief(ctx context.Context, tok string, t *TripView, stop int) (StopBrief, error) {
	stops, ids, err := a.tripStops(ctx, tok, t.ID)
	if err != nil || stop < 0 || stop >= len(stops) {
		return StopBrief{}, err
	}
	return a.stopBrief(ctx, tok, t, stops, ids, stop)
}

// tripBriefs reads every stop's stories at once (the route page lays all stops out on one map and list). A stop whose read failed has an
// empty brief and is counted in failed; only when every stop failed is that an error.
func (a *App) tripBriefs(ctx context.Context, tok string, t *TripView) (briefs []StopBrief, failed int, err error) {
	stops, ids, err := a.tripStops(ctx, tok, t.ID)
	if err != nil {
		return nil, 0, err
	}
	briefs = make([]StopBrief, len(stops))
	errs := make([]error, len(stops))
	var wg sync.WaitGroup
	for i := range stops {
		wg.Add(1)
		go func() {
			defer wg.Done()
			briefs[i], errs[i] = a.stopBrief(ctx, tok, t, stops, ids, i)
		}()
	}
	wg.Wait()
	var first error
	for i, e := range errs {
		if e != nil {
			failed++
			first = e
			briefs[i] = StopBrief{Index: i, Stop: stops[i], Items: []Item{}, Days: []DayGroup{}}
		}
	}
	if len(stops) > 0 && failed == len(stops) {
		return nil, failed, first
	}
	return briefs, failed, nil
}

func (a *App) stopBrief(ctx context.Context, tok string, t *TripView, stops []Stop, ids []string, stop int) (StopBrief, error) {
	st := stops[stop]
	tz := tripZone(st)
	q := url.Values{"q": {"topic:" + ids[stop]}, "group": {"day,bucket"}, "order": {"severity"}, "collapse": {"event"}, "tz": {tz}, "lang": {"en"}, "summary": {"1"}, "limit": {"200"}}
	var out groupedItems
	if err := a.api.DoC(ctx, tok, http.MethodGet, "/c/"+t.ID+"/items?"+q.Encode(), nil, &out); err != nil {
		return StopBrief{}, err
	}
	loc, lerr := time.LoadLocation(tz)
	if lerr != nil {
		loc = time.UTC
	}
	today := time.Now().In(loc).Format("2006-01-02")
	days := tripDays(st, today)
	byDay := map[string]*Group{today: {}} // today is "Right now" even before or after the stop's own days
	for _, d := range days {
		byDay[d] = &Group{}
	}
	seen := map[string]bool{}
	brief := StopBrief{Index: stop, Stop: st, Items: []Item{}, Days: []DayGroup{}}
	for _, g := range out.Groups {
		target := byDay[g.Day]
		if target == nil {
			continue
		}
		for _, ti := range g.Items {
			it := ti.item()
			if it.Title == "" {
				continue
			}
			it.Summary = shortSummary(it.Summary)
			put(target, it)
			if !seen[it.ID] {
				seen[it.ID] = true
				brief.Items = append(brief.Items, it)
			}
		}
	}
	for _, d := range days {
		g := byDay[d]
		g.Background = capItems(g.Background, privateBackgroundMax)
		brief.Days = append(brief.Days, DayGroup{Date: d, Group: *g})
	}
	if g := byDay[today]; g != nil {
		brief.Current = *g
	}
	return brief, nil
}

// tripDays are the days of a stop (at most 14), or five days from today for a stop with no dates.
func tripDays(s Stop, today string) []string {
	from, to := s.From, s.To
	if from == "" {
		from, to = today, addDays(today, 4)
	}
	var days []string
	for d := from; d != "" && d <= to && len(days) < 14; d = addDays(d, 1) {
		days = append(days, d)
	}
	if len(days) == 0 {
		days = []string{today}
	}
	return days
}

// tripAlerts are the alerts sent for the trip's channel: its alert rule's events, newest first.
func (a *App) tripAlerts(ctx context.Context, tok, id string) (Alerts, error) {
	var rules struct {
		Rules []struct {
			ID string `json:"id"`
		} `json:"rules"`
	}
	out := Alerts{Alerts: []Alert{}, Disclaimer: demoDisclaimer}
	if err := a.api.Do(ctx, tok, http.MethodGet, "/alert-rules?channel_id="+url.QueryEscape(id), nil, &rules); err != nil {
		return out, err
	}
	for _, r := range rules.Rules {
		var ev struct {
			Alerts []Alert `json:"alerts"`
		}
		if err := a.api.Do(ctx, tok, http.MethodGet, "/alert-events?limit=20&rule_id="+url.QueryEscape(r.ID), nil, &ev); err != nil {
			return out, err
		}
		out.Alerts = append(out.Alerts, ev.Alerts...)
	}
	sort.SliceStable(out.Alerts, func(i, j int) bool { return out.Alerts[i].SentAt.After(out.Alerts[j].SentAt) })
	return out, nil
}

// archiveTrip pauses the channel: collection stops, it stays readable.
func (a *App) archiveTrip(ctx context.Context, tok, id string) error {
	return a.api.Do(ctx, tok, http.MethodPatch, "/channels/"+id, map[string]any{"paused": true}, nil)
}

// deleteTrip deletes the channel with everything collected for it.
func (a *App) deleteTrip(ctx context.Context, tok, id string) error {
	return a.api.Do(ctx, tok, http.MethodDelete, "/channels/"+id, nil, nil)
}

package main

import (
	"encoding/json"
	"html/template"
	"time"
)

// A route page lays every stop of a route out on one map and one list: a line of stations on top (the navigation), the map with the
// stories that have a point, and the stories of each stop in the list. The same view serves the public example routes and a person's trip.

// RouteStop is a stop of the route page: where it is, its dates, how many serious stories it has, and its briefing.
type RouteStop struct {
	Index    int
	Name     string
	From, To string
	Lat, Lon float64
	RadiusKm int
	Safety   int // stories in the safety bucket
	Roads    int // stories in the roads bucket
	Pinned   int // stories shown on the map
	Brief    BriefView
}

// RouteView is everything the route template needs.
type RouteView struct {
	Stops  []RouteStop
	Cur    int
	HasMap bool
	Data   template.JS // the map's data: stops and the stories with a point
}

type mapStop struct {
	I      int     `json:"i"`
	Name   string  `json:"n"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	Radius int     `json:"r"`
}

type mapItem struct {
	ID       string  `json:"id"`
	Stop     int     `json:"s"`
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	Bucket   string  `json:"b"`
	Severity int     `json:"v"`
	Official bool    `json:"o,omitempty"`
	Far      bool    `json:"far,omitempty"`
	Title    string  `json:"t"`
	Where    string  `json:"w"`
}

// stopIn is one stop as a page hands it over: the stop, the stories it lists, and its briefing.
type stopIn struct {
	Stop  Stop
	Items []Item
	Brief BriefView
}

// itemKey is the id the page and the map share for a story: the start of its feed id.
func itemKey(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// buildRoute lays the stops out. A story is on the map once, at the first stop that lists it; one with no point is only in the list.
func buildRoute(in []stopIn, cur int) RouteView {
	rv := RouteView{Stops: make([]RouteStop, len(in))}
	if cur < 0 || cur >= len(in) {
		cur = 0
	}
	rv.Cur = cur
	ms := []mapStop{}
	mi := []mapItem{}
	seen := map[string]bool{}
	for i, s := range in {
		rs := RouteStop{Index: i, Name: s.Stop.Name, From: s.Stop.From, To: s.Stop.To, Lat: s.Stop.Lat, Lon: s.Stop.Lon, RadiusKm: s.Stop.RadiusKm, Brief: s.Brief}
		if rs.RadiusKm == 0 {
			rs.RadiusKm = 25
		}
		rs.Brief.Index = i
		rs.Brief.Own = len(s.Items)
		for _, it := range s.Items {
			switch it.Bucket {
			case "safety":
				rs.Safety++
			case "roads":
				rs.Roads++
			}
		}
		if s.Stop.Lat != 0 || s.Stop.Lon != 0 {
			rv.HasMap = true
			ms = append(ms, mapStop{I: i, Name: rs.Name, Lat: rs.Lat, Lon: rs.Lon, Radius: rs.RadiusKm})
		}
		add := func(items []Item, far bool) {
			for _, it := range items {
				k := itemKey(it.ID)
				if it.Pin == nil || seen[k] {
					continue
				}
				seen[k] = true
				rs.Pinned++
				mi = append(mi, mapItem{ID: k, Stop: i, Lat: it.Pin.Lat, Lon: it.Pin.Lon, Bucket: it.Bucket, Severity: it.Severity, Official: it.Official, Far: far, Title: it.Title, Where: it.Pin.Name})
			}
		}
		add(s.Items, false)
		add(s.Brief.Elsewhere, true)
		rv.Stops[i] = rs
	}
	b, _ := json.Marshal(map[string]any{"stops": ms, "items": mi, "cur": cur})
	rv.Data = template.JS(b) // encoding/json escapes <, > and &, so nothing in a title can close the script
	return rv
}

// demoRoute is the route view of an example route.
func demoRoute(t *DemoTrip, cur int) RouteView {
	today := todayIn(demoZone[t.Slug])
	in := make([]stopIn, len(t.Stops))
	for i, s := range t.Stops {
		in[i] = stopIn{Stop: s.Stop, Items: s.Items, Brief: BriefView{Elsewhere: s.Elsewhere, ElsewhereLabel: s.ElsewhereLabel, Pending: s.Pending, Widened: s.Widened, Name: s.Name, Current: s.Current, Days: newOnly(s.Current, s.Days), Today: today}}
	}
	return buildRoute(in, cur)
}

// tripRoute is the route view of a person's trip.
func tripRoute(briefs []StopBrief, cur int) RouteView {
	in := make([]stopIn, len(briefs))
	for i, b := range briefs {
		in[i] = stopIn{Stop: b.Stop, Items: b.Items, Brief: BriefView{Name: b.Stop.Name, Current: b.Current, Days: newOnly(b.Current, b.Days), Today: todayIn(tripZone(b.Stop)), Widened: b.Widened}}
	}
	return buildRoute(in, cur)
}

// todayIn is today's date in a zone (UTC for an unknown one): the stop's days are cut in its zone, so "today" is too.
func todayIn(zone string) string {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		loc = time.UTC
	}
	return time.Now().In(loc).Format("2006-01-02")
}

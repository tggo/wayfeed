package main

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// A demo page is a read of the generic public feed of its channel (feedql's fqp_ feed): the stops are the channel's topics, and the
// feed comes back already grouped by topic, day and bucket, in the reader's language, with a summary on every story (Story 40.10).
// Nothing here is special to wayfeed on feedql's side.

// demoZone is the zone each demo's days are cut in.
var demoZone = map[string]string{"sri-lanka": "Asia/Colombo", "georgia": "Asia/Tbilisi", "us-west": "America/Los_Angeles"}

// backgroundPerStop is how many background stories a stop shows: they are context, not news.
const backgroundPerStop = 3

// demoDays is how many days a stop lays out, starting today.
const demoDays = 5

type feedDoc struct {
	Items []struct {
		ID            string `json:"id"`
		URL           string `json:"url"`
		Title         string `json:"title"`
		Summary       string `json:"summary"`
		DatePublished string `json:"date_published"`
		FQ            struct {
			Source        string `json:"source"`
			SourceName    string `json:"source_name"`
			Topic         string `json:"topic"`
			Bucket        string `json:"bucket"`
			Severity      int    `json:"severity"`
			Official      bool   `json:"official"`
			When          *When  `json:"when"`
			Lang          string `json:"lang"`
			LangOriginal  string `json:"lang_original"`
			TitleOriginal string `json:"title_original"`
			Translated    *struct {
				From string `json:"from"`
				Low  bool   `json:"low"`
			} `json:"translated"`
			Translation *struct {
				State string `json:"state"`
			} `json:"translation"`
		} `json:"_feedql"`
	} `json:"items"`
	Groups []struct {
		Day    string   `json:"day"`
		Bucket string   `json:"bucket"`
		Topic  string   `json:"topic"`
		Items  []string `json:"items"`
	} `json:"_feedql_groups"`
}

// demoFromFeed reads the public feed of a demo and lays it out as the page wants it.
func (a *App) demoFromFeed(ctx context.Context, d DemoListItem) (*DemoTrip, error) {
	tz := demoZone[d.Slug]
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	var doc feedDoc
	// the channel's raw stream ("Channel sources") repeats every item beside the stop topics and would fill the 50 rows a public read returns
	q := "?lang=en&summary=1&group=topic,day,bucket&order=severity&collapse=event&tz=" + tz + "&q=" + url.QueryEscape(`-topic:"Channel sources"`)
	if err := a.api.PublicGetURL(ctx, strings.SplitN(d.FeedURL, "?", 2)[0]+q, &doc); err != nil {
		return nil, err
	}
	items := map[string]Item{}
	pending := map[string]int{}
	for _, it := range doc.Items {
		if it.FQ.Translation != nil && it.FQ.Translation.State != "ready" {
			pending[it.FQ.Topic]++ // its headline is not in English yet: held back, counted
			continue
		}
		if it.FQ.Bucket == "" || it.Title == "" {
			continue // a story with no bucket is not a briefing story
		}
		pub, _ := time.Parse(time.RFC3339, it.DatePublished)
		x := Item{ID: it.ID, URL: it.URL, Title: it.Title, TitleOriginal: it.FQ.TitleOriginal, LangOriginal: it.FQ.LangOriginal, Source: it.FQ.SourceName, Published: pub,
			Bucket: it.FQ.Bucket, Severity: it.FQ.Severity, Official: it.FQ.Official, When: it.FQ.When, Summary: shortSummary(it.Summary)}
		if x.Source == "" {
			x.Source = it.FQ.Source
		}
		if it.FQ.Translated != nil {
			x.Translated = &struct {
				From string `json:"from"`
				Low  bool   `json:"low"`
			}{it.FQ.Translated.From, it.FQ.Translated.Low}
		}
		items[it.ID] = x
	}
	today := time.Now().In(loc).Format("2006-01-02")
	days := make([]string, demoDays)
	for i := range days {
		days[i] = time.Now().In(loc).AddDate(0, 0, i).Format("2006-01-02")
	}
	t := a.layOutStops(d, d.Stops, false, doc, items, pending, days, today)
	a.addElsewhere(ctx, d, t) // a stop with nothing near it shows its country's news under its own heading
	for _, st := range t.Stops {
		if len(st.Items) > 0 || st.Pending > 0 || len(st.Elsewhere) > 0 {
			return t, nil
		}
	}
	// the channel has no topic named like a stop yet: the whole channel is one list, grouped by day and bucket
	t = a.layOutStops(d, []string{d.Name}, true, doc, items, pending, days, today)
	for _, st := range t.Stops {
		if len(st.Items) > 0 || st.Pending > 0 {
			return t, nil
		}
	}
	return nil, errNoStops
}

// layOutStops puts the feed's groups into stops. With all, every topic counts for the one stop given (a channel without a topic per stop).
func (a *App) layOutStops(d DemoListItem, names []string, all bool, doc feedDoc, items map[string]Item, pending map[string]int, days []string, today string) *DemoTrip {
	t := &DemoTrip{Slug: d.Slug, Name: d.Name, Alerts: []DemoAlert{}, Disclaimer: demoDisclaimer, GeneratedAt: time.Now().UTC()}
	for _, name := range names {
		st := DemoStop{Stop: Stop{Name: name}, Items: []Item{}, Days: []DayGroup{}, Pending: pending[name]}
		if all {
			for _, n := range pending {
				st.Pending += n
			}
		}
		byDay := map[string]*Group{}
		for _, day := range days {
			byDay[day] = &Group{}
		}
		seen := map[string]bool{}
		for _, g := range doc.Groups {
			if !all && g.Topic != name {
				continue
			}
			target := byDay[g.Day]
			if target == nil {
				continue
			}
			for _, id := range g.Items {
				if x, ok := items[id]; ok {
					put(target, x)
					if !seen[id] {
						seen[id] = true
						st.Items = append(st.Items, x)
					}
				}
			}
		}
		for _, day := range days {
			g := byDay[day]
			g.Background = capItems(g.Background, backgroundPerStop)
			st.Days = append(st.Days, DayGroup{Date: day, Group: *g})
		}
		st.Current = *byDay[today]
		t.Stops = append(t.Stops, st)
	}
	return t
}

func put(g *Group, x Item) {
	switch x.Bucket {
	case "safety":
		g.Safety = append(g.Safety, x)
	case "roads":
		g.Roads = append(g.Roads, x)
	case "events":
		g.Events = append(g.Events, x)
	default:
		g.Background = append(g.Background, x)
	}
}

func capItems(in []Item, n int) []Item {
	if len(in) > n {
		return in[:n]
	}
	return in
}

const demoDisclaimer = "Wayfeed shows what local sources reported. It is not an official warning, and nothing here says a place is safe."

var errNoStops = errors.New("the demo channel has no topic named like a stop yet")

// elsewhereMax is how many stories a stop shows from outside its radius.
const elsewhereMax = 5

var countryName = map[string]string{"LK": "Sri Lanka", "GE": "Georgia", "US": "the US"}

// addElsewhere gives each stop a second list, read from the same channel with a filter of its own: the stop's country (for the US, a
// wider radius around the stop, since a country is too big), minus what the stop already shows, labelled plainly so it is never taken
// for news near the stop.
func (a *App) addElsewhere(ctx context.Context, d DemoListItem, t *DemoTrip) {
	reads := map[string][]Item{}
	for i := range t.Stops {
		st := &t.Stops[i]
		var pl *DemoPlace
		for j := range d.Places {
			if d.Places[j].Name == st.Name {
				pl = &d.Places[j]
			}
		}
		if pl == nil {
			continue
		}
		q, where := "", ""
		switch {
		case pl.Country != "" && pl.Country != "US":
			q, where = "country:"+pl.Country, countryName[pl.Country]
			if where == "" {
				where = pl.Country
			}
		case pl.Lat != 0 || pl.Lon != 0:
			q, where = "near:"+strconv.FormatFloat(pl.Lat, 'f', -1, 64)+","+strconv.FormatFloat(pl.Lon, 'f', -1, 64)+" 150km", "within 150 km"
		default:
			continue
		}
		items, ok := reads[q]
		if !ok {
			items = a.readElsewhere(ctx, d.FeedURL, q)
			reads[q] = items
		}
		shown := map[string]bool{}
		for _, it := range st.Items {
			shown[it.ID] = true
		}
		for _, it := range items {
			if !shown[it.ID] && len(st.Elsewhere) < elsewhereMax {
				st.Elsewhere = append(st.Elsewhere, it)
			}
		}
		if len(st.Elsewhere) == 0 {
			continue
		}
		in := "Elsewhere in " + where
		if strings.HasPrefix(where, "within") {
			in = "Elsewhere " + where
		}
		if len(st.Items) == 0 {
			in = "Nothing reported near " + st.Name + " yet. News from " + strings.TrimPrefix(where, "within ")
			if strings.HasPrefix(where, "within") {
				in = "Nothing reported near " + st.Name + " yet. News " + where
			}
		}
		st.ElsewhereLabel = in
	}
}

// readElsewhere reads the demo's feed with one more filter and returns the briefing stories (a bucket, a headline in English).
func (a *App) readElsewhere(ctx context.Context, feedURL, q string) []Item {
	var doc feedDoc
	filter := q + " bucket:safety,roads,events,background -source:reddit"
	u := strings.SplitN(feedURL, "?", 2)[0] + "?lang=en&summary=1&order=severity&collapse=event&q=" + url.QueryEscape(filter)
	if err := a.api.PublicGetURL(ctx, u, &doc); err != nil {
		return nil
	}
	var out []Item
	for _, it := range doc.Items {
		if it.FQ.Bucket == "" || it.Title == "" || (it.FQ.Translation != nil && it.FQ.Translation.State != "ready") {
			continue
		}
		pub, _ := time.Parse(time.RFC3339, it.DatePublished)
		x := Item{ID: it.ID, URL: it.URL, Title: it.Title, TitleOriginal: it.FQ.TitleOriginal, LangOriginal: it.FQ.LangOriginal, Source: it.FQ.SourceName, Published: pub,
			Bucket: it.FQ.Bucket, Severity: it.FQ.Severity, Official: it.FQ.Official, When: it.FQ.When, Summary: shortSummary(it.Summary)}
		if x.Source == "" {
			x.Source = it.FQ.Source
		}
		if it.FQ.Translated != nil {
			x.Translated = &struct {
				From string `json:"from"`
				Low  bool   `json:"low"`
			}{it.FQ.Translated.From, it.FQ.Translated.Low}
		}
		out = append(out, x)
	}
	return out
}

// shortSummary is the first two sentences of a summary: a line or two under the headline, never a paragraph.
func shortSummary(s string) string {
	s = strings.TrimSpace(s)
	n, end := 0, 0
	for i, r := range s {
		if (r == '.' || r == '!' || r == '?') && (i+1 == len(s) || s[i+1] == ' ') {
			n++
			end = i + 1
			if n == 2 {
				break
			}
		}
	}
	if n == 0 || end == 0 {
		return s
	}
	return strings.TrimSpace(s[:end])
}

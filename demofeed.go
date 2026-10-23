package main

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"
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

// feedItem is one item of the public JSON Feed with feedql's extension.
type feedItem struct {
	ID            string `json:"id"`
	URL           string `json:"url"`
	Title         string `json:"title"`
	Summary       string `json:"summary"`
	DatePublished string `json:"date_published"`
	FQ            struct {
		Source        string  `json:"source"`
		SourceName    string  `json:"source_name"`
		Topic         string  `json:"topic"`
		Bucket        string  `json:"bucket"`
		Severity      int     `json:"severity"`
		Official      bool    `json:"official"`
		When          *When   `json:"when"`
		Lang          string  `json:"lang"`
		LangOriginal  string  `json:"lang_original"`
		TitleOriginal string  `json:"title_original"`
		Places        []Place `json:"places"`
		Translated    *struct {
			From string `json:"from"`
			Low  bool   `json:"low"`
		} `json:"translated"`
		Translation *struct {
			State string `json:"state"`
		} `json:"translation"`
	} `json:"_feedql"`
}

// item is the briefing story of a feed item.
func (it feedItem) item() Item {
	pub, _ := time.Parse(time.RFC3339, it.DatePublished)
	x := Item{ID: it.ID, URL: it.URL, Title: it.Title, TitleOriginal: it.FQ.TitleOriginal, LangOriginal: it.FQ.LangOriginal, Source: it.FQ.SourceName, Published: pub,
		Bucket: it.FQ.Bucket, Severity: it.FQ.Severity, Official: it.FQ.Official, When: it.FQ.When, Summary: shortSummary(it.Summary)}
	if x.Source == "" {
		x.Source = it.FQ.Source
	}
	x.Pin, x.Area = locate(it.FQ.Places)
	if it.FQ.Translated != nil {
		x.Translated = &struct {
			From string `json:"from"`
			Low  bool   `json:"low"`
		}{it.FQ.Translated.From, it.FQ.Translated.Low}
	}
	return fixPageTitle(x)
}

type feedDoc struct {
	Items  []feedItem `json:"items"`
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
	if t := a.demoFromNear(ctx, d, tz, loc); t != nil {
		return t, nil
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
		items[it.ID] = it.item()
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

// thingsMax is how many stories "Events & things to do" shows for a stop.
const thingsMax = 6

// latestMax is how many of a stop's latest stories stand under "Right now" when none is from the days the page lays out.
const latestMax = 4

// thingsDays is how far ahead the events of a stop are looked for.
const thingsDays = 30

// demoFromNear reads each stop of a demo with generic FQL on the channel's public feed: the stories whose subject place is within the
// stop's radius (near:), narrowed by the demo's own filter (country, junk types), grouped by day and bucket. The channel needs no topic
// per stop. A stop also gets "Events & things to do": the events of the next 30 days and the positive stories near it. It returns nil
// when no stop has a point (the topic path then reads the channel) or the feed has nothing for any stop.
func (a *App) demoFromNear(ctx context.Context, d DemoListItem, tz string, loc *time.Location) *DemoTrip {
	today := time.Now().In(loc).Format("2006-01-02")
	days := make([]string, demoDays)
	for i := range days {
		days[i] = time.Now().In(loc).AddDate(0, 0, i).Format("2006-01-02")
	}
	horizon := time.Now().In(loc).AddDate(0, 0, thingsDays).Format("2006-01-02")
	t := &DemoTrip{Slug: d.Slug, Name: d.Name, Alerts: []DemoAlert{}, Disclaimer: demoDisclaimer, GeneratedAt: time.Now().UTC()}
	t.Stops = make([]DemoStop, len(d.Stops))
	var wg sync.WaitGroup
	pointed := false
	for i, name := range d.Stops {
		stop := demoStopOf(d, name)
		t.Stops[i] = DemoStop{Stop: stop, Items: []Item{}, Days: []DayGroup{}}
		if stop.Lat == 0 && stop.Lon == 0 {
			continue
		}
		pointed = true
		wg.Add(1)
		go func() {
			defer wg.Done()
			near := "near:" + strconv.FormatFloat(stop.Lat, 'f', -1, 64) + "," + strconv.FormatFloat(stop.Lon, 'f', -1, 64) + " " + strconv.Itoa(stopRadius(stop)) + "km"
			t.Stops[i] = a.readNearStop(ctx, d, stop, near, tz, days, today, horizon)
		}()
	}
	wg.Wait()
	if !pointed {
		return nil
	}
	a.addElsewhere(ctx, d, t)
	for _, st := range t.Stops {
		if len(st.Items) > 0 || st.Pending > 0 || len(st.Things) > 0 || len(st.Elsewhere) > 0 {
			return t
		}
	}
	return nil
}

func stopRadius(s Stop) int {
	if s.RadiusKm > 0 {
		return s.RadiusKm
	}
	return 25
}

// withFilter adds the demo's own filter (its country, the types it leaves out) to a query.
func withFilter(d DemoListItem, q string) string {
	if d.Filter == "" {
		return q
	}
	return q + " " + d.Filter
}

// readNearStop is one stop: its day-by-day briefing and its "Events & things to do".
func (a *App) readNearStop(ctx context.Context, d DemoListItem, stop Stop, near, tz string, days []string, today, horizon string) DemoStop {
	st := DemoStop{Stop: stop, Items: []Item{}, Days: []DayGroup{}}
	base := strings.SplitN(d.FeedURL, "?", 2)[0]
	common := "?lang=en&summary=1&collapse=event&tz=" + tz
	var doc, events, good feedDoc
	var wg sync.WaitGroup
	for _, r := range []struct {
		into *feedDoc
		path string
	}{
		{&doc, common + "&group=day,bucket&order=severity&q=" + url.QueryEscape(withFilter(d, near))},
		// the events of the next 30 days; a good-news story (tone:positive) is never a safety or roads one, and a negative one never shows here
		{&events, common + "&order=time&q=" + url.QueryEscape(withFilter(d, near+" bucket:events during:"+today+".."+horizon+" -tone:negative"))},
		{&good, common + "&order=time&q=" + url.QueryEscape(withFilter(d, near+" tone:positive bucket:background since:14d -protest -strike -rally -curfew -clash -riot"))},
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = a.api.PublicGetURL(ctx, base+r.path, r.into)
		}()
	}
	wg.Wait()
	items, pending := readItems(&doc)
	st.Pending = pending
	byDay := map[string]*Group{}
	for _, day := range days {
		byDay[day] = &Group{}
	}
	seenDay := map[string]bool{}
	seen := map[string]bool{}
	for _, g := range doc.Groups {
		target := byDay[g.Day]
		if target == nil {
			continue
		}
		for _, id := range g.Items {
			x, ok := items[id]
			if !ok || seenDay[g.Day+id] {
				continue
			}
			seenDay[g.Day+id] = true
			put(target, x)
			if !seen[id] {
				seen[id] = true
				st.Items = append(st.Items, x)
			}
		}
	}
	for _, day := range days {
		g := byDay[day]
		g.Background = capItems(g.Background, backgroundPerStop)
		st.Days = append(st.Days, DayGroup{Date: day, Group: *g})
	}
	st.Current = *byDay[today]
	if len(st.Items) == 0 && len(items) > 0 {
		// the stop has stories, only none from the days the page lays out (a mountain village reported four days ago): the latest ones
		// stand under "Right now" with a note, instead of the page saying nothing was reported near it
		var latest []Item
		for _, it := range doc.Items {
			if x, ok := items[it.ID]; ok && len(latest) < latestMax {
				latest = append(latest, x)
			}
		}
		for _, x := range latest {
			put(&st.Current, x)
			st.Items = append(st.Items, x)
			seen[x.ID] = true
		}
		st.Current.Background = capItems(st.Current.Background, latestMax)
		st.Widened = "Nothing new in the last few days near " + stop.Name + ". These are the latest reports from nearby."
	}
	for _, fd := range []*feedDoc{&events, &good} {
		more, _ := readItems(fd)
		for _, it := range fd.Items {
			x, ok := more[it.ID]
			if !ok || len(st.Things) >= thingsMax || thingSeen(st.Things, x.ID) || (x.Bucket != "events" && x.protest()) {
				continue
			}
			st.Things = append(st.Things, x)
		}
	}
	return st
}

func thingSeen(in []Item, id string) bool {
	for _, x := range in {
		if x.ID == id {
			return true
		}
	}
	return false
}

// readItems turns a feed document into briefing stories: a story with no bucket or no headline in English is left out, and the ones whose
// headline is still being translated are counted (the page says so).
func readItems(doc *feedDoc) (map[string]Item, int) {
	items := map[string]Item{}
	pending := 0
	for _, it := range doc.Items {
		if it.FQ.Translation != nil && it.FQ.Translation.State != "ready" {
			pending++
			continue
		}
		if it.FQ.Bucket == "" || it.Title == "" {
			continue
		}
		items[it.ID] = it.item()
	}
	return items, pending
}

// layOutStops puts the feed's groups into stops. With all, every topic counts for the one stop given (a channel without a topic per stop).
func (a *App) layOutStops(d DemoListItem, names []string, all bool, doc feedDoc, items map[string]Item, pending map[string]int, days []string, today string) *DemoTrip {
	t := &DemoTrip{Slug: d.Slug, Name: d.Name, Alerts: []DemoAlert{}, Disclaimer: demoDisclaimer, GeneratedAt: time.Now().UTC()}
	for _, name := range names {
		st := DemoStop{Stop: demoStopOf(d, name), Items: []Item{}, Days: []DayGroup{}, Pending: pending[name]}
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
			items = a.readElsewhere(ctx, d, q)
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
func (a *App) readElsewhere(ctx context.Context, d DemoListItem, q string) []Item {
	var doc feedDoc
	filter := withFilter(d, q+" bucket:safety,roads,events,background -source:reddit")
	u := strings.SplitN(d.FeedURL, "?", 2)[0] + "?lang=en&summary=1&order=severity&collapse=event&q=" + url.QueryEscape(filter)
	if err := a.api.PublicGetURL(ctx, u, &doc); err != nil {
		return nil
	}
	var out []Item
	for _, it := range doc.Items {
		if it.FQ.Bucket == "" || it.Title == "" || (it.FQ.Translation != nil && it.FQ.Translation.State != "ready") {
			continue
		}
		out = append(out, it.item())
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

// fixPageTitle: a page watcher can leave the page's own title ("DEPARTMENT OF METEOROLOGY") as an item's headline. A short all-capitals
// headline with a summary is shown as that summary's first sentence instead (the recipe is fixed at its source; this keeps the page readable).
func fixPageTitle(x Item) Item {
	t := strings.TrimSpace(x.Title)
	if x.Summary == "" || t == "" || t != strings.ToUpper(t) || len(strings.Fields(t)) > 6 {
		return x
	}
	first := x.Summary
	if i := strings.IndexAny(first, ".!?"); i > 0 {
		first = first[:i+1]
	}
	x.Title, x.Summary = strings.TrimSpace(first), ""
	return x
}

// demoStopOf is a stop of a demo with its point and radius from the configuration, when the route lists it.
func demoStopOf(d DemoListItem, name string) Stop {
	for _, p := range d.Places {
		if p.Name == name {
			return Stop{Name: name, Country: p.Country, Admin: p.Admin, Lat: p.Lat, Lon: p.Lon, RadiusKm: p.RadiusKm}
		}
	}
	return Stop{Name: name}
}

var protestWords = []string{"protest", "strike", "rally", "demonstrat", "curfew", "riot", "clash", "satyagraha", "agitation", "unrest"}

// protest says a story is about a protest, a strike or unrest in its headline or summary: the model sometimes calls such a story
// positive or background, and "Events & things to do" must never present it as good news.
func (x Item) protest() bool {
	t := strings.ToLower(x.Title + " " + x.Summary + " " + x.TitleOriginal)
	for _, w := range protestWords {
		if strings.Contains(t, w) {
			return true
		}
	}
	return false
}

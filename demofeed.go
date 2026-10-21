package main

import (
	"context"
	"errors"
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
	q := "?lang=en&summary=1&group=topic,day,bucket&order=severity&collapse=event&tz=" + tz
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
			Bucket: it.FQ.Bucket, Severity: it.FQ.Severity, Official: it.FQ.Official, When: it.FQ.When, Summary: it.Summary}
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
	for _, st := range t.Stops {
		if len(st.Items) > 0 || st.Pending > 0 {
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

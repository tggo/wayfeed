package main

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// The three pilot routes are public: anyone reads them without an account. They come from feedql's public demo endpoint
// (GET /p/trips, GET /p/trips/<slug>), are kept for five minutes here, and an older copy is shown when feedql cannot be reached.
const demoTTL = 5 * time.Minute

type demoEntry struct {
	list []DemoListItem
	trip *DemoTrip
	at   time.Time
}

type demoCache struct {
	mu sync.Mutex
	m  map[string]demoEntry
}

func (c *demoCache) get(k string) (demoEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[k]
	return e, ok
}

func (c *demoCache) put(k string, e demoEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil || len(c.m) > 50 {
		c.m = map[string]demoEntry{}
	}
	c.m[k] = e
}

// fallbackDemos is shown when feedql cannot be reached and nothing was cached yet.
var fallbackDemos = []DemoListItem{
	{Slug: "sri-lanka", Name: "Sri Lanka: Colombo, Kandy, Ella", Stops: []string{"Colombo", "Kandy", "Ella"}},
	{Slug: "georgia", Name: "Georgia: Tbilisi, Kazbegi, Batumi", Stops: []string{"Tbilisi", "Kazbegi", "Batumi"}},
	{Slug: "us-west", Name: "US west: San Francisco, Las Vegas, Flagstaff, Moab", Stops: []string{"San Francisco", "Las Vegas", "Flagstaff", "Moab"}},
}

func (a *App) demoList(ctx context.Context) []DemoListItem {
	if e, ok := a.demos.get("list"); ok && time.Since(e.at) < demoTTL {
		return e.list
	}
	var out struct {
		Trips []DemoListItem `json:"trips"`
	}
	if err := a.api.PublicGet(ctx, "/p/trips", &out); err != nil || len(out.Trips) == 0 {
		if e, ok := a.demos.get("list"); ok {
			return e.list
		}
		return fallbackDemos
	}
	a.demos.put("list", demoEntry{list: out.Trips, at: time.Now()})
	return out.Trips
}

func (a *App) demoTrip(ctx context.Context, slug string) (*DemoTrip, error) {
	k := "t:" + slug
	if e, ok := a.demos.get(k); ok && time.Since(e.at) < demoTTL {
		return e.trip, nil
	}
	var t DemoTrip
	if err := a.api.PublicGet(ctx, "/p/trips/"+slug, &t); err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			return nil, err
		}
		if e, ok := a.demos.get(k); ok {
			return e.trip, nil
		}
		return nil, err
	}
	a.demos.put(k, demoEntry{trip: &t, at: time.Now()})
	return &t, nil
}

// demoPage is /demo/<slug>: the same trip page as a person's own, read only, with no sign-in.
func (a *App) demoPage(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	t, err := a.demoTrip(r.Context(), slug)
	var ae *APIError
	switch {
	case errors.As(err, &ae) && ae.Status == http.StatusNotFound:
		a.page(w, r, http.StatusNotFound, "message", map[string]any{"Title": "No such example route", "Text": "The example routes are Sri Lanka, Georgia and the US west.", "Action": "/", "ActionText": "See the examples"})
		return
	case err != nil || t == nil || len(t.Stops) == 0:
		a.page(w, r, http.StatusBadGateway, "message", map[string]any{"Title": "The example is not available", "Text": "feedql did not answer. Try again in a minute.", "Action": r.URL.Path, "ActionText": "Reload"})
		return
	}
	stop := 0
	if v := r.URL.Query().Get("stop"); v != "" {
		for i := range t.Stops {
			if v == itoa(i) {
				stop = i
			}
		}
	}
	cur := t.Stops[stop]
	a.page(w, r, http.StatusOK, "demo", map[string]any{"Demo": t, "Stop": stop, "Cur": cur, "BV": BriefView{Pending: cur.Pending, Widened: cur.Widened, Name: cur.Name, Current: cur.Current, Days: newOnly(cur.Current, cur.Days), Today: time.Now().UTC().Format("2006-01-02")}, "Title": t.Name, "Slug": slug,
		"OGPath": "/demo/" + slug, "OGTitle": t.Name + " - an example briefing on Wayfeed", "OGDesc": "The local news near each stop of " + t.Name + ", translated, with a link to every source. Updated every day. No account needed."})
}

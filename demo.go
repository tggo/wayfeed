package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"log"
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

// demoDefs are the example routes: their stops and the public feed of the channel that holds each (feedql's fqp_ feed, an address that is
// public by design). They are configuration, read once from demos.json, not something feedql tells us (Story 40.10).
//
//go:embed demos.json
var demosJSON []byte

func loadDemoDefs(raw []byte) []DemoListItem {
	var out []DemoListItem
	if err := json.Unmarshal(raw, &out); err != nil {
		log.Printf("demos.json: %v", err)
	}
	return out
}

func (a *App) demoList(ctx context.Context) []DemoListItem { return a.demoDefs }

var errNoDemo = &APIError{Status: http.StatusNotFound, Code: "not_found", Message: "no such example"}

func (a *App) demoTrip(ctx context.Context, slug string) (*DemoTrip, error) {
	var def *DemoListItem
	for i := range a.demoDefs {
		if a.demoDefs[i].Slug == slug {
			def = &a.demoDefs[i]
		}
	}
	if def == nil {
		return nil, errNoDemo
	}
	k := "t:" + slug
	if e, ok := a.demos.get(k); ok && time.Since(e.at) < demoTTL {
		return e.trip, nil
	}
	t, err := a.demoFromFeed(ctx, *def)
	if err != nil || len(t.Stops) == 0 {
		if e, ok := a.demos.get(k); ok { // an older copy while feedql cannot be reached
			return e.trip, nil
		}
		if err == nil {
			err = errNoStops
		}
		return nil, err
	}
	a.demos.put(k, demoEntry{trip: t, at: time.Now()})
	return t, nil
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
	a.page(w, r, http.StatusOK, "demo", map[string]any{"Demo": t, "Route": demoRoute(t, stop), "Title": t.Name, "Slug": slug,
		"OGPath": "/demo/" + slug, "OGTitle": t.Name + " - an example briefing on Wayfeed", "OGDesc": "The local news near each stop of " + t.Name + ", translated, with a link to every source. Updated every day. No account needed."})
}

package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestLocatePicksTheMostSpecificPointAndNeverACentroid(t *testing.T) {
	city := Place{Name: "Kandy", Level: "city", Lat: 7.29, Lon: 80.63}
	point := Place{Name: "Temple of the Tooth", Level: "point", Lat: 7.293, Lon: 80.64}
	for name, c := range map[string]struct {
		in   []Place
		pin  string
		area string
	}{
		"city":             {[]Place{{Name: "Sri Lanka", Level: "country"}, city}, "Kandy", ""},
		"point beats city": {[]Place{city, point}, "Temple of the Tooth", ""},
		"region only":      {[]Place{{Name: "Western Province", Level: "admin1"}, {Name: "Sri Lanka", Level: "country"}}, "", "Western Province"},
		"region with a centroid is still no point": {[]Place{{Name: "Utah", Level: "region", Lat: 39.3, Lon: -111.6}}, "", "Utah"},
		"country only":               {[]Place{{Name: "Georgia", Level: "country", Lat: 42, Lon: 43.5}}, "", "Georgia"},
		"a city with no coordinates": {[]Place{{Name: "Ella", Level: "city"}, {Name: "Sri Lanka", Level: "country"}}, "", "Sri Lanka"},
		"nothing":                    {nil, "", ""},
		"unknown level with a point": {[]Place{{Name: "Pass", Level: "pass", Lat: 1, Lon: 2}}, "Pass", ""},
	} {
		pin, area := locate(c.in)
		got := ""
		if pin != nil {
			got = pin.Name
		}
		if got != c.pin || area != c.area {
			t.Errorf("%s: pin %q area %q, want %q %q", name, got, area, c.pin, c.area)
		}
	}
}

func TestBuildRouteCountsAndPutsEachStoryOnTheMapOnce(t *testing.T) {
	pin := &Pt{Lat: 7.29, Lon: 80.63, Name: "Kandy"}
	shared := Item{ID: "aaaaaaaaaaaaaaaa1", Bucket: "safety", Severity: 2, Title: `Bad </script><b>`, Pin: pin}
	in := []stopIn{
		{Stop: Stop{Name: "Colombo", Lat: 6.9, Lon: 79.8, RadiusKm: 30}, Items: []Item{shared, {ID: "r1", Bucket: "roads", Pin: pin}, {ID: "nopin", Bucket: "safety", Area: "Western Province"}}},
		{Stop: Stop{Name: "Kandy", Lat: 7.29, Lon: 80.63}, Items: []Item{shared}, Brief: BriefView{Elsewhere: []Item{{ID: "e1", Bucket: "events", Pin: &Pt{Lat: 1, Lon: 2, Name: "Far"}}}}},
	}
	rv := buildRoute(in, 1)
	if !rv.HasMap || rv.Cur != 1 || rv.Stops[0].Safety != 2 || rv.Stops[0].Roads != 1 || rv.Stops[0].Pinned != 2 || rv.Stops[1].Pinned != 1 || rv.Stops[1].RadiusKm != 25 {
		t.Fatalf("%+v", rv.Stops)
	}
	d := string(rv.Data)
	if strings.Contains(d, "</script>") || strings.Contains(d, "<b>") {
		t.Errorf("a headline closes the data script: %s", d)
	}
	if strings.Count(d, `"id":"aaaaaaaaaaaa"`) != 1 || strings.Contains(d, "nopin") || !strings.Contains(d, `"far":true`) {
		t.Errorf("one marker a story, none for an area story, elsewhere marked far: %s", d)
	}
	if rv := buildRoute(in, 9); rv.Cur != 0 {
		t.Error("a bad current stop falls back to the first")
	}
	if rv := buildRoute([]stopIn{{Stop: Stop{Name: "Somewhere"}}}, 0); rv.HasMap {
		t.Error("a route with no points has no map")
	}
}

func TestDemoRoutePageHasStationsMapDataAndSaysWhatIsNotOnTheMap(t *testing.T) {
	_, h := newTestApp(t, newFake(t))
	b := do(h, "GET", "/demo/sri-lanka", "").Body.String()
	for _, want := range []string{`class="line"`, `href="#stop-1"`, `id="stop-2"`, `id="rdata"`, `"n":"Kandy"`, `"b":"safety"`, "Near Colombo", "Across Western Province, so not on the map", `class="c c-safety"`,
		"fonts.googleapis.com", "leaflet.markercluster@1.5.3", "/static/route.js", `class="grip"`} {
		if !strings.Contains(b, want) {
			t.Errorf("route page misses %q", want)
		}
	}
	if strings.Contains(b, `"t":"Landslide closes the Kandy road"`) {
		t.Error("a story about a province is put on the map")
	}
	rec := do(h, "GET", "/", "")
	if strings.Contains(rec.Body.String(), "fonts.googleapis.com") || strings.Contains(rec.Body.String(), "route.js") {
		t.Error("the landing page loads the route page's map and fonts")
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "https://fonts.googleapis.com") || !strings.Contains(csp, "font-src https://fonts.gstatic.com") {
		t.Errorf("fonts are blocked by the policy: %s", csp)
	}
}

func TestTripRouteLaysAllStopsOnOnePageAndCarriesPointsFromTheirOwnStories(t *testing.T) {
	f := newFake(t)
	app, h := newTestApp(t, f)
	b := do(h, "GET", "/trip", "", signedIn(t, app, live())).Body.String()
	for _, want := range []string{`id="stop-0"`, `id="stop-1"`, `"n":"Kandy"`, `"b":"safety"`, `"w":"Kandy"`, "Near Kandy"} {
		if !strings.Contains(b, want) {
			t.Errorf("trip misses %q", want)
		}
	}
}

func TestTripRouteSurvivesOneStopFailingAndFailsWhenAllDo(t *testing.T) {
	f := newFake(t)
	app, h := newTestApp(t, f)
	c := signedIn(t, app, live())
	f.apiErr = func(r *http.Request) (int, string) {
		if strings.HasPrefix(r.URL.Path, "/c/ch_1/items") && strings.Contains(r.URL.Query().Get("q"), "t_ella") {
			return 500, `{"error":{"code":"internal","message":"x"}}`
		}
		return 0, ""
	}
	rec := do(h, "GET", "/trip", "", c)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Part of the briefing was not ready") || !strings.Contains(rec.Body.String(), "Curfew lifted in Kandy") {
		t.Fatalf("%d", rec.Code)
	}
	f.apiErr = func(r *http.Request) (int, string) {
		if strings.HasPrefix(r.URL.Path, "/c/ch_1/items") {
			return 500, `{"error":{"code":"internal","message":"x"}}`
		}
		return 0, ""
	}
	if rec := do(h, "GET", "/trip", "", c); rec.Code == 200 {
		t.Fatal("every stop failed and the page still says all is well")
	}
}

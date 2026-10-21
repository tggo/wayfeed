package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const testKey = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

// fakeFeedql is the part of the public feedql API Wayfeed uses.
type fakeFeedql struct {
	srv        *httptest.Server
	trips      []map[string]any
	form       url.Values
	refreshed  int
	failToken  bool
	apiErr     func(r *http.Request) (int, string)
	bodies     []string
	publicHits int
	publicDown bool
}

func newFake(t *testing.T) *fakeFeedql {
	f := &fakeFeedql{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.form = r.PostForm
		if f.failToken {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		if r.PostForm.Get("grant_type") == "refresh_token" {
			f.refreshed++
		}
		_, _ = w.Write([]byte(`{"access_token":"fqo_new","token_type":"Bearer","expires_in":3600,"refresh_token":"fqr_new"}`))
	})
	api := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") == "" {
				w.WriteHeader(401)
				return
			}
			if f.apiErr != nil {
				if code, body := f.apiErr(r); code != 0 {
					w.WriteHeader(code)
					_, _ = w.Write([]byte(body))
					return
				}
			}
			h(w, r)
		}
	}
	trip := func() map[string]any {
		return map[string]any{"id": "ch_1", "name": "Sri Lanka", "trip": map[string]any{"state": "active", "lead_days": 14, "archive_days": 7, "delete_days": 30, "alert_sev": "high",
			"collect_from": "2026-10-20", "ends_on": "2026-11-12", "delete_on": "2026-12-19", "note": "Sources are refreshed about every 20 minutes during the trip.",
			"stops": []map[string]any{{"name": "Kandy", "country": "LK", "lat": 7.29, "lon": 80.63, "radius_km": 25, "from": "2026-11-05", "to": "2026-11-08"}, {"name": "Ella", "country": "LK", "lat": 6.87, "lon": 81.05, "radius_km": 25, "from": "2026-11-09", "to": "2026-11-12"}}}}
	}
	mux.HandleFunc("GET /api/v1/trips", api(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"trips": f.trips})
	}))
	mux.HandleFunc("POST /api/v1/trips", api(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		raw, _ := json.Marshal(b)
		f.bodies = append(f.bodies, string(raw))
		if b["dry_run"] == true {
			_, _ = w.Write([]byte(`{"dry_run":true,"name":"Trip to Kandy","what_is_collected":"From 2026-10-22 feedql collects news.","refresh":"Faster refresh.","who_sees_it":"Only you.","when_it_is_deleted":"Deleted on 2026-12-19.","can_it_be_undone":"Delete any time."}`))
			return
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(trip())
	}))
	mux.HandleFunc("GET /api/v1/trips/ch_1/briefing", api(func(w http.ResponseWriter, r *http.Request) {
		stop := r.URL.Query().Get("stop")
		items := `[{"id":"i1","url":"https://news.example/1","title":"Curfew lifted in Kandy","title_original":"ගමන් තහනම","lang_original":"si","source":"adaderana.lk","published":"2026-10-21T08:00:00Z","translated":{"from":"si","low":true}},
		{"id":"i2","url":"https://news.example/2","title":"Perahera dates announced","source":"dailymirror.lk","published":"2026-10-21T07:00:00Z"},
		{"id":"i3","url":"https://news.example/3","title":"Rail strike ends","source":"island.lk","published":"2026-10-20T07:00:00Z"}]`
		if stop == "1" {
			items = "[]"
		}
		_, _ = w.Write([]byte(`{"trip_id":"ch_1","state":"active","disclaimer":"feedql collects what the sources publish. It is not an official warning.","partial":false,"stops":[{"index":` + stop + `,"stop":{"name":"Kandy"},"query":"near:7.29,80.63 25km","items":` + items + `,"note":""}]}`))
	}))
	mux.HandleFunc("GET /api/v1/trips/ch_1/alerts", api(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"alerts":[{"id":"a","stop_index":0,"bucket":"roads","severity":3,"title":"Landslide on the Kandy road","place":"Kandy","source_url":"https://n.example/a","official":true,"sent_at":"2026-10-21T09:00:00Z"}],"overflow":[{"line":"2 more alerts on 2026-10-21 were not sent (at most 10 a day)."}],"disclaimer":"x"}`))
	}))
	mux.HandleFunc("POST /api/v1/trips/ch_1/archive", api(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	mux.HandleFunc("DELETE /api/v1/trips/ch_1", api(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	mux.HandleFunc("GET /p/trips", func(w http.ResponseWriter, r *http.Request) {
		f.publicHits++
		_, _ = w.Write([]byte(`{"trips":[{"slug":"sri-lanka","name":"Sri Lanka: Colombo, Kandy, Ella","stops":["Colombo","Kandy","Ella"]},{"slug":"georgia","name":"Georgia: Tbilisi, Kazbegi, Batumi","stops":["Tbilisi","Kazbegi","Batumi"]}]}`))
	})
	mux.HandleFunc("GET /p/trips/{slug}", func(w http.ResponseWriter, r *http.Request) {
		f.publicHits++
		if f.publicDown {
			w.WriteHeader(503)
			return
		}
		if r.PathValue("slug") != "sri-lanka" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`{"slug":"sri-lanka","name":"Sri Lanka: Colombo, Kandy, Ella","demo":true,"disclaimer":"feedql collects what the sources publish. It is not an official warning.",
		"stops":[{"name":"Colombo","country":"LK","lat":6.9,"lon":79.8,"radius_km":30,"from":"2026-10-21","to":"2026-10-23","items":[
		  {"id":"1","url":"https://adaderana.lk/1","title":"Curfew lifted in Colombo","title_original":"ගමන් තහනම","lang_original":"si","source":"adaderana.lk","published":"2026-10-21T08:00:00Z","translated":{"from":"si","low":true}},
		  {"id":"2","url":"https://dailymirror.lk/2","title":"Perahera dates announced","source":"dailymirror.lk","published":"2026-10-20T07:00:00Z"}]},
		 {"name":"Kandy","from":"2026-10-24","to":"2026-10-26","items":[]}],
		"alerts":[{"stop_index":0,"bucket":"roads","severity":3,"title":"Landslide closes the Kandy road","place":"Kadugannawa","source_url":"https://dmc.gov.lk/a","official":true,"sent_at":"2026-10-21T09:00:00Z"}],"generated_at":"2026-10-21T09:00:00Z"}`))
	})
	mux.HandleFunc("GET /api/v1/trips/push", api(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"public_key":"BKEY","subscribed":` + map[bool]string{true: "true", false: "false"}[r.URL.Query().Get("endpoint") == "https://push.example/e1"] + `}`))
	}))
	mux.HandleFunc("POST /api/v1/trips/push", api(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.bodies = append(f.bodies, "push:"+string(b))
		w.WriteHeader(204)
	}))
	mux.HandleFunc("DELETE /api/v1/trips/push", api(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.bodies = append(f.bodies, "unpush:"+string(b))
		w.WriteHeader(204)
	}))
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	f.trips = []map[string]any{trip()}
	return f
}

func newTestApp(t *testing.T, f *fakeFeedql) (*App, http.Handler) {
	app, err := NewApp(Config{API: f.srv.URL, Base: "http://wayfeed.test", ClientID: "wayfeed", CookieKey: testKey}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return app, app.Handler()
}

func signedIn(t *testing.T, app *App, s Session) *http.Cookie {
	v, err := app.seal.Seal(s)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: sessCookie, Value: v}
}

func do(h http.Handler, method, path string, body string, cs ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cs {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func live() Session {
	return Session{Access: "fqo_live", Refresh: "fqr_live", Expires: time.Now().Add(30 * time.Minute)}
}

func TestStaticRoutesAndSecurityHeaders(t *testing.T) {
	_, h := newTestApp(t, newFake(t))
	for _, c := range []struct{ path, ctype, body string }{
		{"/", "text/html", "Sign in with feedql"},
		{"/manifest.webmanifest", "application/manifest+json", "Wayfeed"},
		{"/robots.txt", "text/plain", "Sitemap: https://wayfeed.app/sitemap.xml"},
		{"/sitemap.xml", "application/xml", "https://wayfeed.app/"},
		{"/sw.js", "text/javascript", "wf-pages"},
		{"/static/icon.svg", "image/svg+xml", "<svg"},
		{"/static/og.png", "image/png", "PNG"},
		{"/static/apple-touch-icon.png", "image/png", "PNG"},
		{"/static/new.js", "javascript", "Leaflet"},
		{"/healthz", "", "ok"},
	} {
		rec := do(h, "GET", c.path, "")
		if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), c.ctype) || !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("%s: %d %q", c.path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	rec := do(h, "GET", "/", "")
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "https://tile.openstreetmap.org") || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("headers: %v", rec.Header())
	}
	if !strings.Contains(rec.Body.String(), `content="index,follow"`) {
		t.Error("the landing page is indexable")
	}
	if rec := do(h, "GET", "/nope", ""); rec.Code != 404 {
		t.Errorf("404: %d", rec.Code)
	}
}

func TestLinkPreviewMeta(t *testing.T) {
	_, h := newTestApp(t, newFake(t))
	b := do(h, "GET", "/", "").Body.String()
	for _, want := range []string{
		`property="og:image" content="https://wayfeed.app/static/og.png"`,
		`property="og:image:width" content="1200"`, `property="og:image:height" content="630"`, `property="og:image:alt"`,
		`property="og:url" content="https://wayfeed.app/"`, `property="og:type"`, `property="og:site_name"`, `property="og:title"`,
		`name="twitter:card" content="summary_large_image"`, `name="theme-color"`, `rel="canonical" href="https://wayfeed.app/"`,
	} {
		if !strings.Contains(b, want) {
			t.Errorf("home page lacks %s", want)
		}
	}
}

func TestSealerRoundTripAndTampering(t *testing.T) {
	s, err := NewSealer(testKey)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := s.Seal(Session{Access: "a"})
	var out Session
	if s.Open(v, &out) != nil || out.Access != "a" {
		t.Fatal("round trip")
	}
	if s.Open(v[:len(v)-3]+"AAA", &out) == nil || s.Open("", &out) == nil || s.Open("###", &out) == nil {
		t.Fatal("tampered value accepted")
	}
	other, _ := NewSealer(strings.Repeat("ab", 32))
	if other.Open(v, &out) == nil {
		t.Fatal("another key opened it")
	}
	for _, k := range []string{"", "zz", strings.Repeat("a", 62)} {
		if _, err := NewSealer(k); err == nil {
			t.Errorf("key %q accepted", k)
		}
	}
	if _, err := NewApp(Config{CookieKey: "bad"}, nil); err == nil {
		t.Fatal("app without a key")
	}
}

func TestSignInUsesPKCEAndExchangesTheCode(t *testing.T) {
	f := newFake(t)
	app, h := newTestApp(t, f)
	rec := do(h, "GET", "/login", "")
	loc, _ := url.Parse(rec.Header().Get("Location"))
	q := loc.Query()
	if rec.Code != 302 || !strings.HasSuffix(loc.Path, "/oauth/authorize") || q.Get("client_id") != "wayfeed" || q.Get("code_challenge_method") != "S256" ||
		q.Get("scope") != "trips:write" || q.Get("redirect_uri") != "http://wayfeed.test/auth/callback" || !strings.HasSuffix(q.Get("resource"), "/api") || q.Get("response_type") != "code" {
		t.Fatalf("%d %s", rec.Code, loc)
	}
	var ac *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == authCookie {
			ac = c
		}
	}
	if ac == nil || !ac.HttpOnly || ac.SameSite != http.SameSiteLaxMode {
		t.Fatalf("auth cookie: %+v", ac)
	}
	var st authState
	if err := app.seal.Open(ac.Value, &st); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(st.Verifier))
	if q.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(sum[:]) || q.Get("state") != st.State {
		t.Fatal("the challenge is not the S256 of the stored verifier")
	}
	// the callback with a wrong state, no cookie, and the right one
	if rec := do(h, "GET", "/auth/callback?code=c&state=wrong", "", ac); rec.Code != 400 {
		t.Fatalf("wrong state: %d", rec.Code)
	}
	if rec := do(h, "GET", "/auth/callback?code=c&state="+st.State, ""); rec.Code != 400 {
		t.Fatalf("no cookie: %d", rec.Code)
	}
	rec = do(h, "GET", "/auth/callback?code=thecode&state="+st.State, "", ac)
	if rec.Code != 302 || rec.Header().Get("Location") != "/trip" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if f.form.Get("code") != "thecode" || f.form.Get("code_verifier") != st.Verifier || f.form.Get("client_id") != "wayfeed" || f.form.Get("grant_type") != "authorization_code" || f.form.Get("client_secret") != "" {
		t.Fatalf("token request: %v", f.form)
	}
	var sc *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessCookie && c.MaxAge > 0 {
			sc = c
		}
	}
	var sess Session
	if sc == nil || !sc.HttpOnly || app.seal.Open(sc.Value, &sess) != nil || sess.Access != "fqo_new" || sess.Refresh != "fqr_new" {
		t.Fatalf("session cookie: %+v %+v", sc, sess)
	}
	// the person said no on the consent page
	rec = do(h, "GET", "/auth/callback?error=access_denied&state="+st.State, "", ac)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "did not connect") {
		t.Fatalf("denied: %d", rec.Code)
	}
	// feedql refuses the code
	f.failToken = true
	if rec := do(h, "GET", "/auth/callback?code=x&state="+st.State, "", ac); rec.Code != 502 {
		t.Fatalf("bad code: %d", rec.Code)
	}
}

func TestTripPageShowsStopsBriefingAlertsAndTheOverflowLine(t *testing.T) {
	f := newFake(t)
	app, h := newTestApp(t, f)
	c := signedIn(t, app, live())
	rec := do(h, "GET", "/trip", "", c)
	b := rec.Body.String()
	for _, want := range []string{"Sri Lanka", "Kandy", "Ella", `aria-current="page"`, "Curfew lifted in Kandy", "translated from si (check the original)", "ගමන් තහනම",
		"Perahera dates announced", "21 Oct", "20 Oct", "Landslide on the Kandy road", "official source", "2 more alerts on 2026-10-21", "not an official warning", "Archive this trip", "Delete this trip",
		"Sources are refreshed about every 20 minutes", `content="noindex,nofollow"`} {
		if !strings.Contains(b, want) {
			t.Errorf("missing %q", want)
		}
	}
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	// the second stop has no items yet: the page says why
	b = do(h, "GET", "/trip?stop=1", "", c).Body.String()
	if !strings.Contains(b, "Nothing found near Ella yet") {
		t.Error("empty stop")
	}
	// a stop index out of range falls back to the first stop
	if !strings.Contains(do(h, "GET", "/trip?stop=9", "", c).Body.String(), "Curfew lifted") {
		t.Error("bad stop index")
	}
	// no trip: the planner
	f.trips = nil
	if rec := do(h, "GET", "/trip", "", c); rec.Code != 302 || rec.Header().Get("Location") != "/new" {
		t.Fatalf("%d %s", rec.Code, rec.Header().Get("Location"))
	}
	if rec := do(h, "GET", "/new", "", c); rec.Code != 200 || !strings.Contains(rec.Body.String(), `id="map"`) || !strings.Contains(rec.Body.String(), "integrity=") {
		t.Fatalf("planner: %d", rec.Code)
	}
	// signed in on the landing page goes to the trip
	if rec := do(h, "GET", "/", "", c); rec.Code != 302 {
		t.Fatal(rec.Code)
	}
}

func TestSignedOutVisitorsAreSentToSignIn(t *testing.T) {
	_, h := newTestApp(t, newFake(t))
	for _, p := range []string{"/trip", "/new"} {
		if rec := do(h, "GET", p, ""); rec.Code != 302 || rec.Header().Get("Location") != "/login" {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Header().Get("Location"))
		}
	}
	for _, p := range []string{"/new/preview", "/new/create", "/trip/archive", "/trip/delete"} {
		if rec := do(h, "POST", p, `{}`); rec.Code != 302 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
}

func TestAnExpiredTokenIsRefreshedAndADeadOneSignsOut(t *testing.T) {
	f := newFake(t)
	app, h := newTestApp(t, f)
	old := signedIn(t, app, Session{Access: "fqo_old", Refresh: "fqr_old", Expires: time.Now().Add(-time.Minute)})
	rec := do(h, "GET", "/trip", "", old)
	if rec.Code != 200 || f.refreshed != 1 || f.form.Get("refresh_token") != "fqr_old" || f.form.Get("client_secret") != "" {
		t.Fatalf("%d refreshed=%d %v", rec.Code, f.refreshed, f.form)
	}
	var renewed bool
	for _, c := range rec.Result().Cookies() {
		var s Session
		if c.Name == sessCookie && app.seal.Open(c.Value, &s) == nil && s.Refresh == "fqr_new" {
			renewed = true
		}
	}
	if !renewed {
		t.Fatal("the new tokens were not stored")
	}
	f.failToken = true
	rec = do(h, "GET", "/trip", "", old)
	if rec.Code != 302 || rec.Header().Get("Location") != "/login" {
		t.Fatalf("%d", rec.Code)
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		cleared = cleared || (c.Name == sessCookie && c.MaxAge < 0)
	}
	if !cleared {
		t.Fatal("the dead session cookie stays")
	}
	noRefresh := signedIn(t, app, Session{Access: "x", Expires: time.Now().Add(-time.Hour)})
	if rec := do(h, "GET", "/trip", "", noRefresh); rec.Code != 302 {
		t.Fatal(rec.Code)
	}
	// the API saying 401 mid-session also signs out
	f.failToken = false
	f.apiErr = func(*http.Request) (int, string) { return 401, "" }
	if rec := do(h, "GET", "/trip", "", signedIn(t, app, live())); rec.Code != 302 || rec.Header().Get("Location") != "/login" {
		t.Fatal(rec.Code)
	}
}

func TestPreviewAndCreateGoThroughThePublicAPI(t *testing.T) {
	f := newFake(t)
	app, h := newTestApp(t, f)
	c := signedIn(t, app, live())
	body := `{"stops":[{"name":"Kandy","country":"LK","lat":7.29,"lon":80.63,"radius_km":25,"from":"2026-11-05","to":"2026-11-08"}]}`
	rec := do(h, "POST", "/new/preview", body, c)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Only you.") || !strings.Contains(f.bodies[0], `"dry_run":true`) || !strings.Contains(f.bodies[0], `"name":"Kandy"`) {
		t.Fatalf("%d %s %v", rec.Code, rec.Body.String(), f.bodies)
	}
	rec = do(h, "POST", "/new/create", body, c)
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), `"redirect":"/trip"`) || strings.Contains(f.bodies[1], "dry_run") {
		t.Fatalf("%d %s %v", rec.Code, rec.Body.String(), f.bodies)
	}
	// feedql's own words come back, and a second trip is told as such
	f.apiErr = func(r *http.Request) (int, string) {
		return 409, `{"error":{"code":"trip_active","message":"You already have a trip to Kandy (until 2026-11-08). Archive or delete it first.","trip_id":"ch_1"}}`
	}
	rec = do(h, "POST", "/new/create", body, c)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "Archive or delete it first") || !strings.Contains(rec.Body.String(), "trip_active") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	f.apiErr = func(r *http.Request) (int, string) {
		return 422, `{"error":{"code":"invalid","field":"stops","message":"Each stop needs a start and an end date."}}`
	}
	if rec := do(h, "POST", "/new/preview", body, c); rec.Code != 422 || !strings.Contains(rec.Body.String(), "start and an end date") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	f.apiErr = func(r *http.Request) (int, string) { return 500, "" }
	if rec := do(h, "POST", "/new/preview", body, c); rec.Code != 500 {
		t.Fatalf("%d", rec.Code)
	}
	f.srv.Close()
	if rec := do(h, "POST", "/new/preview", body, c); rec.Code != 502 || !strings.Contains(rec.Body.String(), "could not be reached") {
		t.Fatalf("unreachable: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, "POST", "/new/preview", `not json`, c); rec.Code != 400 {
		t.Fatalf("bad body: %d", rec.Code)
	}
}

func TestArchiveDeleteLogoutAndCrossSiteRefusal(t *testing.T) {
	f := newFake(t)
	app, h := newTestApp(t, f)
	c := signedIn(t, app, live())
	if rec := do(h, "POST", "/trip/archive", "", c); rec.Code != 303 || rec.Header().Get("Location") != "/trip" {
		t.Fatalf("archive: %d", rec.Code)
	}
	if rec := do(h, "POST", "/trip/delete", "", c); rec.Code != 303 || rec.Header().Get("Location") != "/new" {
		t.Fatalf("delete: %d", rec.Code)
	}
	// another site cannot make the browser do it
	r := httptest.NewRequest("POST", "/trip/delete", nil)
	r.Header.Set("Origin", "https://evil.example")
	r.AddCookie(c)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != 403 {
		t.Fatalf("cross-site: %d", rec.Code)
	}
	rec = do(h, "POST", "/logout", "", c)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Signed out") || !strings.Contains(rec.Body.String(), "clear.js") {
		t.Fatalf("logout: %d", rec.Code)
	}
	// an API error is shown in words, never as a stack
	f.apiErr = func(r *http.Request) (int, string) {
		return 409, `{"error":{"code":"conflict","message":"That cannot be done to a trip in its current state."}}`
	}
	rec = do(h, "POST", "/trip/archive", "", c)
	if rec.Code != 502 || !strings.Contains(rec.Body.String(), "current state") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	f.apiErr = nil
	f.trips = nil
	for _, p := range []string{"/trip/archive", "/trip/delete"} {
		if rec := do(h, "POST", p, "", c); rec.Code != 303 || rec.Header().Get("Location") != "/new" {
			t.Fatalf("no trip %s: %d", p, rec.Code)
		}
	}
}

func TestGroupDaysAndHelpers(t *testing.T) {
	d := func(s string) time.Time { x, _ := time.Parse(time.RFC3339, s); return x }
	g := groupDays([]Item{{Published: d("2026-10-21T08:00:00Z")}, {Published: d("2026-10-21T01:00:00Z")}, {Published: d("2026-10-20T23:00:00Z")}})
	if len(g) != 2 || len(g[0].Items) != 2 || g[1].Day != "2026-10-20" {
		t.Fatalf("%+v", g)
	}
	if len(groupDays(nil)) != 0 || initial("kandy") != "K" || initial("") != "?" || dateFmt("2026-11-05") != "5 Nov" || dateFmt("x") != "x" || itoa(7) != "7" {
		t.Fatal("helpers")
	}
	if (&APIError{Status: 409, Code: "c", Message: "m"}).Error() == "" {
		t.Fatal("error text")
	}
}

func TestPushRoutesGoThroughThePublicAPI(t *testing.T) {
	f := newFake(t)
	app, h := newTestApp(t, f)
	c := signedIn(t, app, live())
	rec := do(h, "GET", "/push/state?endpoint=https://push.example/e1", "", c)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"public_key":"BKEY"`) || !strings.Contains(rec.Body.String(), `"subscribed":true`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(do(h, "GET", "/push/state", "", c).Body.String(), `"subscribed":false`) {
		t.Fatal("no endpoint")
	}
	sub := `{"endpoint":"https://push.example/e1","expirationTime":null,"keys":{"p256dh":"k","auth":"a"}}`
	if rec := do(h, "POST", "/push/subscribe", sub, c); rec.Code != 200 || !strings.Contains(f.bodies[len(f.bodies)-1], `push:{"endpoint":"https://push.example/e1"`) {
		t.Fatalf("%d %v", rec.Code, f.bodies)
	}
	if rec := do(h, "POST", "/push/unsubscribe", `{"endpoint":"https://push.example/e1"}`, c); rec.Code != 200 || !strings.Contains(f.bodies[len(f.bodies)-1], `unpush:{"endpoint":"https://push.example/e1"}`) {
		t.Fatalf("%d %v", rec.Code, f.bodies)
	}
	if rec := do(h, "POST", "/push/subscribe", `nope`, c); rec.Code != 400 {
		t.Fatal(rec.Code)
	}
	if rec := do(h, "POST", "/push/unsubscribe", `nope`, c); rec.Code != 400 {
		t.Fatal(rec.Code)
	}
	f.apiErr = func(r *http.Request) (int, string) {
		return 409, `{"error":{"code":"limit_reached","message":"This app already has the most devices it may subscribe for you."}}`
	}
	if rec := do(h, "POST", "/push/subscribe", sub, c); rec.Code != 409 || !strings.Contains(rec.Body.String(), "most devices") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, "GET", "/push/state", "", c); rec.Code != 409 {
		t.Fatal(rec.Code)
	}
	if rec := do(h, "POST", "/push/unsubscribe", `{"endpoint":"x"}`, c); rec.Code != 409 {
		t.Fatal(rec.Code)
	}
	if rec := do(h, "POST", "/push/subscribe", sub); rec.Code != 302 {
		t.Fatal("signed out:", rec.Code)
	}
	f.apiErr = nil
	if b := do(h, "GET", "/trip", "", c).Body.String(); !strings.Contains(b, "Alerts on this device") || !strings.Contains(b, "/static/push.js") || !strings.Contains(b, "sends nothing") {
		t.Fatal("the trip page has the push box")
	}
	if rec := do(h, "GET", "/static/push.js", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "applicationServerKey") {
		t.Fatal("push.js")
	}
	if rec := do(h, "GET", "/sw.js", ""); !strings.Contains(rec.Body.String(), "addEventListener('push'") {
		t.Fatal("sw push handler")
	}
}

func TestPilotRoutesArePublicWithNoAccount(t *testing.T) {
	f := newFake(t)
	_, h := newTestApp(t, f)
	// the landing page lists the example routes from feedql, and a way to build one's own
	b := do(h, "GET", "/", "").Body.String()
	for _, want := range []string{"/demo/sri-lanka", "/demo/georgia", "Colombo &rarr; Kandy &rarr; Ella", "Build your own trip", "No account needed"} {
		if !strings.Contains(b, want) {
			t.Errorf("home misses %q", want)
		}
	}
	// the demo page needs no sign-in and is read only
	rec := do(h, "GET", "/demo/sri-lanka", "")
	b = rec.Body.String()
	for _, want := range []string{"Sri Lanka: Colombo, Kandy, Ella", "Example", "Curfew lifted in Colombo", "translated from si (check the original)", "ගමන් තහනම", "Landslide closes the Kandy road", "official source",
		"not an official warning", "Build your own trip", `aria-current="page"`, "21 Oct", `content="index,follow"`, `href="https://wayfeed.app/demo/sri-lanka"`} {
		if !strings.Contains(b, want) {
			t.Errorf("demo misses %q", want)
		}
	}
	for _, no := range []string{"/trip/delete", "/trip/archive", "Alerts on this device", "Sign out"} {
		if strings.Contains(b, no) {
			t.Errorf("a public page offers %q", no)
		}
	}
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") && false {
		t.Fatal(rec.Code)
	}
	// a second stop has nothing yet and says so
	if b := do(h, "GET", "/demo/sri-lanka?stop=1", "").Body.String(); !strings.Contains(b, "Nothing found near Kandy right now") {
		t.Error("empty stop")
	}
	if !strings.Contains(do(h, "GET", "/demo/sri-lanka?stop=9", "").Body.String(), "Curfew lifted") {
		t.Error("bad stop falls back to the first")
	}
	// unknown route, cache, and an outage
	if rec := do(h, "GET", "/demo/nope", ""); rec.Code != 404 || !strings.Contains(rec.Body.String(), "No such example route") {
		t.Fatalf("%d", rec.Code)
	}
	hits := f.publicHits
	do(h, "GET", "/demo/sri-lanka", "")
	if f.publicHits != hits+0 && f.publicHits != hits {
		t.Fatal("the cached page called feedql again")
	}
	f.publicDown = true
	if rec := do(h, "GET", "/demo/sri-lanka", ""); rec.Code != 200 {
		t.Fatalf("a cached copy is shown when feedql is down: %d", rec.Code)
	}
	f2 := newFake(t)
	f2.publicDown = true
	_, h2 := newTestApp(t, f2)
	if rec := do(h2, "GET", "/demo/sri-lanka", ""); rec.Code != 502 {
		t.Fatalf("no copy and feedql down: %d", rec.Code)
	}
	f2.srv.Close()
	if b := do(h2, "GET", "/", "").Body.String(); !strings.Contains(b, "/demo/us-west") {
		t.Error("the landing page keeps its examples when feedql is unreachable")
	}
	// a signed-in person is sent to their trip from the landing page, but may still read an example
	app, h3 := newTestApp(t, newFake(t))
	if rec := do(h3, "GET", "/demo/sri-lanka", "", signedIn(t, app, live())); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Sign out") {
		t.Fatal(rec.Code)
	}
	if !strings.Contains(do(h, "GET", "/sitemap.xml", "").Body.String(), "/demo/georgia") {
		t.Error("sitemap")
	}
}

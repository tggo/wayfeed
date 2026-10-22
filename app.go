package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

//go:embed web
var assets embed.FS

// Config comes from the environment only; nothing here is a secret except CookieKey, which lives on the server.
type Config struct {
	API       string // https://api.feedql.com
	Base      string // https://wayfeed.app, where this site is served (the redirect address is Base + /auth/callback)
	ClientID  string // the public OAuth client registered with feedql
	CookieKey string // 64 hex characters
}

const (
	scope      = "trips:write"
	sessCookie = "wf_s"
	authCookie = "wf_a"
	sessAge    = 30 * 24 * time.Hour
)

// App is the web site: it signs people in with feedql, keeps their tokens in a sealed cookie, and reads and writes trips
// through the public API. It has no database and no user store.
type App struct {
	demos  demoCache
	cfg    Config
	seal   *Sealer
	api    *API
	tpl    *template.Template
	secure bool
}

func NewApp(cfg Config, api *API) (*App, error) {
	s, err := NewSealer(cfg.CookieKey)
	if err != nil {
		return nil, err
	}
	t, err := template.New("").Funcs(template.FuncMap{"initial": initial, "dateFmt": dateFmt, "src": srcLabel, "sev": sevLabel, "lower": strings.ToLower, "when": whenText, "dow": dowFmt}).ParseFS(assets, "web/templates/*.html")
	if err != nil {
		return nil, err
	}
	if api == nil {
		api = &API{Base: cfg.API}
	}
	return &App{cfg: cfg, seal: s, api: api, tpl: t, secure: strings.HasPrefix(cfg.Base, "https://")}, nil
}

// srcLabel names where a story came from: the site's host for a story read from a website, else the source's own name.
func srcLabel(it Item) string {
	if u, err := url.Parse(it.URL); err == nil && u.Host != "" && (it.Source == "site" || it.Source == "") {
		return strings.TrimPrefix(u.Host, "www.")
	}
	return it.Source
}

// newOnly takes out of the days the safety and roads stories "Right now" already shows: a story kept for three days is not told again
// every day.
func newOnly(cur Group, days []DayGroup) []DayGroup {
	seen := map[string]bool{}
	for _, it := range append(append([]Item{}, cur.Safety...), cur.Roads...) {
		seen[it.ID] = true
	}
	drop := func(in []Item) []Item {
		var out []Item
		for _, it := range in {
			if !seen[it.ID] {
				out = append(out, it)
			}
		}
		return out
	}
	out := make([]DayGroup, 0, len(days))
	for _, d := range days {
		d.Safety, d.Roads = drop(d.Safety), drop(d.Roads)
		out = append(out, d)
	}
	return out
}

// BriefView is what the briefing block of a stop shows.
type BriefView struct {
	Pending int    // stories held back until their headline is translated into English
	Widened string // set when the stop's own radius held little and a wider area or the country was read
	// Elsewhere are stories from outside the stop's radius (its country), shown under their own heading right after "Right now".
	Elsewhere      []Item
	ElsewhereLabel string
	Name           string
	Current        Group
	Days           []DayGroup
	Today          string
}

// sevLabel says how serious a safety or roads story is, in words (never a verdict about a place).
func sevLabel(n int) string {
	switch {
	case n >= 3:
		return "Serious"
	case n == 2:
		return "Notable"
	case n == 1:
		return "Minor"
	}
	return ""
}

// whenText is the day or days an event is on: "25 Oct", "25 to 27 Oct".
func whenText(w *When) string {
	if w == nil {
		return ""
	}
	a, e1 := time.Parse("2006-01-02", w.Start[:min(10, len(w.Start))])
	z, e2 := time.Parse("2006-01-02", w.End[:min(10, len(w.End))])
	if e1 != nil {
		return ""
	}
	if e2 != nil || z.Equal(a) {
		return a.Format("2 Jan")
	}
	if a.Month() == z.Month() {
		return a.Format("2") + " to " + z.Format("2 Jan")
	}
	return a.Format("2 Jan") + " to " + z.Format("2 Jan")
}

// dowFmt is "Tue 21 Oct" for a day.
func dowFmt(s string) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return t.Format("Mon 2 Jan")
}

func initial(s string) string {
	for _, r := range s {
		return strings.ToUpper(string(r))
	}
	return "?"
}

func dateFmt(s string) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return t.Format("2 Jan")
}

func (a *App) redirectURI() string { return strings.TrimRight(a.cfg.Base, "/") + "/auth/callback" }

func (a *App) setCookie(w http.ResponseWriter, name string, v any, age time.Duration) {
	val := ""
	if v != nil {
		val, _ = a.seal.Seal(v)
	}
	c := &http.Cookie{Name: name, Value: val, Path: "/", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode, MaxAge: int(age.Seconds())}
	if v == nil {
		c.MaxAge = -1
	}
	http.SetCookie(w, c)
}

func rnd(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

type authState struct {
	State    string    `json:"s"`
	Verifier string    `json:"v"`
	Exp      time.Time `json:"e"`
}

// login starts "Sign in with feedql": OAuth authorization code with PKCE S256, as a public client.
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	st := authState{State: rnd(16), Verifier: rnd(32), Exp: time.Now().Add(10 * time.Minute)}
	sum := sha256.Sum256([]byte(st.Verifier))
	a.setCookie(w, authCookie, st, 10*time.Minute)
	q := url.Values{"response_type": {"code"}, "client_id": {a.cfg.ClientID}, "redirect_uri": {a.redirectURI()}, "state": {st.State},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}, "scope": {scope},
		"resource": {strings.TrimRight(a.cfg.API, "/") + "/api"}}
	http.Redirect(w, r, strings.TrimRight(a.cfg.API, "/")+"/oauth/authorize?"+q.Encode(), http.StatusFound)
}

func (a *App) callback(w http.ResponseWriter, r *http.Request) {
	var st authState
	c, err := r.Cookie(authCookie)
	if err != nil || a.seal.Open(c.Value, &st) != nil || time.Now().After(st.Exp) || st.State == "" || r.URL.Query().Get("state") != st.State {
		a.page(w, r, http.StatusBadRequest, "message", map[string]any{"Title": "Sign-in did not finish", "Text": "The sign-in link expired or was opened in another browser. Start again.", "Action": "/login", "ActionText": "Sign in with feedql"})
		return
	}
	a.setCookie(w, authCookie, nil, 0)
	if e := r.URL.Query().Get("error"); e != "" {
		a.page(w, r, http.StatusOK, "message", map[string]any{"Title": "You did not connect feedql", "Text": "Wayfeed needs your feedql account to keep your trip private. Nothing was saved.", "Action": "/", "ActionText": "Back"})
		return
	}
	code := r.URL.Query().Get("code")
	t, err := a.api.Exchange(r.Context(), url.Values{"grant_type": {"authorization_code"}, "client_id": {a.cfg.ClientID}, "code": {code},
		"redirect_uri": {a.redirectURI()}, "code_verifier": {st.Verifier}})
	if err != nil {
		log.Printf("token exchange: %v", err)
		a.page(w, r, http.StatusBadGateway, "message", map[string]any{"Title": "feedql did not answer", "Text": "The sign-in could not be completed. Try again in a minute.", "Action": "/login", "ActionText": "Try again"})
		return
	}
	a.setCookie(w, sessCookie, newSession(t), sessAge)
	http.Redirect(w, r, "/trip", http.StatusFound)
}

func newSession(t Token) Session {
	return Session{Access: t.Access, Refresh: t.Refresh, Expires: time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)}
}

// token returns a valid access token for the request, refreshing it (and the cookie) when it is about to end.
func (a *App) token(w http.ResponseWriter, r *http.Request) (string, error) {
	var s Session
	c, err := r.Cookie(sessCookie)
	if err != nil || a.seal.Open(c.Value, &s) != nil || s.Access == "" {
		return "", ErrSignedOut
	}
	if time.Until(s.Expires) > time.Minute {
		return s.Access, nil
	}
	if s.Refresh == "" {
		a.setCookie(w, sessCookie, nil, 0)
		return "", ErrSignedOut
	}
	t, err := a.api.Exchange(r.Context(), url.Values{"grant_type": {"refresh_token"}, "client_id": {a.cfg.ClientID}, "refresh_token": {s.Refresh}})
	if errors.Is(err, ErrSignedOut) {
		a.setCookie(w, sessCookie, nil, 0)
		return "", ErrSignedOut
	}
	if err != nil {
		return "", err
	}
	a.setCookie(w, sessCookie, newSession(t), sessAge)
	return t.Access, nil
}

func (a *App) signedIn(r *http.Request) bool {
	var s Session
	c, err := r.Cookie(sessCookie)
	return err == nil && a.seal.Open(c.Value, &s) == nil && s.Access != ""
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	a.setCookie(w, sessCookie, nil, 0)
	a.page(w, r, http.StatusOK, "message", map[string]any{"Title": "Signed out", "Text": "Your trip stays in your feedql account. You can disconnect Wayfeed any time under Connected apps in feedql.", "Action": "/", "ActionText": "Back", "ClearCache": true})
}

func (a *App) page(w http.ResponseWriter, r *http.Request, code int, name string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["SignedIn"] = a.signedIn(r)
	if _, ok := data["Title"]; !ok {
		data["Title"] = map[string]string{"new": "Plan your trip", "trip": "Your trip"}[name]
	}
	data["Page"] = name
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(code)
	if err := a.tpl.ExecuteTemplate(w, name+".html", data); err != nil {
		log.Printf("template %s: %v", name, err)
	}
}

// guard runs h with the person's token; no sign-in sends them to the landing page, a dead one to sign in again.
func (a *App) guard(h func(w http.ResponseWriter, r *http.Request, tok string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, err := a.token(w, r)
		switch {
		case errors.Is(err, ErrSignedOut):
			http.Redirect(w, r, "/login", http.StatusFound)
		case err != nil:
			a.page(w, r, http.StatusBadGateway, "message", map[string]any{"Title": "feedql did not answer", "Text": "Try again in a minute.", "Action": r.URL.Path, "ActionText": "Reload"})
		default:
			h(w, r, tok)
		}
	}
}

// failAPI shows a feedql error to the person in words.
func (a *App) failAPI(w http.ResponseWriter, r *http.Request, err error) {
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusUnauthorized {
		a.setCookie(w, sessCookie, nil, 0)
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	log.Printf("feedql: %v", err)
	text := "feedql could not be reached. Try again in a minute."
	if errors.As(err, &ae) && ae.Message != "" && ae.Status < 500 {
		text = ae.Message
	}
	a.page(w, r, http.StatusBadGateway, "message", map[string]any{"Title": "Something went wrong", "Text": text, "Action": "/trip", "ActionText": "Back to your trip"})
}

// currentTrip is the person's live trip (or the newest one), nil when they have none.
func (a *App) currentTrip(ctx context.Context, tok string) (*TripView, error) {
	var out struct {
		Trips []TripView `json:"trips"`
	}
	if err := a.api.Do(ctx, tok, http.MethodGet, "/trips", nil, &out); err != nil {
		return nil, err
	}
	var pick *TripView
	for i := range out.Trips {
		t := &out.Trips[i]
		if t.Trip != nil && t.Trip.State != "archived" {
			return t, nil
		}
		if pick == nil {
			pick = t
		}
	}
	return pick, nil
}

func (a *App) home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if a.signedIn(r) {
		http.Redirect(w, r, "/trip", http.StatusFound)
		return
	}
	a.page(w, r, http.StatusOK, "home", map[string]any{"Demos": a.demoList(r.Context())})
}

func (a *App) tripPage(w http.ResponseWriter, r *http.Request, tok string) {
	t, err := a.currentTrip(r.Context(), tok)
	if err != nil {
		a.failAPI(w, r, err)
		return
	}
	if t == nil {
		http.Redirect(w, r, "/new", http.StatusFound)
		return
	}
	stop := 0
	if v := r.URL.Query().Get("stop"); v != "" {
		for i := range t.Trip.Stops {
			if v == itoa(i) {
				stop = i
			}
		}
	}
	var b Briefing
	if err := a.api.Do(r.Context(), tok, http.MethodGet, "/trips/"+t.ID+"/briefing?stop="+itoa(stop), nil, &b); err != nil {
		a.failAPI(w, r, err)
		return
	}
	var al Alerts
	if err := a.api.Do(r.Context(), tok, http.MethodGet, "/trips/"+t.ID+"/alerts?limit=20", nil, &al); err != nil {
		a.failAPI(w, r, err)
		return
	}
	var cur StopBrief
	if len(b.Stops) > 0 {
		cur = b.Stops[0]
	}
	a.page(w, r, http.StatusOK, "trip", map[string]any{"Trip": t, "Stop": stop, "Brief": cur, "Disclaimer": b.Disclaimer, "Partial": b.Partial, "Alerts": al, "BV": BriefView{Widened: cur.Widened, Name: t.Trip.Stops[stop].Name, Current: cur.Current, Days: newOnly(cur.Current, cur.Days), Today: time.Now().UTC().Format("2006-01-02")}})
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func (a *App) newPage(w http.ResponseWriter, r *http.Request, tok string) {
	t, err := a.currentTrip(r.Context(), tok)
	if err != nil {
		a.failAPI(w, r, err)
		return
	}
	if t != nil && t.Trip != nil && t.Trip.State != "archived" {
		http.Redirect(w, r, "/trip", http.StatusFound)
		return
	}
	a.page(w, r, http.StatusOK, "new", nil)
}

// tripBody is what the map page sends: the stops, as the public API takes them.
type tripBody struct {
	Stops  []Stop `json:"stops"`
	DryRun bool   `json:"dry_run,omitempty"`
}

func readBody(w http.ResponseWriter, r *http.Request) (tripBody, bool) {
	var b tripBody
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "The trip could not be read."})
		return b, false
	}
	return b, true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// apiJSON answers the map page's calls with feedql's own message so the person reads what went wrong.
func (a *App) apiJSON(w http.ResponseWriter, err error) {
	var ae *APIError
	if errors.As(err, &ae) {
		code := ae.Status
		if code == http.StatusUnauthorized {
			code = http.StatusUnauthorized
		}
		writeJSON(w, code, map[string]any{"error": ae.Message, "code": ae.Code, "field": ae.Field})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]any{"error": "feedql could not be reached. Try again in a minute."})
}

func (a *App) preview(w http.ResponseWriter, r *http.Request, tok string) {
	b, ok := readBody(w, r)
	if !ok {
		return
	}
	var p Preview
	if err := a.api.Do(r.Context(), tok, http.MethodPost, "/trips", map[string]any{"stops": b.Stops, "dry_run": true}, &p); err != nil {
		a.apiJSON(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *App) create(w http.ResponseWriter, r *http.Request, tok string) {
	b, ok := readBody(w, r)
	if !ok {
		return
	}
	var v TripView
	if err := a.api.Do(r.Context(), tok, http.MethodPost, "/trips", map[string]any{"stops": b.Stops}, &v); err != nil {
		a.apiJSON(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"redirect": "/trip", "id": v.ID})
}

func (a *App) archive(w http.ResponseWriter, r *http.Request, tok string) {
	t, err := a.currentTrip(r.Context(), tok)
	if err != nil {
		a.failAPI(w, r, err)
		return
	}
	if t == nil {
		http.Redirect(w, r, "/new", http.StatusSeeOther)
		return
	}
	if err := a.api.Do(r.Context(), tok, http.MethodPost, "/trips/"+t.ID+"/archive", nil, nil); err != nil {
		a.failAPI(w, r, err)
		return
	}
	http.Redirect(w, r, "/trip", http.StatusSeeOther)
}

func (a *App) remove(w http.ResponseWriter, r *http.Request, tok string) {
	t, err := a.currentTrip(r.Context(), tok)
	if err != nil {
		a.failAPI(w, r, err)
		return
	}
	if t == nil {
		http.Redirect(w, r, "/new", http.StatusSeeOther)
		return
	}
	if err := a.api.Do(r.Context(), tok, http.MethodDelete, "/trips/"+t.ID, nil, nil); err != nil {
		a.failAPI(w, r, err)
		return
	}
	http.Redirect(w, r, "/new", http.StatusSeeOther)
}

// pushState answers the trip page's script: the key to subscribe with and whether this browser is subscribed.
func (a *App) pushState(w http.ResponseWriter, r *http.Request, tok string) {
	var out map[string]any
	if err := a.api.Do(r.Context(), tok, http.MethodGet, "/trips/push?endpoint="+url.QueryEscape(r.URL.Query().Get("endpoint")), nil, &out); err != nil {
		a.apiJSON(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// pushSubscribe hands this browser's push subscription to feedql: from then on feedql sends the trip's alerts to it.
func (a *App) pushSubscribe(w http.ResponseWriter, r *http.Request, tok string) {
	var b json.RawMessage
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "The subscription could not be read."})
		return
	}
	if err := a.api.Do(r.Context(), tok, http.MethodPost, "/trips/push", b, nil); err != nil {
		a.apiJSON(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) pushUnsubscribe(w http.ResponseWriter, r *http.Request, tok string) {
	var b struct {
		Endpoint string `json:"endpoint"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "The request could not be read."})
		return
	}
	if err := a.api.Do(r.Context(), tok, http.MethodDelete, "/trips/push", b, nil); err != nil {
		a.apiJSON(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// sameOrigin refuses a state-changing request that comes from another site (the cookies are SameSite=Lax as well).
func (a *App) sameOrigin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" && o != strings.TrimRight(a.cfg.Base, "/") {
			http.Error(w, "cross-site request refused", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' https://unpkg.com; style-src 'self' https://unpkg.com; img-src 'self' data: https://tile.openstreetmap.org https://unpkg.com; connect-src 'self' https://nominatim.openstreetmap.org; manifest-src 'self'; worker-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self' https://api.feedql.com")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.ServeHTTP(w, r)
	})
}

// Handler is the whole site.
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	root, _ := fs.Sub(assets, "web")
	static, _ := fs.Sub(assets, "web/static")
	file := func(path, ctype, cache string) {
		b, err := fs.ReadFile(root, path)
		if err != nil {
			log.Fatal(err)
		}
		mux.HandleFunc("GET /"+path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ctype)
			w.Header().Set("Cache-Control", cache)
			_, _ = w.Write(b)
		})
	}
	file("manifest.webmanifest", "application/manifest+json", "public, max-age=3600")
	file("robots.txt", "text/plain; charset=utf-8", "public, max-age=3600")
	file("sitemap.xml", "application/xml", "public, max-age=3600")
	file("sw.js", "text/javascript; charset=utf-8", "no-cache")
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheFS(http.FileServerFS(static))))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("GET /{$}", a.home)
	mux.HandleFunc("GET /demo/{slug}", a.demoPage)
	mux.HandleFunc("GET /login", a.login)
	mux.HandleFunc("GET /auth/callback", a.callback)
	mux.HandleFunc("POST /logout", a.sameOrigin(a.logout))
	mux.HandleFunc("GET /trip", a.guard(a.tripPage))
	mux.HandleFunc("GET /new", a.guard(a.newPage))
	mux.HandleFunc("POST /new/preview", a.sameOrigin(a.guard(a.preview)))
	mux.HandleFunc("POST /new/create", a.sameOrigin(a.guard(a.create)))
	mux.HandleFunc("GET /push/state", a.guard(a.pushState))
	mux.HandleFunc("POST /push/subscribe", a.sameOrigin(a.guard(a.pushSubscribe)))
	mux.HandleFunc("POST /push/unsubscribe", a.sameOrigin(a.guard(a.pushUnsubscribe)))
	mux.HandleFunc("POST /trip/archive", a.sameOrigin(a.guard(a.archive)))
	mux.HandleFunc("POST /trip/delete", a.sameOrigin(a.guard(a.remove)))
	return securityHeaders(mux)
}

func cacheFS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		h.ServeHTTP(w, r)
	})
}

// Wayfeed: a travel briefing built on the public feedql API. It signs people in with feedql (OAuth, PKCE, public client),
// keeps their tokens in a sealed cookie and has no database of its own.
package main

import (
	"log"
	"net/http"
	"os"
	"time"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	cfg := Config{
		API:       env("FEEDQL_API", "https://api.feedql.com"),
		Base:      env("WAYFEED_BASE_URL", "https://wayfeed.app"),
		ClientID:  env("WAYFEED_CLIENT_ID", "wayfeed"),
		CookieKey: os.Getenv("WAYFEED_COOKIE_KEY"),
	}
	app, err := NewApp(cfg, nil)
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Addr: env("LISTEN_ADDR", "127.0.0.1:8080"), Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second}
	log.Printf("wayfeed listening on %s", srv.Addr)
	log.Fatal(srv.ListenAndServe())
}

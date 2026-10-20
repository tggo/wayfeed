package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoutes(t *testing.T) {
	h := newHandler()
	cases := []struct {
		path, ctype, body string
		code              int
	}{
		{"/", "text/html", "Wayfeed", 200},
		{"/manifest.webmanifest", "application/manifest+json", "Wayfeed", 200},
		{"/robots.txt", "text/plain", "Sitemap: https://wayfeed.app/sitemap.xml", 200},
		{"/sitemap.xml", "application/xml", "https://wayfeed.app/", 200},
		{"/static/icon.svg", "image/svg+xml", "<svg", 200},
		{"/healthz", "", "ok", 200},
		{"/nope", "", "", 404},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != c.code || !strings.Contains(rec.Header().Get("Content-Type"), c.ctype) || !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("%s: code=%d ctype=%q", c.path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
}

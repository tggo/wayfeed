// Wayfeed placeholder: one SSR page, PWA manifest, icon, robots and sitemap.
package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"
)

//go:embed web
var assets embed.FS

func newHandler() http.Handler {
	root, _ := fs.Sub(assets, "web")
	static, _ := fs.Sub(assets, "web/static")
	mux := http.NewServeMux()
	serve := func(path, ctype, cache string) {
		b, err := fs.ReadFile(root, path)
		if err != nil {
			log.Fatal(err)
		}
		mux.HandleFunc("GET "+map[bool]string{true: "/{$}", false: "/" + path}[path == "index.html"], func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ctype)
			w.Header().Set("Cache-Control", cache)
			w.Write(b)
		})
	}
	serve("index.html", "text/html; charset=utf-8", "public, max-age=300")
	serve("manifest.webmanifest", "application/manifest+json", "public, max-age=3600")
	serve("robots.txt", "text/plain; charset=utf-8", "public, max-age=3600")
	serve("sitemap.xml", "application/xml", "public, max-age=3600")
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheFS(http.FileServerFS(static))))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	return mux
}

func cacheFS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		h.ServeHTTP(w, r)
	})
}

func main() {
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	srv := &http.Server{Addr: addr, Handler: newHandler(), ReadHeaderTimeout: 5 * time.Second}
	log.Printf("wayfeed listening on %s", addr)
	log.Fatal(srv.ListenAndServe())
}

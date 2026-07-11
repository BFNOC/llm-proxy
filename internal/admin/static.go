package admin

import (
	"embed"
	"io/fs"
	"net/http"
)

// Static dashboard assets (HTML/CSS/JS). Kept as separate files under static/
// so the admin UI is editable without a multi-thousand-line Go string literal.
//
//go:embed static/*
var embeddedStatic embed.FS

// dashboardHTML is the main admin page shell (loads CSS/JS from /admin/assets/).
var dashboardHTML []byte

// staticAssets is the filesystem rooted at static/ for HTTP serving.
var staticAssets fs.FS

func init() {
	sub, err := fs.Sub(embeddedStatic, "static")
	if err != nil {
		panic("admin: embed static subfs: " + err.Error())
	}
	staticAssets = sub
	b, err := fs.ReadFile(staticAssets, "index.html")
	if err != nil {
		panic("admin: embed index.html: " + err.Error())
	}
	dashboardHTML = b
}

// assetsHandler serves /admin/assets/* from the embedded static directory.
// no-cache so admin UI updates ship immediately after binary redeploy.
func assetsHandler() http.Handler {
	files := http.FileServer(http.FS(staticAssets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		http.StripPrefix("/admin/assets/", files).ServeHTTP(w, r)
	})
}

package admin

import (
	"embed"
	"io/fs"
	"net/http"
)

// Dashboard 静态资源（HTML/CSS/JS）。保留为 static/ 下的独立文件，
// 这样 admin UI 可以编辑，而不需要写成数千行的 Go 字符串字面量。
//
//go:embed static/*
var embeddedStatic embed.FS

// dashboardHTML 是主 admin 页面壳层（从 /admin/assets/ 加载 CSS/JS）。
var dashboardHTML []byte

// staticAssets 是以 static/ 为根目录的文件系统，供 HTTP 服务使用。
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

// assetsHandler 从内嵌的静态目录提供 /admin/assets/* 服务。
// 使用 no-cache，让 admin UI 更新在二进制重新部署后立即生效。
func assetsHandler() http.Handler {
	files := http.FileServer(http.FS(staticAssets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		http.StripPrefix("/admin/assets/", files).ServeHTTP(w, r)
	})
}

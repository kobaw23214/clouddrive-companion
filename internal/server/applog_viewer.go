package server

import (
	_ "embed"
	"net/http"
)

//go:embed applog_viewer.html
var applogViewerHTML []byte

// HandleAppLogViewerPage 浏览器端的应用日志查看器（嵌入静态 HTML）
func HandleAppLogViewerPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(applogViewerHTML)
}

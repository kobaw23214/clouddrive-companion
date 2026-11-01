package tracer

import (
	_ "embed"
	"net/http"
)

//go:embed viewer.html
var viewerHTML []byte

// HandleViewerPage 浏览器端的链路追踪查看器（嵌入静态 HTML）。
//
// 路由：GET /task-traces/viewer
//
// 与 /httplog、/applog 同样模式：单页应用，全部数据通过 /task-traces/* JSON
// 端点拉取。不依赖 Wails 窗口，可在任意浏览器打开。
func HandleViewerPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(viewerHTML)
}

package server

import (
	"clouddrive-companion-withWails2/internal/config"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// ----------------------------------------------------------------------------
// PNA 预检观测记录
// ----------------------------------------------------------------------------
// 浏览器若启用老版 PNA，会在正式请求前对同一 URL 发一个 OPTIONS 预检，
// 并携带请求头 Access-Control-Request-Private-Network: true。
// 我们用 URL 上的 probe=<id> 把"预检"和随后的"正式 GET"关联起来：
//   - OPTIONS 带 PNA 头  -> 记下该 id 的观测时间
//   - 正式 GET           -> 回报该 id 是否被观测到预检
//
// 由此可在报告中区分：
//
//	预检发生  = 浏览器在执行 PNA，且是"服务端放行头"让它通过的
//	预检未发生 = 浏览器对该组合根本未启用拦截（与响应头无关）
var (
	pnaPreflightSeen = make(map[string]int64) // probeID -> 观测到预检的毫秒时间戳
	pnaPreflightMu   sync.Mutex
)

func nowMs() int64 { return time.Now().UnixNano() / 1e6 }

func recordPreflight(id string) {
	if id == "" {
		return
	}
	pnaPreflightMu.Lock()
	pnaPreflightSeen[id] = nowMs()
	// 顺手清理 60s 前的旧记录，避免内存无限增长
	cutoff := nowMs() - 60000
	for k, v := range pnaPreflightSeen {
		if v < cutoff {
			delete(pnaPreflightSeen, k)
		}
	}
	pnaPreflightMu.Unlock()
}

func consumePreflight(id string) (bool, int64) {
	if id == "" {
		return false, 0
	}
	pnaPreflightMu.Lock()
	defer pnaPreflightMu.Unlock()
	t, ok := pnaPreflightSeen[id]
	if !ok {
		return false, 0
	}
	return true, nowMs() - t
}

// ============================================================================
// PNA / LNA 诊断探测端点
// ----------------------------------------------------------------------------
// 用途：配合 testing/pna-test.html，验证"现场公网 HTTP 页面 -> 本地 loopback"
//       是否被浏览器的 Private Network Access / Local Network Access 拦截。
//
// 设计要点：
//  1. 本端点自己完整地处理 CORS + PNA 预检放行头，不复用 server.go 里的
//     通用 corsHandler（后者在 OPTIONS 时直接返回，且不带 PNA 头）。
//  2. 关键头 Access-Control-Allow-Private-Network: true 用于回应老版 PNA
//     预检（Chrome 在预检里带 Access-Control-Request-Private-Network: true）。
//     这样可以把"服务端缺头"这个因素排除掉，让唯一变量只剩"浏览器是否拦截"。
//  3. 端点纯只读、无副作用，常驻开启对生产无影响。
// ============================================================================

// writePNACORS 写入完整的 CORS + PNA 放行响应头。
func writePNACORS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = "*"
	}
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", origin)
	h.Set("Vary", "Origin")
	h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Content-Type")
	// 老版 PNA：服务端需在预检里显式回应此头，否则预检失败。
	h.Set("Access-Control-Allow-Private-Network", "true")
	// 防缓存，确保每次探测都是真实结果。
	h.Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
}

// HandlePNAProbe 探测端点：GET 返回一段 JSON，OPTIONS 完成 PNA 预检放行。
func HandlePNAProbe(w http.ResponseWriter, r *http.Request) {
	writePNACORS(w, r)

	probeID := r.URL.Query().Get("probe")
	pnaReqHeader := r.Header.Get("Access-Control-Request-Private-Network")

	if r.Method == http.MethodOptions {
		// 这是浏览器发来的预检。若带了 PNA 标记，说明浏览器正在执行 PNA 检查。
		if pnaReqHeader != "" {
			recordPreflight(probeID)
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	// 正式 GET：回报此 probeID 是否在之前观测到过 PNA 预检。
	preflightSeen, ageMs := consumePreflight(probeID)

	resp := map[string]interface{}{
		"ok":         true,
		"service":    "clouddrive-companion-pna-probe",
		"serverTime": time.Now().Format(time.RFC3339),
		"port":       config.AppConfig.LocalServerPort,
		"origin":     r.Header.Get("Origin"),
		"method":     r.Method,
		// 是否在本次 GET 之前观测到带 PNA 标记的预检 OPTIONS。
		// true  -> 浏览器在执行 PNA，靠服务端放行头通过（原因B）
		// false -> 未观测到预检，浏览器对该组合未启用拦截（原因A）
		"pnaPreflightObserved": preflightSeen,
		"pnaPreflightAgeMs":    ageMs,
		"probeId":              probeID,
		"sawPNAReqHeaderOnGet": pnaReqHeader, // GET 上通常为空
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

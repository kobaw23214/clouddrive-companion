package api

import (
	"bytes"
	"clouddrive-companion-withWails2/internal/config"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withTestLogger 注册一个收集型 logger 并打开开关，返回收集槽与还原函数
func withTestLogger(t *testing.T) (*[]OutboundLogEntry, func()) {
	t.Helper()
	entries := &[]OutboundLogEntry{}
	prevLogger := outboundLogger
	prevLog := config.AppConfig.OutboundHttpLog
	prevPoller := config.AppConfig.OutboundHttpLogPoller
	SetOutboundLogger(func(e OutboundLogEntry) { *entries = append(*entries, e) })
	config.AppConfig.OutboundHttpLog = true
	config.AppConfig.OutboundHttpLogPoller = true
	return entries, func() {
		outboundLogger = prevLogger
		config.AppConfig.OutboundHttpLog = prevLog
		config.AppConfig.OutboundHttpLogPoller = prevPoller
	}
}

func newLoggingClient() *http.Client {
	return &http.Client{Transport: &loggingTransport{base: http.DefaultTransport}}
}

// 小 urlencoded 请求体：被记录、原样送达服务端、taskId 从 body 解析
func TestLoggingTransport_SmallBodyCapturedAndRestored(t *testing.T) {
	entries, restore := withTestLogger(t)
	defer restore()

	var serverGot string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		serverGot = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	body := "taskId=task-123&foo=bar"
	req, _ := http.NewRequest("POST", srv.URL+"/submit", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := newLoggingClient().Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if serverGot != body {
		t.Errorf("服务端收到的 body 被改变: got %q, want %q", serverGot, body)
	}
	if string(respBody) != `{"ok":true}` {
		t.Errorf("调用方收到的响应被改变: %q", respBody)
	}
	if len(*entries) != 1 {
		t.Fatalf("应记录 1 条日志, got %d", len(*entries))
	}
	e := (*entries)[0]
	if e.TaskID != "task-123" {
		t.Errorf("TaskID 应从 urlencoded body 解析出 task-123, got %q", e.TaskID)
	}
	if e.RequestBody != body {
		t.Errorf("RequestBody = %q, want %q", e.RequestBody, body)
	}
	if e.StatusCode != 200 || e.Peer == "" || e.ResponseBody != `{"ok":true}` {
		t.Errorf("日志字段异常: %+v", e)
	}
}

// context 里的 taskId 优先于 URL query
func TestLoggingTransport_TaskIDPriority(t *testing.T) {
	entries, restore := withTestLogger(t)
	defer restore()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/x?taskId=from-query", nil)
	req = req.WithContext(ContextWithTaskID(context.Background(), "from-ctx"))
	resp, err := newLoggingClient().Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	resp.Body.Close()

	e := (*entries)[0]
	if e.TaskID != "from-ctx" {
		t.Errorf("context 里的 taskId 应优先, got %q", e.TaskID)
	}
}

// multipart 请求体绝不缓冲，只记占位说明，且服务端收到完整内容
func TestLoggingTransport_MultipartSkipped(t *testing.T) {
	entries, restore := withTestLogger(t)
	defer restore()

	var serverGot int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		serverGot = int(n)
	}))
	defer srv.Close()

	buf := new(bytes.Buffer)
	w := multipart.NewWriter(buf)
	w.WriteField("taskId", "task-mp")
	part, _ := w.CreateFormFile("file", "chunk.bin")
	part.Write(bytes.Repeat([]byte("x"), 64*1024))
	w.Close()
	total := buf.Len()

	req, _ := http.NewRequest("POST", srv.URL+"/chunk", buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	// 分片上传的真实接线（uploader/chunk.go）通过 ctx 携带 taskId
	req = req.WithContext(ContextWithTaskID(context.Background(), "task-mp"))

	resp, err := newLoggingClient().Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	resp.Body.Close()

	if serverGot != total {
		t.Errorf("服务端应收到完整 multipart body (%d 字节), got %d", total, serverGot)
	}
	e := (*entries)[0]
	if !strings.HasPrefix(e.RequestBody, "[multipart body omitted") {
		t.Errorf("multipart body 应只记占位说明, got %q", e.RequestBody)
	}
	if e.TaskID != "task-mp" {
		t.Errorf("multipart 不读 body, taskId 应来自 ctx, got %q", e.TaskID)
	}
}

// 超过 256KB 的非 multipart 请求体不读取
func TestLoggingTransport_LargeBodySkipped(t *testing.T) {
	entries, restore := withTestLogger(t)
	defer restore()

	var serverGot int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		serverGot = int(n)
	}))
	defer srv.Close()

	body := bytes.Repeat([]byte("y"), 300*1024)
	req, _ := http.NewRequest("POST", srv.URL+"/big", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := newLoggingClient().Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	resp.Body.Close()

	if serverGot != len(body) {
		t.Errorf("服务端应收到完整 body (%d 字节), got %d", len(body), serverGot)
	}
	e := (*entries)[0]
	if !strings.HasPrefix(e.RequestBody, "[body omitted") {
		t.Errorf("大 body 应记占位说明, got %q", e.RequestBody)
	}
}

// 响应超过 1MB：记录截断 + 截断标志，调用方仍读到完整响应
func TestLoggingTransport_ResponseTruncated(t *testing.T) {
	entries, restore := withTestLogger(t)
	defer restore()

	full := bytes.Repeat([]byte("z"), 2*1024*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(full)
	}))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/big-resp", nil)
	resp, err := newLoggingClient().Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if len(got) != len(full) {
		t.Errorf("调用方应读到完整响应 (%d 字节), got %d", len(full), len(got))
	}
	e := (*entries)[0]
	if !e.ResponseBodyTrunc {
		t.Error("超过 1MB 的响应应置 ResponseBodyTrunc")
	}
	if len(e.ResponseBody) != outboundRespBodyMaxBytes {
		t.Errorf("记录的响应体应截断到 1MB, got %d", len(e.ResponseBody))
	}
}

// 网络错误：StatusCode=0、Error 记录错误串，也要记一条日志
func TestLoggingTransport_NetworkError(t *testing.T) {
	entries, restore := withTestLogger(t)
	defer restore()

	// 起一个立刻关掉的 server，拿到一个保证连不上的地址
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := srv.URL
	srv.Close()

	req, _ := http.NewRequest("GET", deadURL+"/x", nil)
	_, err := newLoggingClient().Do(req)
	if err == nil {
		t.Fatal("预期请求失败")
	}
	if len(*entries) != 1 {
		t.Fatalf("网络错误也应记录 1 条日志, got %d", len(*entries))
	}
	e := (*entries)[0]
	if e.StatusCode != 0 {
		t.Errorf("网络错误时 StatusCode 应为 0, got %d", e.StatusCode)
	}
	if e.Error == "" {
		t.Error("网络错误时 Error 应记录错误串")
	}
}

// 开关行为：未注册 logger / 总开关关闭 → 透传；轮询标记 + 轮询开关关闭 → 不记录
func TestLoggingTransport_Switches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	prevLog := config.AppConfig.OutboundHttpLog
	prevPoller := config.AppConfig.OutboundHttpLogPoller
	prevLogger := outboundLogger
	defer func() {
		config.AppConfig.OutboundHttpLog = prevLog
		config.AppConfig.OutboundHttpLogPoller = prevPoller
		outboundLogger = prevLogger
	}()

	do := func(ctx context.Context) {
		req, _ := http.NewRequest("GET", srv.URL+"/s", nil)
		if ctx != nil {
			req = req.WithContext(ctx)
		}
		resp, err := newLoggingClient().Do(req)
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		resp.Body.Close()
	}

	// 1. logger 未设置 → 零开销透传（不应 panic）
	outboundLogger = nil
	config.AppConfig.OutboundHttpLog = true
	do(nil)

	// 2. 总开关关闭 → 不记录
	entries := &[]OutboundLogEntry{}
	SetOutboundLogger(func(e OutboundLogEntry) { *entries = append(*entries, e) })
	config.AppConfig.OutboundHttpLog = false
	do(nil)
	if len(*entries) != 0 {
		t.Errorf("总开关关闭时不应记录, got %d 条", len(*entries))
	}

	// 3. 轮询标记 + 轮询开关关闭 → 不记录；普通请求仍记录
	config.AppConfig.OutboundHttpLog = true
	config.AppConfig.OutboundHttpLogPoller = false
	do(ContextWithPollerMark(context.Background()))
	if len(*entries) != 0 {
		t.Errorf("轮询开关关闭时轮询请求不应记录, got %d 条", len(*entries))
	}
	do(nil)
	if len(*entries) != 1 {
		t.Errorf("非轮询请求应照常记录, got %d 条", len(*entries))
	}
}

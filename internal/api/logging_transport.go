package api

import (
	"bytes"
	"clouddrive-companion-withWails2/internal/config"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// outboundReqBodyMaxBytes 请求体最多读取的字节数，超过则不读（multipart 一律不读）
	outboundReqBodyMaxBytes = 256 * 1024
	// outboundRespBodyMaxBytes 响应体最多记录的字节数，超过则截断（响应一律读全量再装回）
	outboundRespBodyMaxBytes = 1024 * 1024
)

// OutboundLogEntry 一条出站 HTTP 请求日志（与 server.HTTPRequestLog 对应的出站子集）。
// 通过 SetOutboundLogger 注册的回调交给 server 包落库，避免 api → server 的 import 环。
type OutboundLogEntry struct {
	TimestampMs       int64
	Method            string
	URL               string
	Path              string
	Query             string
	Peer              string
	RequestHeaders    map[string][]string
	RequestBody       string
	RequestBodyTrunc  bool
	StatusCode        int
	ResponseHeaders   map[string][]string
	ResponseBody      string
	ResponseBodyTrunc bool
	DurationMs        int64
	TaskID            string
	Error             string
}

// outboundLogger 出站日志回调，由 main 在日志存储初始化成功后接线；nil 时 RoundTripper 零开销透传
var outboundLogger func(OutboundLogEntry)

// SetOutboundLogger 注册出站日志回调（幂等，启动时调用一次）
func SetOutboundLogger(fn func(OutboundLogEntry)) {
	outboundLogger = fn
}

// 私有 context key，避免与其它包的 WithValue 冲突
type outboundCtxKey int

const (
	ctxKeyTaskID outboundCtxKey = iota
	ctxKeyPoller
)

// ContextWithTaskID 在请求 context 里带上 taskId，供出站日志 RoundTripper 提取
func ContextWithTaskID(ctx context.Context, taskID string) context.Context {
	return context.WithValue(ctx, ctxKeyTaskID, taskID)
}

// ContextWithPollerMark 标记该请求来自命令轮询，是否记录受 OutboundHttpLogPoller 开关单独控制
func ContextWithPollerMark(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeyPoller, true)
}

// loggingTransport 包装底层 RoundTripper，把每次出站请求记录成 OutboundLogEntry。
// 对调用方完全透明：请求体/响应体读后原样装回。
type loggingTransport struct {
	base http.RoundTripper
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	// 未注册回调或总开关关闭 → 直接透传，零开销
	if outboundLogger == nil || !config.AppConfig.OutboundHttpLog {
		return base.RoundTrip(req)
	}
	// 轮询请求有独立开关
	if isPoller, _ := req.Context().Value(ctxKeyPoller).(bool); isPoller && !config.AppConfig.OutboundHttpLogPoller {
		return base.RoundTrip(req)
	}

	start := time.Now()
	entry := OutboundLogEntry{
		TimestampMs:    start.UnixMilli(),
		Method:         req.Method,
		URL:            req.URL.String(),
		Path:           req.URL.Path,
		Query:          req.URL.RawQuery,
		Peer:           req.URL.Host,
		RequestHeaders: cloneHeaders(req.Header),
	}

	// taskId 提取优先级：① request context → ② URL query → ③ urlencoded 小 body
	entry.TaskID, _ = req.Context().Value(ctxKeyTaskID).(string)
	if entry.TaskID == "" {
		entry.TaskID = req.URL.Query().Get("taskId")
	}

	// 请求体：multipart（分片文件）绝不缓冲；其它类型 ≤256KB 才读取并恢复原样
	reqBodyBytes := t.captureRequestBody(req, &entry)
	if entry.TaskID == "" && len(reqBodyBytes) > 0 &&
		strings.HasPrefix(req.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		if vals, err := url.ParseQuery(string(reqBodyBytes)); err == nil {
			entry.TaskID = vals.Get("taskId")
		}
	}

	resp, err := base.RoundTrip(req)
	entry.DurationMs = time.Since(start).Milliseconds()

	if err != nil {
		// 网络错误：StatusCode=0，记录错误串，照常记一条日志
		entry.Error = err.Error()
		outboundLogger(entry)
		return resp, err
	}

	entry.StatusCode = resp.StatusCode
	entry.ResponseHeaders = cloneHeaders(resp.Header)
	// 出站响应都是小 JSON：读全量、记录时截断 1MB、再原样装回，行为对调用方透明
	if resp.Body != nil {
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr == nil {
			if len(body) > outboundRespBodyMaxBytes {
				entry.ResponseBody = string(body[:outboundRespBodyMaxBytes])
				entry.ResponseBodyTrunc = true
			} else {
				entry.ResponseBody = string(body)
			}
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))
	}

	outboundLogger(entry)
	return resp, nil
}

// captureRequestBody 按规则读取请求体并把读过的 body 恢复原样，返回读到的字节（未读时返回 nil）
func (t *loggingTransport) captureRequestBody(req *http.Request, entry *OutboundLogEntry) []byte {
	if req.Body == nil || req.Body == http.NoBody {
		return nil
	}
	// 分片上传的 multipart 表单里带着文件内容，绝不能缓冲，仅记占位说明
	if strings.HasPrefix(req.Header.Get("Content-Type"), "multipart/") {
		entry.RequestBody = fmt.Sprintf("[multipart body omitted, Content-Length: %d]", req.ContentLength)
		return nil
	}
	// 长度未知或超过上限则不读，避免为大请求付出内存拷贝
	if req.ContentLength < 0 || req.ContentLength > outboundReqBodyMaxBytes {
		entry.RequestBody = fmt.Sprintf("[body omitted, Content-Length: %d]", req.ContentLength)
		return nil
	}
	body, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		return nil
	}
	entry.RequestBody = string(body)
	// 恢复原样，让底层 transport 照常发送
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	return body
}

func cloneHeaders(h http.Header) map[string][]string {
	out := make(map[string][]string, len(h))
	for k, v := range h {
		out[k] = append([]string(nil), v...)
	}
	return out
}

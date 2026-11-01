package uploader

import (
	"bytes"
	"clouddrive-companion-withWails2/internal/api"
	"clouddrive-companion-withWails2/internal/config"
	"clouddrive-companion-withWails2/internal/tracer"
	"clouddrive-companion-withWails2/internal/utils"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// uploadFileWithRetry 重试上传文件
//
// parentSpanId 是 upload_file 顶层 span，由 processBatch 创建并传入。每次重试本身不开新的
// upload_file span（保持一对一对应关系），但 uploadFileInChunks 内部的 md5/check_chunks/
// chunk_upload 子 span 会在每次重试中重新创建——这样能从链路里看到具体哪次重试卡在哪步。
func (m *Manager) uploadFileWithRetry(fileInfo utils.FullFileInfo, fileNumber int, parentSpanId string) error {
	var lastErr error
	for retry := 0; retry <= config.AppConfig.MaxRetries; retry++ {
		m.checkPause(fileInfo.RelativePath)

		if retry > 0 {
			log.Printf("重试上传文件 %s (第 %d 次)", fileInfo.RelativePath, retry)
			time.Sleep(time.Duration(retry) * 2 * time.Second)
		}

		err := m.uploadFileInChunks(fileInfo, fileNumber, parentSpanId)
		if err == nil {
			return nil
		}
		lastErr = err

		if ue, ok := lastErr.(*url.Error); ok && ue.Timeout() {
			log.Printf("文件 %s 上传超时 (第 %d/%d 次): %v", fileInfo.RelativePath, retry+1, config.AppConfig.MaxRetries, ue)
		} else if os.IsNotExist(err) || strings.Contains(err.Error(), "文件不存在") {
			log.Printf("文件 %s 不存在,停止重试。", fileInfo.RelativePath)
			break
		} else {
			log.Printf("文件 %s 遇到错误 (第 %d/%d 次): %v", fileInfo.RelativePath, retry+1, config.AppConfig.MaxRetries, err)
		}
	}
	return lastErr
}

// checkCompletedChunks 检查已完成的分片
//
// spanId 是 check_chunks 子 span（由 uploadFileInChunks 在调用前创建），
// 非空时把链路上下文透传到出站请求头（X-Trace-Id / X-Parent-Span-Id）。
func (m *Manager) checkCompletedChunks(filePathInTask, spanId string) (map[int64]bool, int64, error) {
	params := url.Values{}
	params.Set("taskId", m.taskId)
	params.Set("relativePath", filePathInTask)

	checkURL := fmt.Sprintf("%s?%s", config.BackendCheckURL(), params.Encode())

	req, err := http.NewRequest("GET", checkURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("创建分片检查请求失败: %w", err)
	}
	attachTraceHeaders(req, spanId)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(config.AppConfig.HTTPTimeout)*time.Second)
	defer cancel()
	req = req.WithContext(api.ContextWithTaskID(ctx, m.taskId))

	resp, err := api.GlobalHTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("请求分片状态失败: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, 0, fmt.Errorf("检查分片失败，服务器返回 %s: %s", resp.Status, string(body))
	}

	var completedChunks []int64
	if err := json.NewDecoder(resp.Body).Decode(&completedChunks); err != nil {
		return nil, 0, fmt.Errorf("解析分片列表JSON失败: %w", err)
	}

	chunkSet := make(map[int64]bool, len(completedChunks))
	var uploadedBytes int64 = 0

	for _, chunkNum := range completedChunks {
		chunkSet[chunkNum] = true
		uploadedBytes += config.AppConfig.ChunkSize
	}

	if len(chunkSet) > 0 {
		log.Printf("文件 %s 发现 %d 个已上传的分片。", filePathInTask, len(chunkSet))
	}

	return chunkSet, uploadedBytes, nil
}

// uploadFileInChunks 分片上传文件
//
// 链路追踪：在父 upload_file span 下创建三类子 span：
//   - md5_calc：MD5 计算耗时
//   - check_chunks：查询已上传分片的耗时
//   - chunk_upload：每个分片一个 span（受采样器控制）
//
// parentSpanId 为空时（开关关 / softCap）所有 tracer 调用都是 no-op，业务逻辑零侵入。
func (m *Manager) uploadFileInChunks(fileInfo utils.FullFileInfo, fileNumber int, parentSpanId string) error {
	filePathInTask := filepath.ToSlash(fileInfo.RelativePath)
	timestamp := time.Now().UnixMilli()
	fileId := fmt.Sprintf("%s-%d-%d", filePathInTask, fileInfo.FileSize, timestamp)
	var fileMD5 string

	file, err := os.Open(fileInfo.AbsolutePath)
	if err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "The system cannot find") {
			log.Printf("【异常跳过】文件已丢失/被移动: %s (已发送 -1 信号给后端)", fileInfo.RelativePath)

			_, _, _ = m.uploadChunk(nil, fileInfo, 0, 0, fileNumber, fileId, fileMD5, false, "", "")

			return nil
		}

		return fmt.Errorf("无法打开文件: %s, %w", filepath.ToSlash(fileInfo.AbsolutePath), err)
	}

	defer file.Close()

	// ── 链路追踪：md5_calc ──
	md5SpanId := tracer.StartChild(parentSpanId, tracer.StageMD5Calc, fileInfo.RelativePath)
	md5Hash, md5Err := utils.CalculateFileMD5(fileInfo.AbsolutePath)
	if md5Err != nil {
		// MD5 失败按原行为只是不发送，不阻断流程；span 标记失败但继续
		tracer.Fail(md5SpanId, md5Err.Error())
	} else {
		fileMD5 = md5Hash
		tracer.End(md5SpanId)
	}

	fileSize := fileInfo.FileSize
	totalChunks := (fileSize + config.AppConfig.ChunkSize - 1) / config.AppConfig.ChunkSize
	if fileSize == 0 {
		totalChunks = 1
	}

	// ── 链路追踪：check_chunks ──
	chkSpanId := tracer.StartChild(parentSpanId, tracer.StageCheckChunks, fileInfo.RelativePath)
	completedChunksSet, initialBytes, err := m.checkCompletedChunks(fileInfo.RelativePath, chkSpanId)
	if err != nil {
		log.Printf("警告: 无法检查文件 %s 的分片状态: %v。将从头开始上传。", fileInfo.RelativePath, err)
		// 与原行为一致：失败不中断，使用空集合从头开始
		tracer.FailWithExtra(chkSpanId, err.Error(),
			fmt.Sprintf(`{"completedChunks":0,"initialBytes":0}`))
		completedChunksSet = make(map[int64]bool)
		initialBytes = 0
	} else {
		tracer.EndWithExtra(chkSpanId, fmt.Sprintf(
			`{"completedChunks":%d,"initialBytes":%d}`,
			len(completedChunksSet), initialBytes))
	}

	if _, ok := completedChunksSet[totalChunks]; ok && fileSize > 0 {
		lastChunkSize := fileSize % config.AppConfig.ChunkSize
		if lastChunkSize == 0 {
			lastChunkSize = config.AppConfig.ChunkSize
		}
		initialBytes = initialBytes - config.AppConfig.ChunkSize + lastChunkSize
	}

	m.updateFileProgressStatus(fileInfo.RelativePath, "UPLOADING", initialBytes)

	m.getOrCreateFilePauseState(fileInfo.RelativePath)

	// 用于在重试 chunk 时把上一次失败的 spanId 写入 extra.retryOf
	var prevRetrySpanId string

	for i := int64(1); i <= totalChunks; i++ {
		m.checkPause(fileInfo.RelativePath)
		m.checkFilePause(fileInfo.RelativePath)

		select {
		case <-m.ctx.Done():
			return fmt.Errorf("上传被取消")
		default:
		}

		if _, ok := completedChunksSet[i]; ok {
			log.Printf("跳过分片 %d/%d (文件 %s)，服务端已存在", i, totalChunks, fileInfo.RelativePath)
			continue
		}

		isLastChunk := (i == totalChunks)

		// ── 链路追踪：chunk_upload（受采样控制；最后一个分片始终记录以承载 server_merge）──
		chunkSpanId := tracer.StartChunkUpload(parentSpanId, i, totalChunks, fileInfo.RelativePath)

		bytesSent, wroteReqMs, err := m.uploadChunk(file, fileInfo, i, totalChunks, fileNumber, fileId, fileMD5, isLastChunk, parentSpanId, chunkSpanId)

		if err != nil {
			if respErr, ok := err.(interface{ Error() string }); ok && strings.Contains(respErr.Error(), "423") {
				log.Printf("分片 %d/%d (文件 %s) 收到 423 (Paused) 响应，工作线程将等待恢复...", i, totalChunks, fileInfo.RelativePath)

				m.Pause()
				log.Printf("工作线程 (文件 %s) 收到 423，已触发本地 m.Pause()。", fileInfo.RelativePath)

				m.checkPause(fileInfo.RelativePath)
				m.checkFilePause(fileInfo.RelativePath)

				log.Printf("任务 %s 已恢复，重试分片 %d...", fileInfo.RelativePath, i)

				// 423 暂停-恢复也算一次重试：把当前 span 标 fail，记下 spanId 给下一轮 retryOf
				tracer.FailWithExtra(chunkSpanId, "paused-423",
					buildChunkExtra(i, totalChunks, bytesSent, isLastChunk, prevRetrySpanId))
				prevRetrySpanId = chunkSpanId

				i--

				continue
			}

			if ue, ok := err.(*url.Error); ok && ue.Timeout() {
				log.Printf("分片 %d/%d 上传超时: %v", i, totalChunks, ue)
			}
			tracer.FailWithExtra(chunkSpanId, err.Error(),
				buildChunkExtra(i, totalChunks, bytesSent, isLastChunk, prevRetrySpanId))
			return fmt.Errorf("分片 %d/%d 上传失败: %w", i, totalChunks, err)
		}

		tracer.EndAtWithExtra(chunkSpanId,
			buildChunkExtra(i, totalChunks, bytesSent, isLastChunk, prevRetrySpanId),
			wroteReqMs)
		prevRetrySpanId = "" // 成功后重置链

		if v, ok := m.fileProgress.Load(fileInfo.RelativePath); ok {
			fp := v.(FileProgress)
			fp.UploadedSize += bytesSent
			m.updateFileProgressStatus(fileInfo.RelativePath, fp.Status, fp.UploadedSize)
		}
	}
	return nil
}

// buildChunkExtra 拼装 chunk_upload span 的 extra JSON。
//
// retryOf 非空时附加上一次失败的 spanId，便于前端把多次重试串成一条时间线。
func buildChunkExtra(chunkNumber, totalChunks, bytes int64, isLastChunk bool, retryOf string) string {
	if retryOf == "" {
		return fmt.Sprintf(
			`{"chunkNumber":%d,"totalChunks":%d,"bytes":%d,"isLastChunk":%t}`,
			chunkNumber, totalChunks, bytes, isLastChunk,
		)
	}
	return fmt.Sprintf(
		`{"chunkNumber":%d,"totalChunks":%d,"bytes":%d,"isLastChunk":%t,"retryOf":"%s"}`,
		chunkNumber, totalChunks, bytes, isLastChunk, retryOf,
	)
}

// attachTraceHeaders 把当前活跃 span 的链路上下文透传到出站请求头：
//
//	X-Trace-Id        链路 ID（traceId）
//	X-Parent-Span-Id  当前 span ID（后端可据此把入站请求挂到同一链路下）
//
// 用简单的自定义头而非 W3C traceparent（格式太重，本期后端也不消费）。
// spanId 为空或 span 已结束（不在活跃表）时不加头，静默跳过。
func attachTraceHeaders(req *http.Request, spanId string) {
	traceId, ok := tracer.SpanTraceContext(spanId)
	if !ok {
		return
	}
	req.Header.Set("X-Trace-Id", traceId)
	req.Header.Set("X-Parent-Span-Id", spanId)
}

// uploadChunk 上传单个分片
//
// 返回值：(bytesSent, wroteReqMs, err)
//   - bytesSent：本次实际写入到请求体的字节数
//   - wroteReqMs：httptrace.WroteRequest 触发的毫秒时间戳；拿不到时为 0
//     业务侧（uploadFileInChunks）会用它作为 chunk_upload 的"客户端写完"
//     时间，避免把"等服务端合并"那段算到 chunk_upload 时长里
//
// uploadFileSpanId 非空且为最后一个分片时，会通过 httptrace 切出"客户端写完请求 →
// 收到首字节"的服务端处理耗时，作为一条 server_merge 子 span 直接挂在 upload_file
// 下面（与 md5/check/chunks 同层）。
//
// chunkSpanId 是当前分片的 chunk_upload span（由 uploadFileInChunks 在调用前创建），
// 非空时把链路上下文透传到出站请求头（X-Trace-Id / X-Parent-Span-Id），
// 便于后端把入站请求关联到同一条链路。
//
// 即使 httptrace 没拿到 WroteRequest（极少数 transport 行为），也会用 sendStartMs
// 兜底，保证最后一个分片走成功路径时一定有 server_merge 入链。
func (m *Manager) uploadChunk(file *os.File, fileInfo utils.FullFileInfo, chunkNumber, totalChunks int64, fileNumber int, fileId string, fileMD5 string, isLastChunk bool, uploadFileSpanId string, chunkSpanId string) (int64, int64, error) {
	buf := new(bytes.Buffer)
	writer := multipart.NewWriter(buf)

	writer.WriteField("taskId", m.taskId)
	writer.WriteField("taskName", m.taskName)
	writer.WriteField("userId", m.userId)
	writer.WriteField("userName", m.userName)
	writer.WriteField("dir", m.dir)
	writer.WriteField("totalSize", fmt.Sprintf("%d", m.totalSize))
	writer.WriteField("fileId", fileId)
	writer.WriteField("chunkNumber", fmt.Sprintf("%d", chunkNumber))
	writer.WriteField("totalChunks", fmt.Sprintf("%d", totalChunks))
	writer.WriteField("filename", fileInfo.Metadata.FileName)
	writer.WriteField("relativePath", fileInfo.RelativePath)
	writer.WriteField("totalFiles", fmt.Sprintf("%d", atomic.LoadInt64(&Stats.totalFiles)))

	if file == nil {
		writer.WriteField("currentFileNumber", "-1")
		writer.WriteField("chunkNumber", "1")
		writer.WriteField("totalChunks", "1")
		writer.WriteField("fileId", fileId)
		writer.WriteField("fileMD5", "")
		writer.WriteField("absolutePath", fileInfo.AbsolutePath)

		part, _ := writer.CreateFormFile("file", "missing")
		part.Write([]byte{})
		writer.Close()

		req, err := http.NewRequest("POST", config.BackendUploadURL(), buf)
		if err != nil {
			return 0, 0, err
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		attachTraceHeaders(req, chunkSpanId)

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(config.AppConfig.HTTPTimeout)*time.Second)
		defer cancel()
		req = req.WithContext(api.ContextWithTaskID(ctx, m.taskId))

		resp, err := api.GlobalHTTPClient.Do(req)

		if err != nil {
			return 0, 0, err
		}
		defer func() {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()

		log.Printf("文件丢失信号已发送: %s, 响应码: %d", fileInfo.RelativePath, resp.StatusCode)

		return 0, 0, nil
	}

	writer.WriteField("currentFileNumber", fmt.Sprintf("%d", fileNumber))
	writer.WriteField("absolutePath", fileInfo.AbsolutePath)
	writer.WriteField("fileMD5", fileMD5)

	part, err := writer.CreateFormFile("file", fileInfo.Metadata.FileName)
	if err != nil {
		return 0, 0, fmt.Errorf("创建表单失败: %w", err)
	}

	var bytesToCopy int64 = 0
	if fileInfo.FileSize > 0 {
		offset := (chunkNumber - 1) * config.AppConfig.ChunkSize
		bytesToCopy = config.AppConfig.ChunkSize
		if offset+bytesToCopy > fileInfo.FileSize {
			bytesToCopy = fileInfo.FileSize - offset
		}
		_, err = file.Seek(offset, 0)
		if err != nil {
			return 0, 0, fmt.Errorf("文件定位失败: %w", err)
		}

		n, err := io.CopyN(part, file, bytesToCopy)
		if err != nil && err != io.EOF {
			return n, 0, fmt.Errorf("读取文件块失败: %w", err)
		}
		bytesToCopy = n
	}

	writer.Close()

	if isLastChunk {
		m.updateFileProgressStatus(fileInfo.RelativePath, "MERGING", -1)
	}

	req, err := http.NewRequest("POST", config.BackendUploadURL(), buf)
	if err != nil {
		return bytesToCopy, 0, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	attachTraceHeaders(req, chunkSpanId)

	var reqTimeout time.Duration
	if isLastChunk {
		reqTimeout = time.Duration(config.AppConfig.HTTPTimeout*config.AppConfig.MergeTimeoutMultiplier) * time.Second
		log.Printf("发送最后分片 %d/%d (文件 %s), 使用合并超时: %v", chunkNumber, totalChunks, fileInfo.RelativePath, reqTimeout)
	} else {
		reqTimeout = time.Duration(config.AppConfig.HTTPTimeout) * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), reqTimeout)
	defer cancel()

	// 给所有分片都挂 httptrace，把"客户端写完请求 → 收到首字节"切出来：
	//   - WroteRequest         请求体已全部送出（含 multipart 结尾、TLS flush）
	//   - GotFirstResponseByte 服务端开始返回响应
	//
	// 用途：
	//   1) 所有分片的 chunk_upload span 都用 wroteReqMs 作为结束时间（更精确：
	//      避免把"等服务端"那段算到 chunk_upload 时长里）；
	//   2) 最后一个分片额外用这两个时间戳生成 server_merge 子 span，
	//      切出"等服务端合并"耗时。
	//
	// 容错：极少数 transport 实现可能不触发 WroteRequest（例如代理或自定义 RoundTripper），
	// 此时 wroteReqMs 保持 0，外层退化用 time.Now() 作为 chunk_upload 结束时间。
	var wroteReqMs, firstByteMs int64
	sendStartMs := time.Now().UnixMilli()
	ct := &httptrace.ClientTrace{
		WroteRequest: func(httptrace.WroteRequestInfo) {
			atomic.StoreInt64(&wroteReqMs, time.Now().UnixMilli())
		},
		GotFirstResponseByte: func() {
			atomic.StoreInt64(&firstByteMs, time.Now().UnixMilli())
		},
	}
	ctx = httptrace.WithClientTrace(ctx, ct)

	req = req.WithContext(api.ContextWithTaskID(ctx, m.taskId))

	resp, err := api.GlobalHTTPClient.Do(req)
	if err != nil {
		return bytesToCopy, atomic.LoadInt64(&wroteReqMs), fmt.Errorf("发送请求失败: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return bytesToCopy, atomic.LoadInt64(&wroteReqMs), fmt.Errorf("服务器返回错误 %s: %s", resp.Status, string(body))
	}

	// 最后一个分片：尝试从响应体里读服务端实测的 mergeStartMs/mergeEndMs；
	// 拿到则用服务端时间，否则下面 fallback 到 httptrace 估算。
	var serverMergeStartMs, serverMergeEndMs int64
	if isLastChunk && uploadFileSpanId != "" {
		var lastChunkResp struct {
			Message      string `json:"message"`
			MergeStartMs int64  `json:"mergeStartMs"`
			MergeEndMs   int64  `json:"mergeEndMs"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&lastChunkResp); err == nil {
			if lastChunkResp.MergeStartMs > 0 && lastChunkResp.MergeEndMs >= lastChunkResp.MergeStartMs {
				serverMergeStartMs = lastChunkResp.MergeStartMs
				serverMergeEndMs = lastChunkResp.MergeEndMs
			}
		}
	}

	io.Copy(io.Discard, resp.Body)

	// 把"服务端合并"作为 server_merge 子 span 入链（仅最后一个分片，且仅当父 upload_file 还活跃时）。
	// span 直接挂在 upload_file 下，与 md5/check/chunks 同层，子阶段视图能直接看到。
	//
	// 数据来源优先级：
	//   1) 服务端响应体里带的 mergeStartMs/mergeEndMs（最精确，纯服务端处理时长）
	//   2) httptrace 切出的"客户端写完 → 收到首字节"差值（含网络 RTT）
	//   3) sendStartMs → now 兜底（最坏情况）
	if isLastChunk && uploadFileSpanId != "" {
		var startMs, endMs int64
		source := "fallback"
		switch {
		case serverMergeStartMs > 0:
			startMs, endMs = serverMergeStartMs, serverMergeEndMs
			source = "server"
		default:
			w := atomic.LoadInt64(&wroteReqMs)
			fb := atomic.LoadInt64(&firstByteMs)
			if w == 0 {
				w = sendStartMs
			}
			if fb < w {
				fb = time.Now().UnixMilli()
			}
			startMs, endMs = w, fb
			if atomic.LoadInt64(&wroteReqMs) > 0 && atomic.LoadInt64(&firstByteMs) > 0 {
				source = "httptrace"
			}
		}
		extra := fmt.Sprintf(
			`{"chunkNumber":%d,"totalChunks":%d,"isLastChunk":true,"source":"%s"}`,
			chunkNumber, totalChunks, source,
		)
		tracer.RecordSpan(uploadFileSpanId, tracer.StageServerMerge, fileInfo.RelativePath,
			startMs, endMs, true, "", extra)
		log.Printf("[tracer] server_merge 已记录 文件=%s 分片=%d/%d 起=%d 终=%d 耗时=%dms 来源=%s",
			fileInfo.RelativePath, chunkNumber, totalChunks, startMs, endMs, endMs-startMs, source)
	}

	return bytesToCopy, atomic.LoadInt64(&wroteReqMs), nil
}

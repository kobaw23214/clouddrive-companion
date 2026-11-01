package uploader

import (
	"clouddrive-companion-withWails2/internal/api"
	"clouddrive-companion-withWails2/internal/config"
	"clouddrive-companion-withWails2/internal/tracer"
	"clouddrive-companion-withWails2/internal/utils"
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	Stats         = &UploadStats{}
	ActiveUploads = &sync.Map{}
)

// SetTotalFiles 设置总文件数
func (s *UploadStats) SetTotalFiles(count int64) {
	atomic.StoreInt64(&s.totalFiles, count)
}

// SetCompletedFiles 设置已完成文件数
func (s *UploadStats) SetCompletedFiles(count int64) {
	atomic.StoreInt64(&s.completedFiles, count)
}

// SetTotalBytes 设置总字节数
func (s *UploadStats) SetTotalBytes(bytes int64) {
	atomic.StoreInt64(&s.totalBytes, bytes)
}

// SetUploadedBytes 设置已上传字节数
func (s *UploadStats) SetUploadedBytes(bytes int64) {
	atomic.StoreInt64(&s.uploadedBytes, bytes)
}

// SetStartTime 设置开始时间
func (s *UploadStats) SetStartTime(t time.Time) {
	s.startTime = t
}

// NewManager 创建新的上传管理器
func NewManager(taskInfo TaskInfo, pendingFiles []utils.FullFileInfo, totalSize int64, manifestDir string, allFiles []FileProgress) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	resumeSignal := make(chan bool)
	close(resumeSignal)

	fpMap := &sync.Map{}
	for _, fp := range allFiles {
		fpCopy := fp
		fpMap.Store(fp.RelativePath, fpCopy)
	}

	return &Manager{
		taskId:    taskInfo.TaskId,
		taskName:  taskInfo.TaskName,
		userId:    taskInfo.UserId,
		userName:  taskInfo.UserName,
		dir:       taskInfo.Dir,
		totalSize: totalSize,
		files:     pendingFiles,
		semaphore: make(chan struct{}, config.AppConfig.MaxWorkers),
		errorChan: make(chan error, len(pendingFiles)),
		ctx:       ctx,
		cancel:    cancel,

		resumeSignal:    resumeSignal,
		taskManifestDir: manifestDir,

		allTaskFiles: allFiles,
		fileProgress: fpMap,

		// 透传扫描 trace 信息，Start() 会据此把扫描 trace 并入本次 upload trace。
		scanTraceId: taskInfo.ScanTraceId,
		scanSpanId:  taskInfo.ScanSpanId,
	}
}

// SetCompleted 设置已完成数量
func (m *Manager) SetCompleted(count int32) {
	atomic.StoreInt32(&m.completed, count)
}

// SetTotalUploadedBytes 设置总上传字节数
func (m *Manager) SetTotalUploadedBytes(bytes int64) {
	m.totalUploadedBytes.Store(bytes)
}

// GetTaskId 获取任务ID
func (m *Manager) GetTaskId() string {
	return m.taskId
}

// Pause 暂停上传
func (m *Manager) Pause() {
	m.pauseMutex.Lock()
	defer m.pauseMutex.Unlock()

	if m.isPaused.CompareAndSwap(false, true) {
		log.Println("Pausing upload...")
		m.resumeSignal = make(chan bool)
		m.fileProgress.Range(func(key, value interface{}) bool {
			fp := value.(FileProgress)
			if fp.Status == "UPLOADING" || fp.Status == "MERGING" {
				fp.Status = "PAUSING"
				m.fileProgress.Store(key, fp)
			} else if fp.Status == "QUEUED" {
				fp.Status = "PAUSED"
				m.fileProgress.Store(key, fp)
			}
			return true
		})
	}
}

// Resume 恢复上传
func (m *Manager) Resume() {
	m.pauseMutex.Lock()
	defer m.pauseMutex.Unlock()

	if m.isPaused.CompareAndSwap(true, false) {
		log.Println("Resuming upload...")

		m.fileProgress.Range(func(key, value interface{}) bool {
			fp := value.(FileProgress)
			if fp.Status == "PAUSED" || fp.Status == "PAUSING" {
				fp.Status = "QUEUED"
				m.fileProgress.Store(key, fp)
			}
			return true
		})

		close(m.resumeSignal)
	}
}

// Stop 中止上传任务（用于"删除暂停中的任务"等场景）。
//
// 与 Pause 的区别：Pause 只是让 worker 挂起等待 resumeSignal，任务仍然活跃；
// Stop 会取消 ctx 让所有 worker 尽快退出，Manager.Start 随之返回，
// StartUploadProcess 的 defer 会把任务从 ActiveUploads 移除并清空
// GlobalUploadManager。
//
// 关键点：必须先把处于暂停等待中的 worker 唤醒，否则它们阻塞在
// <-resumeSignal 上，永远看不到 ctx.Done()，Start 也就无法返回。
// 这里复用 Resume / ResumeFile，两者都是 CompareAndSwap 保护的，
// 未处于暂停态时调用无副作用，可安全重复调用。
func (m *Manager) Stop() {
	log.Printf("Stopping upload task %s ...", m.taskId)

	// 1. 先取消 ctx，保证被唤醒的 worker 立刻走 ctx.Done 分支而不是继续上传
	if m.cancel != nil {
		m.cancel()
	}

	// 2. 唤醒全局暂停等待
	m.Resume()

	// 3. 唤醒所有文件级暂停等待
	m.filePauseStates.Range(func(key, value interface{}) bool {
		if relativePath, ok := key.(string); ok {
			m.ResumeFile(relativePath)
		}
		return true
	})
}

// checkPause 检查是否需要暂停
func (m *Manager) checkPause(relativePath string) {
	if !m.isPaused.Load() {
		return
	}

	log.Println("...Upload worker paused, waiting for resume...")

	m.updateFileProgressStatus(relativePath, "PAUSED", -1)

	m.pauseMutex.Lock()
	signal := m.resumeSignal
	m.pauseMutex.Unlock()

	<-signal

	log.Println("...Upload worker resumed.")
}

// getOrCreateFilePauseState 获取或创建文件暂停状态
func (m *Manager) getOrCreateFilePauseState(relativePath string) *filePauseState {
	if state, ok := m.filePauseStates.Load(relativePath); ok {
		return state.(*filePauseState)
	}

	resumeSignal := make(chan bool)
	close(resumeSignal)

	state := &filePauseState{
		resumeSignal: resumeSignal,
	}

	m.filePauseStates.Store(relativePath, state)
	return state
}

// checkFilePause 检查文件是否需要暂停
func (m *Manager) checkFilePause(relativePath string) {
	state := m.getOrCreateFilePauseState(relativePath)

	if !state.isPaused.Load() {
		return
	}

	m.updateFileProgressStatus(relativePath, "PAUSED", -1)

	log.Printf("...File %s paused, waiting for resume...", relativePath)

	state.pauseMutex.Lock()
	signal := state.resumeSignal
	state.pauseMutex.Unlock()

	<-signal

	log.Printf("...File %s resumed.", relativePath)
	m.updateFileProgressStatus(relativePath, "UPLOADING", -1)
}

// PauseFile 暂停单个文件
func (m *Manager) PauseFile(relativePath string) {
	state := m.getOrCreateFilePauseState(relativePath)

	state.pauseMutex.Lock()
	defer state.pauseMutex.Unlock()

	if state.isPaused.CompareAndSwap(false, true) {
		log.Printf("Pausing file: %s", relativePath)
		state.resumeSignal = make(chan bool)
		m.updateFileProgressStatus(relativePath, "PAUSED", -1)
	}
}

// ResumeFile 恢复单个文件
func (m *Manager) ResumeFile(relativePath string) {
	state := m.getOrCreateFilePauseState(relativePath)

	state.pauseMutex.Lock()
	defer state.pauseMutex.Unlock()

	if state.isPaused.CompareAndSwap(true, false) {
		log.Printf("Resuming file: %s", relativePath)
		close(state.resumeSignal)
		m.updateFileProgressStatus(relativePath, "UPLOADING", -1)
	}
}

// updateFileProgressStatus 更新文件进度状态
func (m *Manager) updateFileProgressStatus(relativePath string, status string, uploadedSize int64) {
	if v, ok := m.fileProgress.Load(relativePath); ok {
		fp := v.(FileProgress)
		fp.Status = status
		if uploadedSize != -1 {
			fp.UploadedSize = uploadedSize
		}
		if fp.TotalSize > 0 {
			fp.Progress = float64(fp.UploadedSize) / float64(fp.TotalSize) * 100
		} else if fp.Status == "COMPLETED" {
			fp.Progress = 100
		}
		m.fileProgress.Store(relativePath, fp)
	}
}

// GetTaskProgress 获取任务进度
func (m *Manager) GetTaskProgress() TaskProgress {
	m.pauseMutex.Lock()
	defer m.pauseMutex.Unlock()

	var totalUploaded int64
	var completedCount int
	var failedCount int

	for _, templateFp := range m.allTaskFiles {
		if v, ok := m.fileProgress.Load(templateFp.RelativePath); ok {
			fp := v.(FileProgress)
			totalUploaded += fp.UploadedSize
			if fp.Status == "COMPLETED" {
				completedCount++
			} else if fp.Status == "FAILED" {
				failedCount++
			}
		}
	}

	m.totalUploadedBytes.Store(totalUploaded)

	progress := 0.0
	if m.totalSize > 0 {
		progress = float64(totalUploaded) / float64(m.totalSize) * 100
	}

	status := "UPLOADING"
	if m.isPaused.Load() {
		status = "PAUSED"
	}

	return TaskProgress{
		TaskId:         m.taskId,
		TaskName:       m.taskName,
		Status:         status,
		TotalSize:      m.totalSize,
		UploadedSize:   totalUploaded,
		TotalFiles:     len(m.allTaskFiles),
		CompletedFiles: completedCount,
		FailedFiles:    failedCount,
		Progress:       progress,
	}
}

// BuildHistorySnapshot 把当前 manager 的状态打包成一条 HistoryRecord。
//
// 调用时机：StartUploadProcess 在判断 success 后、删除 manifest 之前。
// 之所以放在 manager 上做而不是在外部组装：m.fileProgress 是 sync.Map，外部
// 难以一次性拿到准确快照，且 m.allTaskFiles 才是"任务真正包含哪些文件"的
// 权威来源。
//
// finalStatus 由调用方传入：完整成功 → "COMPLETED"，部分失败 → "PARTIAL_COMPLETED"。
// 我们不在 manager 内部判断，因为 server 包对"是否所有文件都被尝试过"的语义
// 把控更全（比如 cancelled vs 真失败 vs 全完成）。
func (m *Manager) BuildHistorySnapshot(finalStatus string) HistoryRecord {
	m.pauseMutex.Lock()
	defer m.pauseMutex.Unlock()

	files := make([]FileProgress, 0, len(m.allTaskFiles))
	var totalUploaded int64
	var completedCount, failedCount int
	for _, templateFp := range m.allTaskFiles {
		fp := templateFp
		if v, ok := m.fileProgress.Load(templateFp.RelativePath); ok {
			fp = v.(FileProgress)
		}
		files = append(files, fp)
		totalUploaded += fp.UploadedSize
		if fp.Status == "COMPLETED" {
			completedCount++
		} else if fp.Status == "FAILED" {
			failedCount++
		}
	}

	return HistoryRecord{
		TaskId:         m.taskId,
		TaskName:       m.taskName,
		UserId:         m.userId,
		UserName:       m.userName,
		Dir:            m.dir,
		Status:         finalStatus,
		TotalSize:      m.totalSize,
		UploadedSize:   totalUploaded,
		TotalFiles:     len(m.allTaskFiles),
		CompletedFiles: completedCount,
		FailedFiles:    failedCount,
		Files:          files,
		TraceId:        m.traceId,
	}
}

// Start 开始上传
//
// 链路追踪：在函数入口生成新的 traceId/runSeq 并创建 upload_task 顶层 span，
// 退出时根据 m.failed 决定 End 还是 Fail。函数返回前会包装 NotifyBackendTaskComplete
// 调用并埋 notify_complete 子 span（成功路径），把"通知后端"的耗时也纳入链路。
//
// 如果 m.scanTraceId 非空（扫描后立即上传的常见路径），会复用扫描 trace 的
// traceId，并在 upload_task 顶层 span 创建之前把扫描那边的所有 span（含
// file_scan + metadata_send 子树）一并 RetagTrace 改成 upload 类型。
// 最终前端 trace 列表只显示 1 条"文件上传"，里面包含完整时间轴。
func (m *Manager) Start() bool {
	totalFiles := atomic.LoadInt64(&Stats.totalFiles)

	// ── 链路追踪：开启 upload_task ──
	// 估算 span 数：每文件 ≈ 3（upload_file + md5 + check_chunks） + ceil(size/chunkSize) 个 chunk_upload
	estimated := estimateSpanCount(m.files)

	if m.scanTraceId != "" {
		// 扫描 → 上传并入路径：复用扫描 traceId，先把扫描那边的 span 全改成 upload 类型。
		// runSeq 仍然按 taskId 分配下一个，避免和重复执行的 upload 冲突。
		m.traceId = m.scanTraceId
		_, m.runSeq = tracer.NewTraceId(m.taskId)
		if err := tracer.RetagTrace(m.traceId, tracer.TraceTypeUpload, m.taskId, m.runSeq); err != nil {
			log.Printf("[tracer] 并入扫描 trace 失败 (trace=%s task=%s): %v",
				m.traceId, m.taskId, err)
		}
	} else {
		// 续传 / 没有匹配到扫描 trace：走原行为新建 trace。
		m.traceId, m.runSeq = tracer.NewTraceId(m.taskId)
	}
	tracer.PrepareSampling(m.traceId, estimated)
	m.taskSpanId = tracer.Start(m.traceId, m.taskId, m.runSeq,
		tracer.TraceTypeUpload, tracer.StageUploadTask, m.taskName)

	go m.monitor()

	batchSize := config.AppConfig.MaxFilesInBatch
	if batchSize <= 0 {
		batchSize = 1000
	}
	if batchSize > len(m.files) {
		batchSize = len(m.files)
	}

	cancelled := false
	for i := 0; i < len(m.files); i += batchSize {
		end := i + batchSize
		if end > len(m.files) {
			end = len(m.files)
		}
		batch := m.files[i:end]
		m.processBatch(batch)

		select {
		case <-m.ctx.Done():
			log.Printf("上传被中止,已完成 %d/%d", atomic.LoadInt32(&m.completed), totalFiles)
			cancelled = true
			break
		default:
		}
		if cancelled {
			break
		}
	}
	m.wg.Wait()

	failed := atomic.LoadInt32(&m.failed)
	completed := atomic.LoadInt32(&m.completed)
	allOk := failed == 0 && !cancelled

	// 通知后端合并：只在全部成功且没被取消时才通知（与原行为一致，搬迁自 server.StartUploadProcess）
	if allOk {
		m.notifyBackendComplete()
	}

	// ── 链路追踪：收尾 upload_task ──
	extra := fmt.Sprintf(
		`{"totalFiles":%d,"completed":%d,"failed":%d,"totalBytes":%d}`,
		totalFiles, completed, failed, m.totalSize,
	)
	switch {
	case cancelled:
		tracer.FailWithExtra(m.taskSpanId, "cancelled", extra)
	case failed > 0:
		tracer.FailWithExtra(m.taskSpanId, "部分文件上传失败", extra)
	default:
		tracer.EndWithExtra(m.taskSpanId, extra)
	}
	tracer.CleanupTrace(m.traceId)

	return allOk
}

// notifyBackendComplete 通知后端合并并埋 notify_complete 子 span。
//
// 该逻辑原本位于 server.StartUploadProcess，迁回 Manager 内部以便：
//  1. 让 tracer 的埋点完全封装在 uploader 包（server 包不依赖 tracer）
//  2. 通知耗时纳入 upload_task 的总时长
func (m *Manager) notifyBackendComplete() {
	notifySpan := tracer.StartChild(m.taskSpanId, tracer.StageNotifyComplete, "")
	if err := api.NotifyBackendTaskComplete(m.taskId); err != nil {
		log.Printf("Warning: Failed to notify backend about task completion: %v", err)
		tracer.Fail(notifySpan, err.Error())
		return
	}
	tracer.End(notifySpan)
}

// estimateSpanCount 估算一次任务的总 span 数，给 sampler 决定采样间隔用。
//
// 公式：upload_task + notify_complete + Σfiles(upload_file + md5_calc + check_chunks + ceil(size/chunkSize))
func estimateSpanCount(files []utils.FullFileInfo) int {
	var total int64 = 2 // upload_task + notify_complete
	chunkSize := config.AppConfig.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 5 * 1024 * 1024
	}
	for _, f := range files {
		var chunks int64 = 1
		if f.FileSize > 0 {
			chunks = (f.FileSize + chunkSize - 1) / chunkSize
		}
		total += 3 + chunks // upload_file + md5_calc + check_chunks + N × chunk_upload
	}
	if total > int64(^uint(0)>>1) { // overflow 防御
		return int(^uint(0) >> 1)
	}
	return int(total)
}

// GetTaskSpanId 返回 upload_task 的顶层 span ID，供其他业务模块（chunk.go 等）创建子 span。
func (m *Manager) GetTaskSpanId() string {
	return m.taskSpanId
}

// processBatch 处理批次
//
// 链路追踪：每个文件 goroutine 起点创建 upload_file 子 span（挂在 m.taskSpanId 下），
// 把 fileSpanId 透传给 uploadFileWithRetry 让其内部继续创建 md5/check_chunks/chunk_upload 子 span。
func (m *Manager) processBatch(batch []utils.FullFileInfo) {
	for _, fileInfo := range batch {
		m.semaphore <- struct{}{}
		m.wg.Add(1)

		fileNumber := fileInfo.Metadata.Index + 1

		go func(file utils.FullFileInfo, fileNumber int) {
			defer func() {
				m.filePauseStates.Delete(file.RelativePath)
				<-m.semaphore
				m.wg.Done()
			}()

			m.checkPause(fileInfo.RelativePath)

			// 链路追踪：upload_file span（挂在任务顶层 span 下）
			fileSpanId := tracer.StartChild(m.taskSpanId, tracer.StageUploadFile, file.RelativePath)

			select {
			case <-m.ctx.Done():
				// 取消时把已经开启的 fileSpan 标记为 cancelled，避免活跃表泄漏
				tracer.Fail(fileSpanId, "cancelled")
				return
			default:
				m.updateFileProgressStatus(file.RelativePath, "UPLOADING", -1)
				err := m.uploadFileWithRetry(file, fileNumber, fileSpanId)
				if err != nil {
					atomic.AddInt32(&m.failed, 1)
					m.errorChan <- err
					m.updateFileProgressStatus(file.RelativePath, "FAILED", -1)
					if logErr := m.logFileFailed(file.RelativePath); logErr != nil {
						log.Printf("!!! 严重磁盘错误: 无法记录文件 %s 的失败状态: %v", file.RelativePath, logErr)
					}
					log.Printf("文件 %s 上传失败: %v", file.RelativePath, err)
					tracer.Fail(fileSpanId, err.Error())
				} else {
					completedCount := atomic.AddInt32(&m.completed, 1)
					if err := m.logFileCompleted(file.RelativePath); err != nil {
						log.Printf("!!! 严重磁盘错误: 无法记录文件 %s 的完成状态: %v", file.RelativePath, err)
					}
					m.updateFileProgressStatus(file.RelativePath, "COMPLETED", file.FileSize)

					log.Printf("上传成功 (%d, %d-成功, %d-失败)",
						atomic.LoadInt64(&Stats.totalFiles), completedCount, atomic.LoadInt32(&m.failed))
					tracer.End(fileSpanId)
				}
			}
		}(fileInfo, fileNumber)
	}
}

// monitor 监控上传进度
func (m *Manager) monitor() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			if m.isPaused.Load() {
				log.Println("上传已暂停...")
				continue
			}

			var completedCount int
			m.fileProgress.Range(func(key, value interface{}) bool {
				fp := value.(FileProgress)
				if fp.Status == "COMPLETED" {
					completedCount++
				}
				return true
			})

			failed := atomic.LoadInt32(&m.failed)
			elapsed := time.Since(Stats.startTime)
			if elapsed.Seconds() < 1 {
				continue
			}
			log.Printf("上传进度: 完成=%d/%d, 失败=%d, 耗时=%v",
				completedCount, len(m.allTaskFiles), failed, elapsed.Round(time.Second))
		}
	}
}

// LogFinalStats 记录最终统计信息
func (m *Manager) LogFinalStats() {
	elapsed := time.Since(Stats.startTime)

	var completedCount int
	m.fileProgress.Range(func(key, value interface{}) bool {
		fp := value.(FileProgress)
		if fp.Status == "COMPLETED" {
			completedCount++
		}
		return true
	})

	failed := atomic.LoadInt32(&m.failed)

	log.Println("================ 任务统计 ================")
	log.Printf("任务结束。总耗时: %v", elapsed.Round(time.Second))
	log.Printf("文件统计: 成功 %d, 失败 %d, 总计 %d", completedCount, failed, len(m.allTaskFiles))
	log.Println("========================================")
}

// GetFilesPage 分页获取文件列表（用于前端懒加载）
func (m *Manager) GetFilesPage(offset, limit int, sortKey, sortDir, statusFilter string) PagedFileResult {
	// 构建完整文件列表快照
	allFiles := make([]FileProgress, 0, len(m.allTaskFiles))
	for _, templateFp := range m.allTaskFiles {
		if v, ok := m.fileProgress.Load(templateFp.RelativePath); ok {
			fp := v.(FileProgress)
			allFiles = append(allFiles, fp)
		} else {
			allFiles = append(allFiles, templateFp)
		}
	}

	// 按状态过滤
	if statusFilter != "" && statusFilter != "ALL" {
		filtered := make([]FileProgress, 0)
		for _, f := range allFiles {
			if f.Status == statusFilter {
				filtered = append(filtered, f)
			}
		}
		allFiles = filtered
	}

	// 排序
	SortFiles(allFiles, sortKey, sortDir)

	total := len(allFiles)

	// 分页
	if offset >= total {
		return PagedFileResult{Files: []FileProgress{}, Total: total}
	}
	end := offset + limit
	if end > total {
		end = total
	}

	return PagedFileResult{Files: allFiles[offset:end], Total: total}
}

// SortFiles 对文件列表排序
func SortFiles(files []FileProgress, sortKey, sortDir string) {
	statusOrder := func(s string) int {
		switch s {
		case "FAILED":
			return 0
		case "UPLOADING":
			return 1
		case "MERGING":
			return 2
		case "PAUSED":
			return 3
		case "QUEUED":
			return 4
		case "COMPLETED":
			return 5
		default:
			return 6
		}
	}

	sort.Slice(files, func(i, j int) bool {
		var less bool
		switch sortKey {
		case "fileName":
			less = strings.ToLower(files[i].FileName) < strings.ToLower(files[j].FileName)
		case "relativePath":
			less = strings.ToLower(files[i].RelativePath) < strings.ToLower(files[j].RelativePath)
		case "progress":
			less = files[i].Progress < files[j].Progress
		case "status":
			less = statusOrder(files[i].Status) < statusOrder(files[j].Status)
		default:
			// 默认按状态排序
			less = statusOrder(files[i].Status) < statusOrder(files[j].Status)
		}
		if sortDir == "desc" {
			return !less
		}
		return less
	})
}

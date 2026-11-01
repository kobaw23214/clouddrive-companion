package server

import (
	"clouddrive-companion-withWails2/internal/tracer"
	"clouddrive-companion-withWails2/internal/uploader"
	"clouddrive-companion-withWails2/internal/utils"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// TaskSummary 任务摘要
type TaskSummary struct {
	TaskId         string `json:"taskId"`
	TaskName       string `json:"taskName"`
	TotalFiles     int    `json:"totalFiles"`
	CompletedFiles int    `json:"completedFiles"`
	TotalSize      int64  `json:"totalSize"`
	UploadedSize   int64  `json:"uploadedSize"`
	Dir            string `json:"dir"`
	UserId         string `json:"userId"`
	UserName       string `json:"userName"`
}

// ResumeResponse 恢复响应
type ResumeResponse struct {
	AllFiles       []utils.FullFileInfo `json:"allFiles"`
	CompletedPaths []string             `json:"completedPaths"`
}

// HandleAppLogList 列出应用日志文件（供浏览器调用）
func HandleAppLogList(w http.ResponseWriter, r *http.Request) {
	files := ListAppLogFiles()
	if files == nil {
		files = []AppLogFileInfo{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}

// HandleAppLogRead 读取指定日志文件（供浏览器调用）
//
//	查询参数: name=<file>&offset=<bytes>&size=<lines>&keyword=<text>
func HandleAppLogRead(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	offsetStr := r.URL.Query().Get("offset")
	sizeStr := r.URL.Query().Get("size")
	keyword := r.URL.Query().Get("keyword")

	var offset int64
	if v, err := strconv.ParseInt(offsetStr, 10, 64); err == nil {
		offset = v
	}
	size := 200
	if v, err := strconv.Atoi(sizeStr); err == nil && v > 0 {
		size = v
	}

	page, err := ReadAppLogFile(name, offset, size, keyword)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(page)
}

// HandleAppLogDelete 处理删除应用日志的请求
func HandleAppLogDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var names []string
	if err := json.NewDecoder(r.Body).Decode(&names); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if err := DeleteAppLogFiles(names); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// HandleGetScanStatus 返回当前扫描会话状态（立即返回，浏览器轮询用）
func HandleGetScanStatus(w http.ResponseWriter, r *http.Request) {
	session := GetScanSession()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(session)
}

// HandleGetMetadata 处理获取元数据请求
//
// 根据扫描会话状态立即返回，不再阻塞：
//   - completed:   200 + 元数据 JSON 数组，并把会话标记为 consumed
//   - scanning:    202 Accepted（扫描进行中，浏览器应继续轮询 /scan-status）
//   - cancelled:   404（用户取消）
//   - error:       500 + 错误信息
//   - idle/consumed: 404（无可用结果）
//
// 链路追踪：completed 分支会补一条 metadata_send span，时间窗 = 进入本 handler →
// 写完响应。这段差值通常很短（毫秒级），用户后续如果点上传 → RetagTrace 会把它
// 一并改成 upload trace 类型，前端能在合并后的时间轴里看到这一段。
func HandleGetMetadata(w http.ResponseWriter, r *http.Request) {
	startMs := time.Now().UnixMilli()
	session := GetScanSession()

	switch session.Status {
	case ScanStatusCompleted:
		// 进入读临界区
		SessionMutex.RLock()
		metadataList := make([]utils.FileMetadata, len(CurrentFileList))
		for i, info := range CurrentFileList {
			metadataList[i] = info.Metadata
		}
		SessionMutex.RUnlock()

		// 标记为已被浏览器拉走，避免重复触发上传准备流程
		MarkScanConsumed()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(metadataList)
		log.Printf("Sent metadata for %d files to frontend.", len(metadataList))

		// 仅在能拿到 scan trace 的情况下记录；记录时 traceType 仍为 scan，
		// 之后用户点上传会走 RetagTrace 把整条 trace 一并改写为 upload。
		// parentSpanId 留空 → metadata_send 是顶层 span，与 file_scan / upload_task
		// 在 trace 时间轴上并列，符合"三段同级"的 UX 设计。
		if session.TraceId != "" {
			tracer.RecordSpanWithTrace(
				session.TraceId, "", 1, tracer.TraceTypeScan,
				"", tracer.StageMetadataSend, "",
				startMs, time.Now().UnixMilli(),
				true, "",
				fmt.Sprintf(`{"fileCount":%d}`, len(metadataList)))
		}

	case ScanStatusScanning:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(session)

	case ScanStatusCancelled:
		http.Error(w, "User cancelled file selection", http.StatusNotFound)

	case ScanStatusError:
		http.Error(w, session.ErrorMessage, http.StatusInternalServerError)

	default:
		// idle / consumed
		http.Error(w, "No file list available", http.StatusNotFound)
	}
}

// HandleGetPendingTasks 处理获取待处理任务请求
func HandleGetPendingTasks(w http.ResponseWriter, r *http.Request) {
	log.Println("Frontend requested pending task list.")
	var pendingTasks []TaskSummary

	files, err := os.ReadDir(uploader.TaskManifestDir)
	if err != nil {
		log.Printf("Error reading task directory: %v", err)
		http.Error(w, "Could not read task directory", http.StatusInternalServerError)
		return
	}

	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".manifest.json") {
			taskId := strings.TrimSuffix(file.Name(), ".manifest.json")

			if _, isActive := uploader.ActiveUploads.Load(taskId); isActive {
				log.Printf("任务 %s 正在活跃上传中, 将其从待续传列表中跳过。", taskId)
				continue
			}

			manifest, err := uploader.ReadManifestFile(taskId)
			if err != nil {
				log.Printf("Skipping task %s: could not read manifest: %v", taskId, err)
				continue
			}

			completedSet, err := uploader.ReadCompletedLog(taskId)
			if err != nil {
				log.Printf("Skipping task %s: could not read completion log: %v", taskId, err)
				continue
			}

			var completedSize int64 = 0
			for _, f := range manifest.AllFiles {
				if _, isCompleted := completedSet[f.RelativePath]; isCompleted {
					completedSize += f.FileSize
				}
			}

			summary := TaskSummary{
				TaskId:         manifest.TaskInfo.TaskId,
				TaskName:       manifest.TaskInfo.TaskName,
				TotalFiles:     len(manifest.AllFiles),
				CompletedFiles: len(completedSet),
				TotalSize:      manifest.TaskInfo.TotalSize,
				UploadedSize:   completedSize,
				Dir:            manifest.TaskInfo.Dir,
				UserId:         manifest.TaskInfo.UserId,
				UserName:       manifest.TaskInfo.UserName,
			}
			pendingTasks = append(pendingTasks, summary)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pendingTasks)
	log.Printf("Found %d pending tasks (已过滤活跃任务).", len(pendingTasks))
}

// HandleResumeUpload 处理恢复上传请求
func HandleResumeUpload(w http.ResponseWriter, r *http.Request, app AppInterface) {
	var payload struct {
		TaskId string `json:"taskId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("Received resume request for TaskId=%s", payload.TaskId)

	// 同 /confirm-upload：空 taskId 会读到路径为 ".manifest.json" 的坏清单，直接拒绝
	payload.TaskId = strings.TrimSpace(payload.TaskId)
	if payload.TaskId == "" {
		log.Printf("错误：/resume-upload 请求缺少 taskId，拒绝续传。")
		http.Error(w, "taskId must not be empty", http.StatusBadRequest)
		return
	}

	if _, isAlreadyActive := uploader.ActiveUploads.LoadOrStore(payload.TaskId, true); isAlreadyActive {
		log.Printf("错误：任务 %s 已经正在上传中。", payload.TaskId)
		http.Error(w, "Task is already active", http.StatusConflict)
		return
	}

	manifest, err := uploader.ReadManifestFile(payload.TaskId)
	if err != nil {
		log.Printf("Error resuming task %s: could not read manifest: %v", payload.TaskId, err)
		http.Error(w, "Manifest file not found", http.StatusNotFound)
		uploader.ActiveUploads.Delete(payload.TaskId)
		return
	}

	completedSet, err := uploader.ReadCompletedLog(payload.TaskId)
	if err != nil {
		log.Printf("Error resuming task %s: could not read completion log: %v", payload.TaskId, err)
		http.Error(w, "Completion log not found", http.StatusInternalServerError)
		uploader.ActiveUploads.Delete(payload.TaskId)
		return
	}

	pendingFiles := uploader.CalculatePendingFiles(manifest, completedSet)
	if len(pendingFiles) == 0 {
		log.Printf("Task %s has no pending files. Cleaning up.", payload.TaskId)
		uploader.DeleteTaskFiles(payload.TaskId)
		http.Error(w, "Task already completed", http.StatusBadRequest)
		uploader.ActiveUploads.Delete(payload.TaskId)
		return
	}

	// 续传时清空 failed.log，让失败文件重新尝试
	uploader.ClearFailedLog(payload.TaskId)

	var initialUploadedBytes int64
	initialFileStates := make([]uploader.FileProgress, len(manifest.AllFiles))
	fileMap := make(map[string]utils.FullFileInfo, len(manifest.AllFiles))
	for _, f := range manifest.AllFiles {
		fileMap[f.RelativePath] = f
	}

	for i, fileInfo := range manifest.AllFiles {
		fp := uploader.FileProgress{
			FileName:     fileInfo.Metadata.FileName,
			RelativePath: fileInfo.RelativePath,
			AbsolutePath: fileInfo.AbsolutePath,
			TotalSize:    fileInfo.FileSize,
			UploadedSize: 0,
			Progress:     0,
		}
		if _, isCompleted := completedSet[fileInfo.RelativePath]; isCompleted {
			fp.Status = "COMPLETED"
			fp.UploadedSize = fileInfo.FileSize
			fp.Progress = 100
			initialUploadedBytes += fileInfo.FileSize
		} else {
			fp.Status = "QUEUED"
		}
		initialFileStates[i] = fp
	}

	uploader.Stats = &uploader.UploadStats{}
	uploader.Stats.SetTotalFiles(int64(len(manifest.AllFiles)))
	uploader.Stats.SetCompletedFiles(int64(len(completedSet)))
	uploader.Stats.SetTotalBytes(manifest.TaskInfo.TotalSize)
	uploader.Stats.SetUploadedBytes(initialUploadedBytes)
	uploader.Stats.SetStartTime(time.Now())

	uploadManager := uploader.NewManager(manifest.TaskInfo, pendingFiles, manifest.TaskInfo.TotalSize, uploader.TaskManifestDir, initialFileStates)
	uploadManager.SetCompleted(int32(len(completedSet)))
	uploadManager.SetTotalUploadedBytes(initialUploadedBytes)

	completedPaths := make([]string, 0, len(completedSet))
	for path := range completedSet {
		completedPaths = append(completedPaths, path)
	}

	resumeData := ResumeResponse{
		AllFiles:       manifest.AllFiles,
		CompletedPaths: completedPaths,
	}

	go StartUploadProcess(uploadManager, manifest.TaskInfo, payload.TaskId, app)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resumeData)
}

// HandleConfirmUpload 处理确认上传请求
func HandleConfirmUpload(w http.ResponseWriter, r *http.Request, app AppInterface) {
	var payload uploader.TaskInfo
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("Received upload confirmation for TaskId=%s with %d files.", payload.TaskId, len(payload.SelectedIndices))

	// taskId 是整个任务的唯一标识：manifest 文件名、活跃任务表、后端的 SSE 通道 /
	// 暂停恢复命令 / 分片续传目录 / 文件去重集合全部以它为 key，落库后还是 c_file_id 的前缀。
	// 空值不会报错但会让上述机制全部失效（现场出现过后端落库 c_task_id 为空的记录），
	// 所以在任务真正启动前就拒掉，别写出坏 manifest。
	payload.TaskId = strings.TrimSpace(payload.TaskId)
	if payload.TaskId == "" {
		log.Printf("错误：/confirm-upload 请求缺少 taskId（taskName=%q, userId=%q），拒绝启动上传。",
			payload.TaskName, payload.UserId)
		http.Error(w, "taskId must not be empty", http.StatusBadRequest)
		return
	}

	if _, isAlreadyActive := uploader.ActiveUploads.LoadOrStore(payload.TaskId, true); isAlreadyActive {
		log.Printf("错误：任务 %s 已经正在上传中。", payload.TaskId)
		http.Error(w, "Task is already active", http.StatusConflict)
		return
	}

	SessionMutex.RLock()
	selectedFiles := utils.FilterFilesByIndices(CurrentFileList, payload.SelectedIndices)
	SessionMutex.RUnlock()

	if len(selectedFiles) == 0 {
		log.Println("Error: No matching files found after filtering.")
		http.Error(w, "No matching files found based on indices", http.StatusBadRequest)
		uploader.ActiveUploads.Delete(payload.TaskId)
		return
	}

	var totalSize int64
	for _, file := range selectedFiles {
		totalSize += file.FileSize
	}
	payload.TotalSize = totalSize

	// 认领当前 ScanSession 的扫描 trace（如果有）。Manager.Start 拿到这两个字段后，
	// 会复用 traceId 并通过 RetagTrace 把扫描 trace 一并改成 upload 类型，
	// 让前端 trace 列表里只显示 1 条"文件上传"，里面包含完整的 file_scan +
	// metadata_send + upload_task 时间轴。失败 / 没匹配上时静默走原行为。
	if scanTraceId, scanSpanId, _, ok := ConsumeScanTrace(payload.TaskId); ok {
		payload.ScanTraceId = scanTraceId
		payload.ScanSpanId = scanSpanId
		log.Printf("任务 %s 认领扫描 trace=%s span=%s", payload.TaskId, scanTraceId, scanSpanId)
	}

	initialFileStates := make([]uploader.FileProgress, len(selectedFiles))
	for i, fileInfo := range selectedFiles {
		initialFileStates[i] = uploader.FileProgress{
			FileName:     fileInfo.Metadata.FileName,
			RelativePath: fileInfo.RelativePath,
			AbsolutePath: fileInfo.AbsolutePath,
			Status:       "QUEUED",
			TotalSize:    fileInfo.FileSize,
			UploadedSize: 0,
			Progress:     0,
		}
	}

	manifest := uploader.TaskManifest{
		TaskInfo: payload,
		AllFiles: selectedFiles,
	}
	if err := uploader.WriteManifestFile(payload.TaskId, &manifest); err != nil {
		log.Printf("严重错误: 无法写入 manifest file for task %s: %v", payload.TaskId, err)
		http.Error(w, "Failed to create task manifest", http.StatusInternalServerError)
		uploader.ActiveUploads.Delete(payload.TaskId)
		return
	}

	if err := uploader.CreateEmptyCompletedLog(payload.TaskId); err != nil {
		log.Printf("严重错误: 无法创建 completion log for task %s: %v", payload.TaskId, err)
		http.Error(w, "Failed to create task log", http.StatusInternalServerError)
		uploader.ActiveUploads.Delete(payload.TaskId)
		return
	}

	uploader.Stats = &uploader.UploadStats{}
	uploader.Stats.SetTotalFiles(int64(len(selectedFiles)))
	uploader.Stats.SetTotalBytes(totalSize)
	uploader.Stats.SetStartTime(time.Now())

	w.WriteHeader(http.StatusOK)
	uploadManager := uploader.NewManager(payload, selectedFiles, totalSize, uploader.TaskManifestDir, initialFileStates)

	go StartUploadProcess(uploadManager, payload, payload.TaskId, app)
}

package app

import (
	"clouddrive-companion-withWails2/internal/api"
	"clouddrive-companion-withWails2/internal/config"
	"clouddrive-companion-withWails2/internal/preselect"
	"clouddrive-companion-withWails2/internal/server"
	"clouddrive-companion-withWails2/internal/tracer"
	"clouddrive-companion-withWails2/internal/tray"
	"clouddrive-companion-withWails2/internal/uploader"
	"clouddrive-companion-withWails2/internal/utils"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// 定义应用状态
const (
	StateIdle             int32 = 0
	StateSelectingFiles   int32 = 1
	StateShowingDashboard int32 = 2
	StateConfigEditor     int32 = 3
)

// App 应用结构
type App struct {
	ctx         context.Context
	appState    atomic.Int32
	initialView string

	triggerStats chan bool

	// pendingPreselect 待恢复的预选列表（浏览器"追加"场景带回的上次已选文件），
	// 由 /reselect 带 body 时写入，FileSelector 挂载后经 GetPendingPreselect 消费式拉取。
	preselectMu      sync.Mutex
	pendingPreselect *preselect.Result

	lastStatsCheckTime     time.Time
	lastStatsUploadedBytes int64
}

// GetContext 获取上下文 (用于托盘)
func (a *App) GetContext() context.Context {
	return a.ctx
}

// NewApp 创建新的 App 实例
func NewApp() *App {
	return &App{
		triggerStats: make(chan bool, 1),
	}
}

// DashboardStats 仪表盘统计
type DashboardStats struct {
	ActiveTasks     int                        `json:"activeTasks"`
	QueuedTasks     int                        `json:"queuedTasks"`
	CompletedTasks  int                        `json:"completedTasks"`
	TotalFiles      int64                      `json:"totalFiles"`
	CompletedFiles  int64                      `json:"completedFiles"`
	CurrentSpeed    string                     `json:"currentSpeed"`
	CurrentSpeedBps int64                      `json:"currentSpeedBps"`
	Tasks           []uploader.TaskProgress    `json:"tasks"`
	SysStats        utils.SystemStats          `json:"sysStats"`
	EndpointInfo    server.EndpointSessionInfo `json:"endpointInfo"`
}

// OnStartup Wails 生命周期钩子
func (a *App) OnStartup(ctx context.Context, wasLaunchedByURI bool, openClientMode bool) {
	a.ctx = ctx
	log.Println("OnStartup: Application context saved.")

	if openClientMode {
		log.Println("启动方式: open-client URI (显示主面板)")
		go server.StartLocalServer(a)
		go a.runProgressEmitter()

		a.initialView = "Dashboard"
		a.appState.Store(StateShowingDashboard)

	} else if wasLaunchedByURI {
		go server.StartLocalServer(a)
		go a.runProgressEmitter()

		a.initialView = "FileSelector"
		a.appState.Store(StateSelectingFiles)

	} else {
		// 直接双击启动：打开上传面板（Dashboard）。
		// 配置面板不再是双击的入口，改为在 Dashboard 右上角的齿轮图标进入。
		//
		// 注意：Dashboard 依赖本地 HTTP 服务与进度推送协程，
		// 因此这里必须和 URI 启动一样把两者拉起来，否则面板拿不到统计数据，
		// 浏览器端的 /reselect、/ping 等调用也会失败。
		log.Println("启动方式: 直接双击启动 (打开上传面板)")
		go server.StartLocalServer(a)
		go a.runProgressEmitter()

		a.initialView = "Dashboard"
		a.appState.Store(StateShowingDashboard)
	}

	// ⚠️ 不要在这里用 EventsOn 注册 "doHide" / "doCancel" 来隐藏窗口。
	//
	// 历史教训（2026-08）：原先三个启动分支各注册了一对
	//   EventsOn("doHide"|"doCancel") → WindowHide
	// 而 doHide/doCancel 同时也被前端 FileSelector 监听（用来收扫描遮罩）。
	// Wails v2 的事件注销是**按事件名整体删除、不区分注册方**的：
	//   前端最后一个 JS 监听器被摘掉时（组件卸载调用 EventsOn 返回的取消函数），
	//   注入运行时的 listenerOff → removeListener 会向 Go 端发 'EX<eventName>'，
	//   dispatcher 收到后执行 events.Off(name) → delete(listeners, name)，
	//   把**Go 侧注册的同名监听器一并抹掉**。
	// 后果：文件选择器第二次被唤起（组件重挂载 → 卸载旧实例）之后，
	//   点"取消"再也收不起窗口，而点 X 仍正常（OnBeforeClose 直接调 WindowHide）。
	//
	// 结论：Go 要控制自己的窗口就直接调 WindowHide/ShowWindow，不要绕事件往返。
	// doHide/doCancel 现在是**单向的对前端通知**（前端据此复位扫描遮罩），
	// 窗口的隐藏由 CancelSelection / performScan 自己直接完成。
	// 同理，凡是 Go 侧 EventsOn 的事件名（frontend:ready、tray:*）都必须
	// 保证前端不会 EventsOn 同名事件，否则会重演这个问题。

	wailsRuntime.EventsOn(a.ctx, "frontend:ready", func(optionalData ...interface{}) {
		// 前端握手时会带上界面版本标识（来自 frontend/src/version.js，也就是右下角
		// 水印显示的那个值）。记进日志，现场反馈时直接从日志对账装的是哪个包。
		if len(optionalData) > 0 && optionalData[0] != nil {
			log.Printf("收到 'frontend:ready' 握手信号，界面版本=%v", optionalData[0])
		} else {
			log.Println("收到 'frontend:ready' 握手信号（未携带版本标识）")
		}
		wailsRuntime.EventsEmit(a.ctx, "view:show", a.initialView)
	})
	wailsRuntime.EventsOn(a.ctx, "tray:showDashboard", func(optionalData ...interface{}) {
		log.Println("收到 'tray:showDashboard' 事件，正在显示主面板")
		a.ShowDashboard()
	})
	wailsRuntime.EventsOn(a.ctx, "tray:quit", func(optionalData ...interface{}) {
		log.Println("收到 'tray:quit' 事件，正在退出程序")
		a.Quit()
	})

	log.Println("应用已启动")
}

// OnBeforeClose Wails 生命周期钩子
func (a *App) OnBeforeClose() (prevent bool) {
	if runtime.GOOS != "windows" {
		log.Println("非 Windows 系统: 关闭窗口将退出应用。")
		a.Quit()
		return false
	}

	currentState := a.appState.Load()
	log.Printf("窗口即将关闭 (当前状态: %d)，隐藏到托盘", currentState)

	if currentState == StateSelectingFiles {
		log.Println("关闭窗口，同时取消文件选择")
		a.CancelSelection()
	}

	a.appState.Store(StateIdle)
	wailsRuntime.WindowHide(a.ctx)
	return true
}

// ShowWindow 显示主窗口
func (a *App) ShowWindow() {
	if a.ctx != nil {
		wailsRuntime.WindowShow(a.ctx)
		wailsRuntime.WindowUnminimise(a.ctx)
		wailsRuntime.WindowCenter(a.ctx)
		wailsRuntime.WindowSetAlwaysOnTop(a.ctx, true)
		wailsRuntime.WindowSetAlwaysOnTop(a.ctx, false)
		wailsRuntime.WindowShow(a.ctx)
		log.Println("窗口已显示")
	}
}

// HideWindow 隐藏主窗口到托盘
func (a *App) HideWindow() {
	if a.ctx != nil {
		wailsRuntime.WindowHide(a.ctx)
		log.Println("窗口已隐藏到托盘")
	}
}

// Quit 退出应用程序
func (a *App) Quit() {
	log.Println("应用程序正在退出...")
	// 先退出托盘并等图标清理完成（内部有超时保护）：进程直接退掉时托盘图标
	// 不会自动消失，会在通知区域留下一个"点不动"的幽灵图标（右键无反应，
	// 鼠标划过才被 Explorer 回收）——用户反馈的"托盘右键没反应"多源于此。
	tray.Quit()
	// 任务追踪：在退出前 flush 异步队列，最多等 3 秒（内部已超时保护）
	tracer.Shutdown()
	uploader.ShutdownHistoryStore()
	if a.ctx != nil {
		wailsRuntime.Quit(a.ctx)
	}
	os.Exit(0)
}

// ShowDashboard 显示仪表盘
func (a *App) ShowDashboard() {
	if a.appState.CompareAndSwap(StateSelectingFiles, StateShowingDashboard) {
		log.Println("ShowDashboard: 正在覆盖文件选择状态...")
		// 用户从文件选择切到 Dashboard，等同于取消当前扫描会话
		server.CancelScanSession()
	} else {
		a.appState.Store(StateShowingDashboard)
	}

	wailsRuntime.EventsEmit(a.ctx, "view:show", "Dashboard")
	a.ShowWindow()
	log.Println("ShowDashboard: 成功")
}

// ShowConfigEditor 显示配置编辑器
func (a *App) ShowConfigEditor() {
	if a.appState.CompareAndSwap(StateSelectingFiles, StateConfigEditor) {
		log.Println("ShowConfigEditor: 正在覆盖文件选择状态...")
		server.CancelScanSession()
	} else {
		a.appState.Store(StateConfigEditor)
	}

	wailsRuntime.EventsEmit(a.ctx, "view:show", "ConfigEditor")
	a.ShowWindow()
	log.Println("ShowConfigEditor: 成功")
}

// ShowFileSelector 显示文件选择器（全新选择，清掉可能残留的预选恢复数据）
func (a *App) ShowFileSelector() {
	a.setPendingPreselect(nil)
	a.showFileSelectorView()
	log.Println("ShowFileSelector: 成功")
}

// ShowFileSelectorWithPreselect 显示文件选择器并恢复浏览器带回的上次已选列表
// （"追加"场景）。预选数据先存后开窗口：FileSelector 挂载后通过 GetPendingPreselect
// 主动拉取——拉取模型天然规避"事件先于前端监听器注册"的时序竞态。
func (a *App) ShowFileSelectorWithPreselect(result *preselect.Result) {
	log.Printf("ShowFileSelectorWithPreselect: 预选恢复 received=%d dirs=%d files=%d missing=%d",
		result.Received, len(result.Dirs), len(result.Files), len(result.Missing))
	a.setPendingPreselect(result)
	a.showFileSelectorView()
	log.Println("ShowFileSelectorWithPreselect: 成功")
}

// showFileSelectorView 切换视图并显示窗口（不涉及预选数据处理）
func (a *App) showFileSelectorView() {
	a.appState.Store(StateSelectingFiles)
	wailsRuntime.EventsEmit(a.ctx, "view:show", "FileSelector")
	a.ShowWindow()
}

// setPendingPreselect 设置/清空待恢复的预选列表
func (a *App) setPendingPreselect(result *preselect.Result) {
	a.preselectMu.Lock()
	defer a.preselectMu.Unlock()
	a.pendingPreselect = result
}

// GetPendingPreselect 供 FileSelector 挂载后拉取待恢复的预选列表（Wails 绑定，
// 消费式：取走即清，保证每次打开选择器只恢复一次）。无待恢复数据时返回 nil。
func (a *App) GetPendingPreselect() *preselect.Result {
	a.preselectMu.Lock()
	defer a.preselectMu.Unlock()
	result := a.pendingPreselect
	a.pendingPreselect = nil
	return result
}

// formatSpeed 格式化速度
func formatSpeed(bytesPerSecond int64) string {
	if bytesPerSecond < 1024 {
		return fmt.Sprintf("%d B/s", bytesPerSecond)
	}
	kb := float64(bytesPerSecond) / 1024
	if kb < 1024 {
		return fmt.Sprintf("%.1f KB/s", kb)
	}
	mb := kb / 1024
	if mb < 1024 {
		return fmt.Sprintf("%.2f MB/s", mb)
	}
	gb := mb / 1024
	return fmt.Sprintf("%.2f GB/s", gb)
}

// TriggerStatsRefresh 触发统计刷新
func (a *App) TriggerStatsRefresh() {
	select {
	case a.triggerStats <- true:
		log.Println("TriggerStatsRefresh: 触发即时刷新")
	default:
	}
}

// runProgressEmitter 运行进度发射器
func (a *App) runProgressEmitter() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	a.lastStatsCheckTime = time.Now()

	for {
		select {
		case <-a.ctx.Done():
			return

		case <-ticker.C:
			a.emitDashboardStats()

		case <-a.triggerStats:
			log.Println("runProgressEmitter: 收到即时刷新信号")
			a.emitDashboardStats()
		}
	}
}

// emitDashboardStats 发射仪表盘统计信息
func (a *App) emitDashboardStats() {
	stats := DashboardStats{
		Tasks: make([]uploader.TaskProgress, 0),
	}

	stats.SysStats = utils.GetNetworkStats()
	stats.EndpointInfo = server.GetEndpointInfo()

	var currentTotalUploadedBytes int64

	server.ManagerMutex.Lock()
	activeManager := server.GlobalUploadManager
	server.ManagerMutex.Unlock()

	activeTaskId := ""
	if activeManager != nil {
		activeTaskId = activeManager.GetTaskId()
		taskProgress := activeManager.GetTaskProgress()
		taskProgress.Files = nil // 不在定时推送中发送文件列表，前端按需分页加载
		stats.Tasks = append(stats.Tasks, taskProgress)
		currentTotalUploadedBytes += taskProgress.UploadedSize
		stats.ActiveTasks = 1
		stats.TotalFiles += int64(taskProgress.TotalFiles)
		stats.CompletedFiles += int64(taskProgress.CompletedFiles)
	}

	files, err := os.ReadDir(uploader.TaskManifestDir)
	if err == nil {
		for _, file := range files {
			if strings.HasSuffix(file.Name(), ".manifest.json") {
				taskId := strings.TrimSuffix(file.Name(), ".manifest.json")
				if taskId == activeTaskId {
					continue
				}
				if _, isActive := uploader.ActiveUploads.Load(taskId); isActive && taskId != activeTaskId {
					manifest, err := uploader.ReadManifestFile(taskId)
					if err != nil {
						continue
					}
					stats.Tasks = append(stats.Tasks, uploader.TaskProgress{
						TaskId:   taskId,
						TaskName: manifest.TaskInfo.TaskName,
						Status:   "QUEUED",
					})
					stats.QueuedTasks++
					continue
				}
				manifest, _ := uploader.ReadManifestFile(taskId)
				completedSet, _ := uploader.ReadCompletedLog(taskId)
				if manifest != nil {
					failedSet, _ := uploader.ReadFailedLog(taskId)
					var completedSize int64
					completedCount := 0
					failedCount := 0
					fileMap := make(map[string]utils.FullFileInfo)
					for _, f := range manifest.AllFiles {
						fileMap[f.RelativePath] = f
					}
					for path := range completedSet {
						if f, ok := fileMap[path]; ok {
							completedSize += f.FileSize
							completedCount++
						}
					}
					for range failedSet {
						failedCount++
					}
					progress := 0.0
					if manifest.TaskInfo.TotalSize > 0 {
						progress = float64(completedSize) / float64(manifest.TaskInfo.TotalSize) * 100
					}

					// 判断任务状态：有失败文件且没有待上传文件 = 部分完成
					taskStatus := "PAUSED"
					pendingCount := len(manifest.AllFiles) - completedCount - failedCount
					if failedCount > 0 && pendingCount == 0 {
						taskStatus = "PARTIAL_COMPLETED"
					}

					stats.Tasks = append(stats.Tasks, uploader.TaskProgress{
						TaskId:         taskId,
						TaskName:       manifest.TaskInfo.TaskName,
						Status:         taskStatus,
						TotalSize:      manifest.TaskInfo.TotalSize,
						UploadedSize:   completedSize,
						TotalFiles:     len(manifest.AllFiles),
						CompletedFiles: completedCount,
						FailedFiles:    failedCount,
						Progress:       progress,
						Files:          nil, // 不在定时推送中发送文件列表
					})
					stats.QueuedTasks++
					stats.TotalFiles += int64(len(manifest.AllFiles))
					stats.CompletedFiles += int64(completedCount)
				}
			}
		}
	}

	now := time.Now()
	duration := now.Sub(a.lastStatsCheckTime).Seconds()
	if duration > 0 && a.lastStatsUploadedBytes > 0 {
		bytesDiff := currentTotalUploadedBytes - a.lastStatsUploadedBytes
		if bytesDiff < 0 {
			bytesDiff = 0
		}
		stats.CurrentSpeedBps = int64(float64(bytesDiff) / duration)
		stats.CurrentSpeed = formatSpeed(stats.CurrentSpeedBps)
	} else {
		stats.CurrentSpeed = "0 B/s"
	}
	a.lastStatsUploadedBytes = currentTotalUploadedBytes
	a.lastStatsCheckTime = now

	wailsRuntime.EventsEmit(a.ctx, "dashboard:stats", stats)
}

// PauseTask 暂停任务
func (a *App) PauseTask(taskId string) error {
	server.ManagerMutex.Lock()
	if server.GlobalUploadManager != nil && server.GlobalUploadManager.GetTaskId() == taskId {
		log.Printf("[App] 立即在本地暂停任务: %s", taskId)
		server.GlobalUploadManager.Pause()
	}
	server.ManagerMutex.Unlock()

	a.TriggerStatsRefresh()

	return api.SendTaskCommand(taskId, "pause")
}

// ResumeTask 恢复任务
func (a *App) ResumeTask(taskId string) error {
	// 空 taskId 会读到路径为 ".manifest.json" 的坏清单，并让后端所有以 taskId
	// 为 key 的状态（SSE、暂停命令、分片续传目录、去重集合）失效，直接拒绝。
	taskId = strings.TrimSpace(taskId)
	if taskId == "" {
		log.Printf("[App] 错误：ResumeTask 收到空 taskId，拒绝续传。")
		return fmt.Errorf("taskId must not be empty")
	}

	server.ManagerMutex.Lock()
	activeManager := server.GlobalUploadManager
	server.ManagerMutex.Unlock()

	if activeManager != nil && activeManager.GetTaskId() == taskId {
		// 活跃任务的暂停恢复（正常流程）
		log.Printf("[App] 立即在本地恢复任务: %s", taskId)
		activeManager.Resume()
		a.TriggerStatsRefresh()
		return api.SendTaskCommand(taskId, "resume")
	}

	// 断电重启后的断点续传：没有活跃的 Manager，需要重新创建上传流程
	log.Printf("[App] 任务 %s 无活跃 Manager，启动断点续传流程...", taskId)

	// 检查是否已经在活跃列表中（防止重复启动）
	if _, isAlreadyActive := uploader.ActiveUploads.LoadOrStore(taskId, true); isAlreadyActive {
		log.Printf("[App] 错误：任务 %s 已经正在上传中。", taskId)
		return fmt.Errorf("task %s is already active", taskId)
	}

	manifest, err := uploader.ReadManifestFile(taskId)
	if err != nil {
		log.Printf("[App] 读取任务 %s 的 manifest 失败: %v", taskId, err)
		uploader.ActiveUploads.Delete(taskId)
		return fmt.Errorf("manifest file not found for task %s: %w", taskId, err)
	}

	completedSet, err := uploader.ReadCompletedLog(taskId)
	if err != nil {
		log.Printf("[App] 读取任务 %s 的 completed log 失败: %v", taskId, err)
		uploader.ActiveUploads.Delete(taskId)
		return fmt.Errorf("completion log error for task %s: %w", taskId, err)
	}

	pendingFiles := uploader.CalculatePendingFiles(manifest, completedSet)
	if len(pendingFiles) == 0 {
		log.Printf("[App] 任务 %s 没有待上传文件，清理任务文件。", taskId)
		uploader.DeleteTaskFiles(taskId)
		uploader.ActiveUploads.Delete(taskId)
		return fmt.Errorf("task %s already completed", taskId)
	}

	// 续传时清空 failed.log，让失败文件重新尝试
	uploader.ClearFailedLog(taskId)

	var initialUploadedBytes int64
	initialFileStates := make([]uploader.FileProgress, len(manifest.AllFiles))
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

	go server.StartUploadProcess(uploadManager, manifest.TaskInfo, taskId, a)

	a.TriggerStatsRefresh()
	log.Printf("[App] 任务 %s 断点续传已启动，待上传 %d 个文件。", taskId, len(pendingFiles))

	// 通知后端清除暂停状态
	return api.SendTaskCommand(taskId, "resume")
}

// PauseFile 暂停文件
func (a *App) PauseFile(taskId string, relativePath string) error {
	server.ManagerMutex.Lock()
	if server.GlobalUploadManager != nil && server.GlobalUploadManager.GetTaskId() == taskId {
		log.Printf("[App] 立即在本地暂停文件: %s / %s", taskId, relativePath)
		server.GlobalUploadManager.PauseFile(relativePath)
	}
	server.ManagerMutex.Unlock()

	a.TriggerStatsRefresh()

	return api.SendFileCommand(taskId, relativePath, "pause")
}

// ResumeFile 恢复文件
func (a *App) ResumeFile(taskId string, relativePath string) error {
	server.ManagerMutex.Lock()
	if server.GlobalUploadManager != nil && server.GlobalUploadManager.GetTaskId() == taskId {
		log.Printf("[App] 立即在本地恢复文件: %s / %s", taskId, relativePath)
		server.GlobalUploadManager.ResumeFile(relativePath)
	}
	server.ManagerMutex.Unlock()

	a.TriggerStatsRefresh()

	return api.SendFileCommand(taskId, relativePath, "resume")
}

// GetTaskFiles 分页获取任务文件列表（前端懒加载调用）
func (a *App) GetTaskFiles(taskId string, offset, limit int, sortKey, sortDir, statusFilter string) uploader.PagedFileResult {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	// 先检查是否是当前活动任务
	server.ManagerMutex.Lock()
	activeManager := server.GlobalUploadManager
	server.ManagerMutex.Unlock()

	if activeManager != nil && activeManager.GetTaskId() == taskId {
		return activeManager.GetFilesPage(offset, limit, sortKey, sortDir, statusFilter)
	}

	// 非活动任务，从 manifest 文件读取
	return a.getManifestTaskFiles(taskId, offset, limit, sortKey, sortDir, statusFilter)
}

// getManifestTaskFiles 从 manifest / 历史 db 读取分页文件列表。
//
// 优先级：
//  1. task_history.db 中的 files_json（已完成 / 已写入历史的任务）
//  2. manifest 文件 + completed.log + failed.log（未完成任务）
//
// 排序、状态过滤、分页对两种来源行为一致。
func (a *App) getManifestTaskFiles(taskId string, offset, limit int, sortKey, sortDir, statusFilter string) uploader.PagedFileResult {
	// 1) history db
	if histFiles, err := uploader.GetHistoryFiles(taskId); err == nil && len(histFiles) > 0 {
		return paginateFiles(histFiles, offset, limit, sortKey, sortDir, statusFilter)
	}

	// 2) manifest 兜底
	manifest, err := uploader.ReadManifestFile(taskId)
	if err != nil {
		return uploader.PagedFileResult{Files: []uploader.FileProgress{}, Total: 0}
	}

	completedSet, _ := uploader.ReadCompletedLog(taskId)
	failedSet, _ := uploader.ReadFailedLog(taskId)

	allFiles := make([]uploader.FileProgress, 0, len(manifest.AllFiles))
	for _, f := range manifest.AllFiles {
		fp := uploader.FileProgress{
			FileName:     f.Metadata.FileName,
			RelativePath: f.RelativePath,
			AbsolutePath: f.AbsolutePath,
			TotalSize:    f.FileSize,
		}
		if _, ok := completedSet[f.RelativePath]; ok {
			fp.Status = "COMPLETED"
			fp.UploadedSize = f.FileSize
			fp.Progress = 100
		} else if _, ok := failedSet[f.RelativePath]; ok {
			fp.Status = "FAILED"
		} else {
			fp.Status = "QUEUED"
		}
		allFiles = append(allFiles, fp)
	}
	return paginateFiles(allFiles, offset, limit, sortKey, sortDir, statusFilter)
}

// paginateFiles 对一个 FileProgress 切片做"过滤 + 排序 + 切片"，给两种数据源
// 共用，保证行为一致。
func paginateFiles(allFiles []uploader.FileProgress, offset, limit int, sortKey, sortDir, statusFilter string) uploader.PagedFileResult {
	// 状态过滤
	if statusFilter != "" && statusFilter != "ALL" {
		filtered := make([]uploader.FileProgress, 0, len(allFiles))
		for _, fp := range allFiles {
			if fp.Status == statusFilter {
				filtered = append(filtered, fp)
			}
		}
		allFiles = filtered
	}

	uploader.SortFiles(allFiles, sortKey, sortDir)

	total := len(allFiles)
	if offset >= total {
		return uploader.PagedFileResult{Files: []uploader.FileProgress{}, Total: total}
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return uploader.PagedFileResult{Files: allFiles[offset:end], Total: total}
}

// RevealFileInFolder 打开文件所在的本地文件夹并在资源管理器中选中该文件。
// path 为文件的本地绝对路径（FileProgress.AbsolutePath，扫描时就是 slash 形式，
// 这里统一转成本地分隔符再交给 explorer）。
func (a *App) RevealFileInFolder(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("文件路径为空")
	}
	localPath := filepath.FromSlash(path)
	if _, err := os.Stat(localPath); err != nil {
		return fmt.Errorf("文件不存在或已被移动: %w", err)
	}
	// explorer /select, 即使成功打开也经常返回非零退出码，所以不能凭退出码判失败；
	// 用 Run 等它退出以释放进程句柄。
	if err := exec.Command("explorer", "/select,", localPath).Run(); err != nil {
		log.Printf("[RevealFileInFolder] explorer 返回非零退出码（通常不影响打开）: %v", err)
	}
	return nil
}

// ListHistoricalTasks 列出本地保留的所有历史任务（不含正在活跃的），按完成
// 时间倒序。返回的 TaskProgress 状态语义与"上传任务"列表完全一致，前端可以
// 原样复用同一套渲染。
//
// 数据来源（按优先级合并去重）：
//  1. task_history.db —— 任务结束时一次性写入的完整快照。这是主数据源，
//     即使 manifest / completed.log 被清理也能查到。
//  2. manifest 目录 —— 兜底：处理"history db 还没写入但 manifest 还在"的
//     极端情况（启动崩溃恢复 / 老数据迁移期）。
//
// 不在这里返回 Files 字段，前端用 GetTaskFiles 按需分页加载（与活跃任务一致）。
func (a *App) ListHistoricalTasks() []uploader.TaskProgress {
	out := make([]uploader.TaskProgress, 0)
	seen := make(map[string]struct{})

	// 1) 主源：task_history.db
	if records, err := uploader.ListHistoryRecords(); err != nil {
		log.Printf("[history] read history db failed: %v", err)
	} else {
		for _, r := range records {
			if _, isActive := uploader.ActiveUploads.Load(r.TaskId); isActive {
				continue
			}
			var progress float64
			if r.TotalSize > 0 {
				progress = float64(r.UploadedSize) / float64(r.TotalSize) * 100
			} else if r.Status == "COMPLETED" {
				progress = 100
			}
			out = append(out, uploader.TaskProgress{
				TaskId:         r.TaskId,
				TaskName:       r.TaskName,
				Status:         r.Status,
				TotalSize:      r.TotalSize,
				UploadedSize:   r.UploadedSize,
				TotalFiles:     r.TotalFiles,
				CompletedFiles: r.CompletedFiles,
				FailedFiles:    r.FailedFiles,
				Progress:       progress,
				Files:          nil,
				TraceId:        r.TraceId,
			})
			seen[r.TaskId] = struct{}{}
		}
	}

	// 2) 兜底：扫 manifest 目录补充未在 history db 中的（兼容老数据 / 未完成任务）
	entries, err := os.ReadDir(uploader.TaskManifestDir)
	if err != nil {
		log.Printf("[history] read manifest dir failed: %v", err)
		return out
	}
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasSuffix(name, ".manifest.json") {
			continue
		}
		taskId := strings.TrimSuffix(name, ".manifest.json")
		if _, dup := seen[taskId]; dup {
			continue
		}
		if _, isActive := uploader.ActiveUploads.Load(taskId); isActive {
			continue
		}

		manifest, err := uploader.ReadManifestFile(taskId)
		if err != nil {
			log.Printf("[history] read manifest %s: %v", taskId, err)
			continue
		}
		completedSet, _ := uploader.ReadCompletedLog(taskId)
		failedSet, _ := uploader.ReadFailedLog(taskId)

		var totalSize, uploadedSize int64
		var completedCount, failedCount int
		for _, f := range manifest.AllFiles {
			totalSize += f.FileSize
			if _, ok := completedSet[f.RelativePath]; ok {
				uploadedSize += f.FileSize
				completedCount++
			} else if _, ok := failedSet[f.RelativePath]; ok {
				failedCount++
			}
		}

		totalFiles := len(manifest.AllFiles)
		status := "INTERRUPTED"
		switch {
		case completedCount == totalFiles && totalFiles > 0:
			status = "COMPLETED"
		case failedCount > 0:
			status = "PARTIAL_COMPLETED"
		}

		var progress float64
		if totalSize > 0 {
			progress = float64(uploadedSize) / float64(totalSize) * 100
		} else if status == "COMPLETED" {
			progress = 100
		}

		out = append(out, uploader.TaskProgress{
			TaskId:         taskId,
			TaskName:       manifest.TaskInfo.TaskName,
			Status:         status,
			TotalSize:      totalSize,
			UploadedSize:   uploadedSize,
			TotalFiles:     totalFiles,
			CompletedFiles: completedCount,
			FailedFiles:    failedCount,
			Progress:       progress,
			Files:          nil,
		})
	}

	// history db 这一段已经是 finished_at_ms DESC，manifest 兜底插在后面，
	// 整体上"最新完成的在前"足够用。复杂场景再统一按 ModTime 排序也行。
	return out
}

// DeleteHistoricalTask 从本地删除一条历史任务（含 task_history.db 中的记录与
// 残留的 manifest / completed.log / failed.log）。仅对非活跃任务有效。
func (a *App) DeleteHistoricalTask(taskId string) error {
	if _, isActive := uploader.ActiveUploads.Load(taskId); isActive {
		return fmt.Errorf("任务 %s 正在活跃中，无法从历史记录删除", taskId)
	}
	if err := uploader.DeleteHistoryRecord(taskId); err != nil {
		log.Printf("[history] delete db record %s: %v", taskId, err)
	}
	// manifest 等文件成功完成的任务里早已被清理；这里再调一次保证未完成任务的
	// 文件也一起清掉，幂等。
	return uploader.DeleteTaskFiles(taskId)
}

// DeletePausedTask 删除一个已暂停 / 部分完成（未跑完）的上传任务。
//
// 与 DeleteHistoricalTask 的区别：后者只处理"非活跃"的历史记录，遇到活跃任务
// 直接报错；本方法面向仪表盘"上传任务"列表里那些还没跑完的任务，允许在暂停状态
// 下把它彻底删掉，因此需要先把可能还活着的 Manager 停掉。
//
// 两种情形都要覆盖：
//  1. 任务仍是当前活跃 Manager（用户点了暂停但进程没退）→ 先 Stop() 让它退出，
//     等 ActiveUploads 中的记录被 StartUploadProcess 的 defer 清掉。
//  2. 客户端重启后由 manifest 还原出来的暂停任务（Manager 为 nil）→ 直接删文件。
//
// 删除内容：manifest / completed.log / failed.log + task_history.db 里的残留记录。
// 已经上传到服务端的文件不受影响。
func (a *App) DeletePausedTask(taskId string) error {
	if taskId == "" {
		return fmt.Errorf("taskId 不能为空")
	}

	// 1. 若该任务正是当前活跃的 Manager，先把它停掉
	server.ManagerMutex.Lock()
	activeManager := server.GlobalUploadManager
	server.ManagerMutex.Unlock()

	if activeManager != nil && activeManager.GetTaskId() == taskId {
		log.Printf("[App] 删除任务 %s：它仍是活跃 Manager，先中止上传。", taskId)
		activeManager.Stop()
	}

	// 2. 等待任务真正从活跃列表中退出，避免删完文件后 worker 又把日志写回来。
	//    最多等 5 秒；超时则继续删除（Stop 已经取消了 ctx，残留写入影响有限）。
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, isActive := uploader.ActiveUploads.Load(taskId); !isActive {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if _, isActive := uploader.ActiveUploads.Load(taskId); isActive {
		log.Printf("[App] 警告：任务 %s 在等待超时后仍处于活跃列表，继续执行删除。", taskId)
	}

	// 3. 清理历史库中的残留记录（未跑完的任务也可能写过 INTERRUPTED 快照）
	if err := uploader.DeleteHistoryRecord(taskId); err != nil {
		log.Printf("[history] delete db record %s: %v", taskId, err)
	}

	// 4. 删除本地任务文件（manifest / completed.log / failed.log），幂等
	if err := uploader.DeleteTaskFiles(taskId); err != nil {
		return fmt.Errorf("删除任务 %s 的本地文件失败: %w", taskId, err)
	}

	log.Printf("[App] 任务 %s 已删除。", taskId)

	// 5. 立即刷新仪表盘，让卡片马上消失
	a.TriggerStatsRefresh()

	return nil
}

// restartFlag 与 main.go 中的同名常量保持一致：
// 传给新实例，告知它"这是重启拉起的，跳过单实例探测、先等端口释放"。
const restartFlag = "--restarted"

// HasActiveUpload 是否存在正在进行（非暂停）的上传任务。
// 前端在"保存并重启"前用它决定是否需要额外提示用户，避免打断上传。
func (a *App) HasActiveUpload() bool {
	server.ManagerMutex.Lock()
	activeManager := server.GlobalUploadManager
	server.ManagerMutex.Unlock()

	if activeManager == nil {
		return false
	}
	// 已暂停的任务重启后可以续传，不算"会被打断"
	return activeManager.GetTaskProgress().Status == "UPLOADING"
}

// SaveConfigAndRestart 保存配置并重启客户端，让新配置立刻生效。
//
// 配置项（端口、URI Scheme、并发数等）大多在启动时读取，改完必须重启进程才生效。
// 这里把"保存 + 重启"合成一步，省掉用户手动退出再打开的麻烦。
//
// 流程：
//  1. 先复用 SaveConfig 落盘（失败则直接返回，不重启）
//  2. 以 restartFlag 参数拉起一个新进程（新实例会等本进程端口释放后再启动，
//     并按双击模式直接显示上传面板）
//  3. 延迟一小段时间再退出当前进程，让这次调用的响应能先回到前端
//
// 注意：重启会中断正在进行的上传，未完成的任务会保留 manifest，重启后可续传。
func (a *App) SaveConfigAndRestart(newConfig map[string]interface{}) error {
	// 1. 先保存，保存失败就不要重启，避免用户改动丢失
	if err := a.SaveConfig(newConfig); err != nil {
		return fmt.Errorf("保存配置失败，未执行重启: %w", err)
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("配置已保存，但获取程序路径失败，无法自动重启: %w", err)
	}

	// 2. 拉起新实例
	cmd := exec.Command(exePath, restartFlag)
	cmd.Dir = filepath.Dir(exePath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("配置已保存，但启动新实例失败，请手动重启客户端: %w", err)
	}
	// 不等待子进程（它是独立的新实例），显式释放句柄避免僵尸进程
	go func() { _ = cmd.Wait() }()

	log.Printf("[ConfigEditor] 已保存配置并拉起新实例 (pid=%d)，当前实例即将退出。", cmd.Process.Pid)

	// 3. 稍等一下再退出，让前端先收到本次调用的成功响应
	go func() {
		time.Sleep(600 * time.Millisecond)
		a.Quit()
	}()

	return nil
}

// QueryHTTPLogs 按条件分页查询 HTTP 请求日志（来自 SQLite）
//
//	page: 1-based; size: 每页条数（>=1）
//	查询参数支持：method、pathLike（模糊）、statusGroup（"2xx"/"3xx"/"4xx"/"5xx"）、startMs、endMs
func (a *App) QueryHTTPLogs(query server.HTTPLogQuery) (server.HTTPLogPage, error) {
	return server.QueryHTTPLogs(query)
}

// GetHTTPLogByID 拉取一条完整 HTTP 请求日志（详情视图用）
func (a *App) GetHTTPLogByID(id int64) (*server.HTTPRequestLog, error) {
	return server.GetHTTPLogByID(id)
}

// GetHTTPLogStats 返回 HTTP 请求日志数据库的整体统计
func (a *App) GetHTTPLogStats() server.HTTPLogStats {
	return server.GetHTTPLogStats()
}

// GetSequenceEvents 时序图数据：按时间正序查询指定范围内的入站/出站请求日志
//
//	fromMs/toMs: 毫秒时间戳，0 表示不限制
//	taskId:      可选，非空时只返回该任务的日志
//	direction:   可选，"in"/"out"，空串表示全部
//	page/size:   1-based 分页
func (a *App) GetSequenceEvents(fromMs, toMs int64, taskId string, direction string, page int, size int) (server.HTTPLogPage, error) {
	entries, total, err := server.QuerySequenceEvents(fromMs, toMs, taskId, direction, page, size)
	if err != nil {
		return server.HTTPLogPage{Page: page, Size: size, Entries: []server.HTTPRequestLog{}}, err
	}
	return server.HTTPLogPage{Total: total, Page: page, Size: size, Entries: entries}, nil
}

// ListAppLogFiles 列出应用日志文件（clouddrive-companion-YYYY-MM-DD.log），按日期倒序
func (a *App) ListAppLogFiles() []server.AppLogFileInfo {
	return server.ListAppLogFiles()
}

// ReadAppLogFile 按行倒序分页读取应用日志
//
//	name:    日志文件名
//	offset:  字节偏移，首次传 0 表示从文件末尾开始
//	size:    本次最多返回的行数
//	keyword: 可选关键字过滤（大小写不敏感）
func (a *App) ReadAppLogFile(name string, offset int64, size int, keyword string) (server.AppLogPage, error) {
	return server.ReadAppLogFile(name, offset, size, keyword)
}

// ─── 任务级链路追踪 ───
//
// 全部委托给 internal/tracer 包；这里仅做 Wails Bind 的薄壳层，让前端能用
// 与 HTTP 端点等价的方式调用（Dashboard.vue 走 Bind，浏览器版查看器走 HTTP）。

// TraceListResponse 与 /task-traces/list 的 JSON 结构对齐
type TraceListResponse struct {
	Total int                   `json:"total"`
	Page  int                   `json:"page"`
	Size  int                   `json:"size"`
	Items []tracer.TraceSummary `json:"items"`
}

// TraceAllSpansResponse 与 /task-traces/{traceId}/all 的 JSON 结构对齐
type TraceAllSpansResponse struct {
	Spans     []tracer.Span `json:"spans"`
	Truncated bool          `json:"truncated"`
}

// TraceModulesResponse 与 /task-traces/modules 的 JSON 结构对齐
type TraceModulesResponse struct {
	Global  bool                 `json:"global"`
	Modules []tracer.TraceModule `json:"modules"`
}

// TraceConfigResponse 当前追踪配置
type TraceConfigResponse struct {
	SamplingThreshold int `json:"traceSamplingThreshold"`
	MaxAgeDays        int `json:"traceMaxAgeDays"`
	MaxRows           int `json:"traceMaxRows"`
	VacuumThresholdMB int `json:"traceVacuumThresholdMB"`
}

// ListTracedTasks 分页查询追踪任务
//
//	traceType: "upload" / "scan" / 空字符串（全部）
//	taskId:    精确匹配；空字符串忽略
//	fromMs/toMs: 时间范围（毫秒），0 表示不限
//	page:      1-based；<1 视为 1
//	size:      每页条数；<1 视为 50，>200 截到 200
func (a *App) ListTracedTasks(traceType, taskId string, fromMs, toMs int64, page, size int) (TraceListResponse, error) {
	store := tracer.StoreRef()
	if store == nil {
		return TraceListResponse{}, fmt.Errorf("tracer not initialized")
	}
	summaries, total, err := store.ListTraces(tracer.ListTracesQuery{
		TraceType: traceType, TaskID: taskId,
		FromMs: fromMs, ToMs: toMs, Page: page, Size: size,
	})
	if err != nil {
		return TraceListResponse{}, err
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 50
	} else if size > 200 {
		size = 200
	}
	return TraceListResponse{Total: total, Page: page, Size: size, Items: summaries}, nil
}

// GetTraceTopSpans 获取某 trace 的所有顶层 span
func (a *App) GetTraceTopSpans(traceId string) ([]tracer.Span, error) {
	store := tracer.StoreRef()
	if store == nil {
		return nil, fmt.Errorf("tracer not initialized")
	}
	return store.GetTopSpans(traceId)
}

// GetSpanChildren 获取某 span 的直接子 span
func (a *App) GetSpanChildren(spanId string) ([]tracer.Span, error) {
	store := tracer.StoreRef()
	if store == nil {
		return nil, fmt.Errorf("tracer not initialized")
	}
	return store.GetChildren(spanId)
}

// GetTraceAllSpans 获取某 trace 的所有 span（平铺，最多 5000 条）
func (a *App) GetTraceAllSpans(traceId string) (TraceAllSpansResponse, error) {
	store := tracer.StoreRef()
	if store == nil {
		return TraceAllSpansResponse{}, fmt.Errorf("tracer not initialized")
	}
	spans, truncated, err := store.GetAllSpans(traceId, 5000)
	if err != nil {
		return TraceAllSpansResponse{}, err
	}
	return TraceAllSpansResponse{Spans: spans, Truncated: truncated}, nil
}

// DeleteTrace 删除某 trace 的所有 span，返回删除条数
func (a *App) DeleteTrace(traceId string) (int, error) {
	store := tracer.StoreRef()
	if store == nil {
		return 0, fmt.Errorf("tracer not initialized")
	}
	return store.DeleteTrace(traceId)
}

// ListTraceModules 列出所有追踪模块及全局开关状态
func (a *App) ListTraceModules() TraceModulesResponse {
	return TraceModulesResponse{
		Global:  config.AppConfig.TraceEnabled,
		Modules: tracer.Modules(),
	}
}

// SetTraceModuleEnabled 切换某追踪模块开关
func (a *App) SetTraceModuleEnabled(code string, enabled bool) error {
	if !tracer.SetModuleEnabled(code, enabled) {
		return fmt.Errorf("module not found: %s", code)
	}
	return nil
}

// SetTraceGlobalEnabled 切换全局总开关
func (a *App) SetTraceGlobalEnabled(enabled bool) error {
	if !tracer.SetGlobalEnabled(enabled) {
		return fmt.Errorf("failed to persist global flag")
	}
	return nil
}

// GetTraceConfig 获取当前追踪相关配置
func (a *App) GetTraceConfig() TraceConfigResponse {
	cfg := TraceConfigResponse{}
	if v := config.AppConfig.TraceSamplingThreshold; v != nil {
		cfg.SamplingThreshold = *v
	}
	if v := config.AppConfig.TraceMaxAgeDays; v != nil {
		cfg.MaxAgeDays = *v
	}
	if v := config.AppConfig.TraceMaxRows; v != nil {
		cfg.MaxRows = *v
	}
	if v := config.AppConfig.TraceVacuumThresholdMB; v != nil {
		cfg.VacuumThresholdMB = *v
	}
	return cfg
}

// UpdateTraceConfig 部分更新追踪配置（任意字段可为 nil 表示不修改）
//
// 注意：Wails 不支持 *int，前端传 -1 表示"不修改"，传 0 或正数视为期望值。
func (a *App) UpdateTraceConfig(samplingThreshold, maxAgeDays, maxRows, vacuumThresholdMB int) (TraceConfigResponse, error) {
	if samplingThreshold >= 0 {
		v := samplingThreshold
		config.AppConfig.TraceSamplingThreshold = &v
	}
	if maxAgeDays >= 0 {
		v := maxAgeDays
		config.AppConfig.TraceMaxAgeDays = &v
	}
	if maxRows >= 0 {
		v := maxRows
		config.AppConfig.TraceMaxRows = &v
	}
	if vacuumThresholdMB >= 0 {
		v := vacuumThresholdMB
		config.AppConfig.TraceVacuumThresholdMB = &v
	}
	if err := config.Save(); err != nil {
		return TraceConfigResponse{}, fmt.Errorf("persist failed: %w", err)
	}
	return a.GetTraceConfig(), nil
}

// CleanupTraces 立即触发一次清理；参数 <=0 表示使用 config 当前值
func (a *App) CleanupTraces(maxAgeDays, maxRows int) (int, error) {
	store := tracer.StoreRef()
	if store == nil {
		return 0, fmt.Errorf("tracer not initialized")
	}
	if maxAgeDays <= 0 {
		if v := config.AppConfig.TraceMaxAgeDays; v != nil {
			maxAgeDays = *v
		}
	}
	if maxRows <= 0 {
		if v := config.AppConfig.TraceMaxRows; v != nil {
			maxRows = *v
		}
	}
	return store.Cleanup(maxAgeDays, maxRows)
}

type GoroutineInfo struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Stack string `json:"stack"`
}

// GetGoroutinesProfile 获取 Goroutine 堆栈信息用于可视化
func (a *App) GetGoroutinesProfile() []GoroutineInfo {
	bufBytes := make([]byte, 2<<20)
	n := runtime.Stack(bufBytes, true)
	stackText := string(bufBytes[:n])

	var result []GoroutineInfo
	var currentGoroutine *GoroutineInfo
	var currentStack strings.Builder

	lines := strings.Split(stackText, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "goroutine ") {
			if currentGoroutine != nil {
				currentGoroutine.Stack = currentStack.String()
				result = append(result, *currentGoroutine)
			}
			parts := strings.SplitN(line, "[", 2)
			id := strings.TrimSpace(strings.TrimPrefix(parts[0], "goroutine "))
			state := ""
			if len(parts) > 1 {
				state = strings.TrimSuffix(parts[1], "]:")
			}
			currentGoroutine = &GoroutineInfo{ID: id, State: state}
			currentStack.Reset()
		} else if currentGoroutine != nil {
			if line != "" {
				currentStack.WriteString(line + "\n")
			}
		}
	}
	if currentGoroutine != nil {
		currentGoroutine.Stack = currentStack.String()
		result = append(result, *currentGoroutine)
	}

	return result
}

type MemoryDetails struct {
	Alloc        uint64 `json:"alloc"`
	TotalAlloc   uint64 `json:"totalAlloc"`
	Sys          uint64 `json:"sys"`
	NumGC        uint32 `json:"numGC"`
	NextGC       uint64 `json:"nextGC"`
	PauseTotalNs uint64 `json:"pauseTotalNs"`
}

// GetMemoryDetails 获取详细内存统计
func (a *App) GetMemoryDetails() MemoryDetails {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return MemoryDetails{
		Alloc:        m.Alloc,
		TotalAlloc:   m.TotalAlloc,
		Sys:          m.Sys,
		NumGC:        m.NumGC,
		NextGC:       m.NextGC,
		PauseTotalNs: m.PauseTotalNs,
	}
}

type ConnectionInfo struct {
	Proto  string `json:"proto"`
	Local  string `json:"local"`
	Remote string `json:"remote"`
	State  string `json:"state"`
}

// GetPortConnections 获取当前客户端进程占用的所有端口连接
func (a *App) GetPortConnections() []ConnectionInfo {
	pid := strconv.Itoa(os.Getpid())
	cmd := exec.Command("netstat", "-ano")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var conns []ConnectionInfo
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasSuffix(line, pid) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 5 {
			conns = append(conns, ConnectionInfo{
				Proto:  fields[0],
				Local:  fields[1],
				Remote: fields[2],
				State:  fields[3],
			})
		} else if len(fields) == 4 && (strings.HasPrefix(fields[0], "UDP")) {
			conns = append(conns, ConnectionInfo{
				Proto:  fields[0],
				Local:  fields[1],
				Remote: fields[2],
				State:  "NONE",
			})
		}
	}
	return conns
}

// ─── 配置编辑器 ───

// GetConfigMeta 返回所有配置字段的元数据（前端用于动态渲染表单）
func (a *App) GetConfigMeta() []config.ConfigFieldMeta {
	return config.GetFieldsMeta()
}

// GetConfig 返回当前 AppConfig 的所有配置项（扁平化 map，前端可直接绑定）
func (a *App) GetConfig() map[string]interface{} {
	result := make(map[string]interface{})

	result["uriScheme"] = config.AppConfig.URIScheme
	result["localServerPort"] = config.AppConfig.LocalServerPort
	result["backendUploadUrl"] = config.AppConfig.BackendUploadURL
	result["backendCompleteUrl"] = config.AppConfig.BackendCompleteURL
	result["backendCommandUrl"] = config.AppConfig.BackendCommandURL
	result["backendCheckUrl"] = config.AppConfig.BackendCheckURL
	result["chunkSize"] = config.AppConfig.ChunkSize
	result["maxWorkers"] = config.AppConfig.MaxWorkers
	result["maxRetries"] = config.AppConfig.MaxRetries
	result["maxFilesInBatch"] = config.AppConfig.MaxFilesInBatch
	result["httpTimeout"] = config.AppConfig.HTTPTimeout
	result["mergeTimeoutMultiplier"] = config.AppConfig.MergeTimeoutMultiplier
	result["dialogTimeout"] = config.AppConfig.DialogTimeout
	result["enableMD5Cache"] = config.AppConfig.EnableMD5Cache

	if config.AppConfig.SendRelativePath != nil {
		result["sendRelativePath"] = *config.AppConfig.SendRelativePath
	} else {
		result["sendRelativePath"] = true
	}
	if config.AppConfig.SendFileSize != nil {
		result["sendFileSize"] = *config.AppConfig.SendFileSize
	} else {
		result["sendFileSize"] = true
	}

	result["traceEnabled"] = config.AppConfig.TraceEnabled
	if config.AppConfig.TraceSamplingThreshold != nil {
		result["traceSamplingThreshold"] = *config.AppConfig.TraceSamplingThreshold
	}
	if config.AppConfig.TraceMaxAgeDays != nil {
		result["traceMaxAgeDays"] = *config.AppConfig.TraceMaxAgeDays
	}
	if config.AppConfig.TraceMaxRows != nil {
		result["traceMaxRows"] = *config.AppConfig.TraceMaxRows
	}
	if config.AppConfig.TraceVacuumThresholdMB != nil {
		result["traceVacuumThresholdMB"] = *config.AppConfig.TraceVacuumThresholdMB
	}

	return result
}

// BackendConnectionInfo 当前生效的后端连接信息，供 Dashboard 顶部展示
type BackendConnectionInfo struct {
	// BaseURL 形如 http://192.168.1.100:8081；解析失败时为空串
	BaseURL string `json:"baseUrl"`
	// Overridden 为 true 表示当前用的是 URI 传入的内存覆盖地址；
	// false 表示用的是 config.json 中的地址
	Overridden bool `json:"overridden"`
}

// GetBackendConnectionInfo 返回当前实际连接的后端地址（scheme://host:port）及其来源。
// Dashboard 顶部用它显示"当前连接后端"。
func (a *App) GetBackendConnectionInfo() BackendConnectionInfo {
	return BackendConnectionInfo{
		BaseURL:    config.EffectiveBackendBase(),
		Overridden: config.GetBackendOverrideBase() != "",
	}
}

// GetConfigPath 返回当前 config.json 文件的完整路径
func (a *App) GetConfigPath() string {
	return filepath.Join(config.AppSupportDir, "config.json")
}

// SaveConfig 接收前端提交的配置 map，更新 AppConfig 并持久化到 config.json
func (a *App) SaveConfig(newConfig map[string]interface{}) error {
	log.Println("[ConfigEditor] 收到保存配置请求")

	// 将 map 序列化为 JSON，再反序列化到 Config 结构体
	// 这样可以安全地利用 JSON 标签做字段匹配
	data, err := json.Marshal(newConfig)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	var updated config.Config
	if err := json.Unmarshal(data, &updated); err != nil {
		return fmt.Errorf("反序列化配置失败: %w", err)
	}

	// 对指针字段做 nil 保护（前端发来的 bool 直接变成了 *bool 的 zero value）
	if updated.SendRelativePath == nil {
		v := true
		updated.SendRelativePath = &v
	}
	if updated.SendFileSize == nil {
		v := true
		updated.SendFileSize = &v
	}
	if updated.TraceSamplingThreshold == nil {
		v := 50000
		updated.TraceSamplingThreshold = &v
	}
	if updated.TraceMaxAgeDays == nil {
		v := 7
		updated.TraceMaxAgeDays = &v
	}
	if updated.TraceMaxRows == nil {
		v := 1000000
		updated.TraceMaxRows = &v
	}
	if updated.TraceVacuumThresholdMB == nil {
		v := 512
		updated.TraceVacuumThresholdMB = &v
	}
	if updated.TraceModuleEnabled == nil {
		updated.TraceModuleEnabled = map[string]bool{}
	}

	// 基本校验
	if updated.LocalServerPort <= 0 || updated.LocalServerPort > 65535 {
		return fmt.Errorf("本地服务端口必须在 1-65535 之间")
	}
	if updated.ChunkSize <= 0 {
		return fmt.Errorf("分片大小必须大于 0")
	}
	if updated.MaxRetries < 0 {
		return fmt.Errorf("最大重试次数不能为负数")
	}
	if updated.HTTPTimeout <= 0 {
		return fmt.Errorf("HTTP 超时时间必须大于 0")
	}
	if updated.MergeTimeoutMultiplier <= 0 {
		return fmt.Errorf("合并超时倍率必须大于 0")
	}

	// 更新全局配置
	config.AppConfig = updated

	// 持久化到文件
	if err := config.Save(); err != nil {
		return fmt.Errorf("保存配置文件失败: %w", err)
	}

	log.Println("[ConfigEditor] 配置保存成功")
	return nil
}

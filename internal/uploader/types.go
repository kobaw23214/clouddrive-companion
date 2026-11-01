package uploader

import (
	"clouddrive-companion-withWails2/internal/utils"
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// TaskInfo 任务信息
type TaskInfo struct {
	TaskId          string `json:"taskId"`
	TaskName        string `json:"taskName"`
	UserId          string `json:"userId"`
	UserName        string `json:"userName"`
	Dir             string `json:"dir"`
	SelectedIndices []int  `json:"selectedIndices"`
	TotalSize       int64  `json:"totalSize,omitempty"`

	// 扫描链路并入用：HandleConfirmUpload 通过 server.ConsumeScanTrace 拿到，
	// 透传给 NewManager。Manager.Start 拿到后会复用 traceId 并把 scan trace
	// 整体并入 upload trace（RetagTrace），最终 trace 列表里只显示 1 条"文件上传"。
	// 续传 / 没有匹配到扫描会话时为空，走原行为创建独立 trace。
	ScanTraceId string `json:"scanTraceId,omitempty"`
	ScanSpanId  string `json:"scanSpanId,omitempty"`
}

// TaskProgress 任务进度
type TaskProgress struct {
	TaskId         string         `json:"taskId"`
	TaskName       string         `json:"taskName"`
	Status         string         `json:"status"`
	TotalSize      int64          `json:"totalSize"`
	UploadedSize   int64          `json:"uploadedSize"`
	TotalFiles     int            `json:"totalFiles"`
	CompletedFiles int            `json:"completedFiles"`
	FailedFiles    int            `json:"failedFiles"`
	Progress       float64        `json:"progress"`
	Files          []FileProgress `json:"files"`

	// TraceId 用于历史记录卡片"查看链路"按钮跳转。仅历史 / 已结束任务会写入；
	// 活跃任务不主动填充（前端用不到）。来源：HistoryRecord.TraceId（持久化的
	// 任务最终 traceId，已包含 retag 后并入的扫描 span）。
	TraceId string `json:"traceId,omitempty"`
}

// FileProgress 文件进度
type FileProgress struct {
	FileName     string  `json:"fileName"`
	RelativePath string  `json:"relativePath"`
	// AbsolutePath 文件在本地的绝对路径，用于"打开所在文件夹"等功能。
	// 老的历史记录（files_json）没有该字段，前端需做空值兜底。
	AbsolutePath string  `json:"absolutePath,omitempty"`
	Status       string  `json:"status"` // "UPLOADING", "MERGING", "COMPLETED", "FAILED", "QUEUED", "PAUSED", "PAUSING"
	TotalSize    int64   `json:"totalSize"`
	UploadedSize int64   `json:"uploadedSize"`
	Progress     float64 `json:"progress"`
}

// TaskManifest 任务清单
type TaskManifest struct {
	TaskInfo TaskInfo             `json:"taskInfo"`
	AllFiles []utils.FullFileInfo `json:"allFiles"`
}

// UploadStats 上传统计
type UploadStats struct {
	totalFiles     int64
	completedFiles int64
	failedFiles    int64
	totalBytes     int64
	uploadedBytes  int64
	startTime      time.Time
}

// Manager 上传管理器
type Manager struct {
	taskId    string
	taskName  string
	userId    string
	userName  string
	dir       string
	totalSize int64
	files     []utils.FullFileInfo
	semaphore chan struct{}
	errorChan chan error
	wg        sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc
	failed    int32
	completed int32

	isPaused     atomic.Bool
	resumeSignal chan bool
	pauseMutex   sync.Mutex

	filePauseStates sync.Map

	taskManifestDir string
	logFileMutex    sync.Mutex

	allTaskFiles       []FileProgress
	totalUploadedBytes atomic.Int64
	fileProgress       *sync.Map

	// ─ 链路追踪 ─
	// 由 Manager.Start 在执行开始时填充。每次任务运行（含续传）都会重新生成 traceId 与 runSeq。
	// taskSpanId 是 upload_task 顶层 span，所有 upload_file / notify_complete 都挂在它下面。
	traceId    string
	runSeq     int
	taskSpanId string

	// 扫描链路并入：由 NewManager 从 TaskInfo 中接收。Start() 拿到非空时，
	// 会复用该 traceId（不调 NewTraceId），再通过 RetagTrace 把扫描那边的
	// span 全改成 upload 类型，达到"扫描 + 上传 = 一条 trace"的效果。
	scanTraceId string
	scanSpanId  string
}

type filePauseState struct {
	isPaused     atomic.Bool
	resumeSignal chan bool
	pauseMutex   sync.Mutex
}

// PagedFileResult 分页文件结果
type PagedFileResult struct {
	Files []FileProgress `json:"files"`
	Total int            `json:"total"`
}

// CommandResponse 命令响应
type CommandResponse struct {
	Command string `json:"command"`
}

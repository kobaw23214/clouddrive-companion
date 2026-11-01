// Package tracer 提供任务级链路追踪能力：以 trace_id 为单位，按 parent-child
// 嵌套记录文件扫描、上传任务、单文件、分片等阶段的耗时与状态。
//
// 数据结构与持久化层（store）解耦，本文件只定义跨子模块共享的类型与枚举。
package tracer

// ─── 受控的 stage 枚举（业务侧只应使用这些常量作为 stage 入参）───
const (
	StageFileScan       = "file_scan"
	StageMetadataSend   = "metadata_send"
	StageUploadTask     = "upload_task"
	StageUploadFile     = "upload_file"
	StageMD5Calc        = "md5_calc"
	StageCheckChunks    = "check_chunks"
	StageChunkUpload    = "chunk_upload"
	StageServerMerge    = "server_merge"
	StageNotifyComplete = "notify_complete"
	StageOther          = "other"
)

// ─── traceType 枚举 ───
const (
	TraceTypeUpload = "upload"
	TraceTypeScan   = "scan"
)

// Span 单条追踪记录。运行时活跃表与持久化行复用同一结构。
//
// 设计要点：
//   - 时间字段统一用毫秒级 Unix 时间戳，方便前端 ECharts 直接消费
//   - ErrorMessage 在 finish 阶段截断到 4 KB（见 tracer.truncate）
//   - Extra 是 JSON 字符串而非 map[string]interface{}，避免业务侧每次构造都触发反射
type Span struct {
	// ID 主键。运行时活跃 span 该字段为 0，落库后由 SQLite 填充。
	ID int64 `json:"id,omitempty"`

	// SpanID 32 位十六进制字符串（crypto/rand 生成的 16 字节），全局唯一。
	SpanID string `json:"spanId"`

	// ParentSpanID 顶层 span 为空字符串。
	ParentSpanID string `json:"parentSpanId"`

	// TraceID 链路 ID。同一任务的多次执行会有不同的 traceId（见 NewTraceId）。
	TraceID string `json:"traceId"`

	// TaskID 关联到 uploader.Manager.taskId，可空（如独立的 file_scan trace）。
	TaskID string `json:"taskId"`

	// RunSeq 同一 taskId 的第几次执行。新任务为 1，续传/重试递增。
	RunSeq int `json:"runSeq"`

	// TraceType 链路类型，对应 ModuleManager 的 code（如 "upload" / "scan"）。
	TraceType string `json:"traceType"`

	// Stage 受控枚举之一；未注册的 stage 前端按 StageOther 处理。
	Stage string `json:"stage"`

	// Filename 关联文件相对路径，可空。
	Filename string `json:"filename,omitempty"`

	// StartMs / EndMs / DurationMs 毫秒。EndMs == 0 表示进行中（活跃 span）。
	StartMs    int64 `json:"startMs"`
	EndMs      int64 `json:"endMs"`
	DurationMs int64 `json:"durationMs"`

	// Success 是否成功。失败时 ErrorMessage 非空。
	Success bool `json:"success"`

	// ErrorMessage 失败原因；最大 4 KB，超出截断并标记。
	ErrorMessage string `json:"errorMessage,omitempty"`

	// Extra 业务扩展信息（JSON 字符串），如分片号、采样率、retryOf 等。
	Extra string `json:"extra,omitempty"`

	// CreatedAtMs 入队时的毫秒时间戳，用于按时间清理。
	CreatedAtMs int64 `json:"createdAtMs"`
}

// TraceModule 可开关的追踪模块。一个 traceType 对应一个 module。
type TraceModule struct {
	Code        string `json:"code"`        // 与 traceType 一致，如 "upload"
	Name        string `json:"name"`        // 中文显示名
	Description string `json:"description"` // 用于 UI 上的二级说明
	Enabled     bool   `json:"enabled"`     // 当前是否启用
}

// TraceSummary GET /task-traces/list 单行返回值。
//
// 由 store_query 通过 GROUP BY trace_id 聚合得到；taskName 由 handler 在
// 拼装阶段从 manifest 文件读取后注入，store 不感知。
type TraceSummary struct {
	TraceID             string `json:"traceId"`
	TaskID              string `json:"taskId,omitempty"`
	RunSeq              int    `json:"runSeq"`
	TraceType           string `json:"traceType"`
	TaskName            string `json:"taskName,omitempty"`
	StartMs             int64  `json:"startMs"`
	EndMs               int64  `json:"endMs"`
	DurationMs          int64  `json:"durationMs"`
	SpanCount           int    `json:"spanCount"`
	HasFailure          bool   `json:"hasFailure"`
	TotalSelfDurationMs int64  `json:"totalSelfDurationMs"`
}

// TraceConfigPatch PATCH /task-traces/config 的请求体。
//
// 全部字段可选；handler 仅替换非 nil 的字段，避免覆盖未传字段。
type TraceConfigPatch struct {
	SamplingThreshold *int `json:"traceSamplingThreshold,omitempty"`
	MaxAgeDays        *int `json:"traceMaxAgeDays,omitempty"`
	MaxRows           *int `json:"traceMaxRows,omitempty"`
	VacuumThresholdMB *int `json:"traceVacuumThresholdMB,omitempty"`
}

// ListTracesQuery GET /task-traces/list 的查询参数（Go 端内部结构）。
type ListTracesQuery struct {
	TraceType string
	TaskID    string
	FromMs    int64
	ToMs      int64
	Page      int
	Size      int
}

package config

type Config struct {
	URIScheme              string `json:"uriScheme"`
	ChunkSize              int64  `json:"chunkSize"`
	BackendUploadURL       string `json:"backendUploadUrl"`
	BackendCompleteURL     string `json:"backendCompleteUrl"`
	BackendCommandURL      string `json:"backendCommandUrl"`
	BackendCheckURL        string `json:"backendCheckUrl"`
	MaxWorkers             int    `json:"maxWorkers"`
	MaxRetries             int    `json:"maxRetries"`
	HTTPTimeout            int    `json:"httpTimeout"`
	MergeTimeoutMultiplier int    `json:"mergeTimeoutMultiplier"`
	EnableMD5Cache         bool   `json:"enableMD5Cache"`
	MaxFilesInBatch        int    `json:"maxFilesInBatch"`
	LocalServerPort        int    `json:"localServerPort"`
	DialogTimeout          int    `json:"dialogTimeout"`
	SendRelativePath       *bool  `json:"sendRelativePath"`
	SendFileSize           *bool  `json:"sendFileSize"`

	// ── 出站 HTTP 请求日志 ──
	// OutboundHttpLog 出站请求日志总开关，默认 true（老 config.json 缺字段时视为开启）
	OutboundHttpLog bool `json:"outboundHttpLog"`
	// OutboundHttpLogPoller 是否记录命令轮询请求（频率高、无 taskId），默认 true
	OutboundHttpLogPoller bool `json:"outboundHttpLogPoller"`

	// ── 链路追踪（task tracing） ──
	// TraceEnabled 全局总开关，默认 true
	TraceEnabled bool `json:"traceEnabled"`
	// TraceModuleEnabled 各模块开关，缺省视为启用；持久化到 config.json
	TraceModuleEnabled map[string]bool `json:"traceModuleEnabled"`
	// TraceSamplingThreshold 采样阈值（span 数）。指针化以区分"未配置（nil）"和"显式 0（禁用采样）"
	TraceSamplingThreshold *int `json:"traceSamplingThreshold"`
	// TraceMaxAgeDays 保留天数；nil 走默认
	TraceMaxAgeDays *int `json:"traceMaxAgeDays"`
	// TraceMaxRows 最大行数；nil 走默认
	TraceMaxRows *int `json:"traceMaxRows"`
	// TraceVacuumThresholdMB 数据库超过该 MB 触发 VACUUM
	TraceVacuumThresholdMB *int `json:"traceVacuumThresholdMB"`
}

var (
	defaultSendPath = true
	defaultSendSize = false

	defaultTraceSamplingThreshold = 50_000
	defaultTraceMaxAgeDays        = 7
	defaultTraceMaxRows           = 1_000_000
	defaultTraceVacuumThresholdMB = 512
)

var DefaultConfig = Config{
	URIScheme:              "my-clouddrive",
	ChunkSize:              5 * 1024 * 1024,
	BackendUploadURL:       "http://localhost:8081/dataImport/upload/chunk",
	BackendCompleteURL:     "http://localhost:8081/dataImport/upload/complete",
	BackendCommandURL:      "http://localhost:8081/dataImport/upload/command",
	BackendCheckURL:        "http://localhost:8081/dataImport/upload/check",
	MaxWorkers:             8,
	MaxRetries:             3,
	HTTPTimeout:            60,
	MergeTimeoutMultiplier: 20,
	EnableMD5Cache:         true,
	MaxFilesInBatch:        1000,
	LocalServerPort:        34567,
	DialogTimeout:          600,
	SendRelativePath:       &defaultSendPath,
	SendFileSize:           &defaultSendSize,

	// 出站 HTTP 请求日志默认值
	OutboundHttpLog:       true,
	OutboundHttpLogPoller: true,

	// 链路追踪默认值
	TraceEnabled:           true,
	TraceModuleEnabled:     map[string]bool{},
	TraceSamplingThreshold: &defaultTraceSamplingThreshold,
	TraceMaxAgeDays:        &defaultTraceMaxAgeDays,
	TraceMaxRows:           &defaultTraceMaxRows,
	TraceVacuumThresholdMB: &defaultTraceVacuumThresholdMB,
}

// ConfigFieldMeta 配置字段元数据，供前端配置编辑器动态渲染
type ConfigFieldMeta struct {
	Key          string      `json:"key"`          // JSON key
	Label        string      `json:"label"`        // 中文名称
	Type         string      `json:"type"`         // 字段类型: string / int / int64 / bool
	DefaultValue interface{} `json:"defaultValue"` // 默认值
	Description  string      `json:"description"`  // 中文描述
	Group        string      `json:"group"`        // 分组名
	GroupOrder   int         `json:"groupOrder"`   // 分组排序
}

// GetFieldsMeta 返回所有可编辑配置字段的元数据
func GetFieldsMeta() []ConfigFieldMeta {
	return []ConfigFieldMeta{
		// ── 基础连接配置 ──
		{Key: "uriScheme", Label: "URI 协议", Type: "string", DefaultValue: "my-clouddrive",
			Description: "浏览器通过此 URI 协议拉起客户端（如 my-clouddrive://...），修改后需同步更新注册表",
			Group: "基础连接", GroupOrder: 1},
		{Key: "localServerPort", Label: "本地服务端口", Type: "int", DefaultValue: 34567,
			Description: "客户端本地 HTTP 服务监听端口，浏览器端通过此端口与客户端通信",
			Group: "基础连接", GroupOrder: 1},

		// ── 后端地址配置 ──
		{Key: "backendUploadUrl", Label: "分片上传地址", Type: "string", DefaultValue: "http://localhost:8081/dataImport/upload/chunk",
			Description: "后端分片上传接口的完整 URL",
			Group: "后端地址", GroupOrder: 2},
		{Key: "backendCompleteUrl", Label: "上传完成地址", Type: "string", DefaultValue: "http://localhost:8081/dataImport/upload/complete",
			Description: "文件上传完成后通知后端的接口 URL",
			Group: "后端地址", GroupOrder: 2},
		{Key: "backendCommandUrl", Label: "命令轮询地址", Type: "string", DefaultValue: "http://localhost:8081/dataImport/upload/command",
			Description: "客户端轮询后端获取任务指令的接口 URL",
			Group: "后端地址", GroupOrder: 2},
		{Key: "backendCheckUrl", Label: "分片校验地址", Type: "string", DefaultValue: "http://localhost:8081/dataImport/upload/check",
			Description: "上传前检查分片是否已存在（秒传检测）的接口 URL",
			Group: "后端地址", GroupOrder: 2},

		// ── 上传性能配置 ──
		{Key: "chunkSize", Label: "分片大小", Type: "int64", DefaultValue: int64(5 * 1024 * 1024),
			Description: "文件分片大小（字节），默认 5MB。增大可减少请求次数但增加单次传输时间",
			Group: "上传性能", GroupOrder: 3},
		{Key: "maxWorkers", Label: "最大并发数", Type: "int", DefaultValue: 8,
			Description: "同时上传的最大线程数。设为 0 则自动使用 CPU 核心数",
			Group: "上传性能", GroupOrder: 3},
		{Key: "maxRetries", Label: "最大重试次数", Type: "int", DefaultValue: 3,
			Description: "单个分片上传失败后的最大重试次数",
			Group: "上传性能", GroupOrder: 3},
		{Key: "maxFilesInBatch", Label: "批次最大文件数", Type: "int", DefaultValue: 1000,
			Description: "单个上传批次中允许的最大文件数量",
			Group: "上传性能", GroupOrder: 3},

		// ── 超时配置 ──
		{Key: "httpTimeout", Label: "HTTP 超时", Type: "int", DefaultValue: 60,
			Description: "标准 HTTP 请求超时时间（秒）",
			Group: "超时配置", GroupOrder: 4},
		{Key: "mergeTimeoutMultiplier", Label: "合并超时倍率", Type: "int", DefaultValue: 20,
			Description: "合并请求的超时倍率，最终合并超时 = HTTP超时 × 此值",
			Group: "超时配置", GroupOrder: 4},
		{Key: "dialogTimeout", Label: "对话框超时", Type: "int", DefaultValue: 600,
			Description: "文件选择对话框超时时间（秒）（保留字段，暂未启用）",
			Group: "超时配置", GroupOrder: 4},

		// ── 功能开关 ──
		{Key: "enableMD5Cache", Label: "启用 MD5 缓存", Type: "bool", DefaultValue: true,
			Description: "开启后会缓存已计算的文件 MD5，避免重复计算同一文件的哈希值",
			Group: "功能开关", GroupOrder: 5},
		{Key: "sendRelativePath", Label: "发送相对路径", Type: "bool", DefaultValue: true,
			Description: "上传时是否将文件的相对路径信息一并发送给后端",
			Group: "功能开关", GroupOrder: 5},
		{Key: "sendFileSize", Label: "发送文件大小", Type: "bool", DefaultValue: true,
			Description: "上传时是否将文件大小信息一并发送给后端",
			Group: "功能开关", GroupOrder: 5},

		// ── 链路追踪配置 ──
		{Key: "traceEnabled", Label: "追踪总开关", Type: "bool", DefaultValue: true,
			Description: "启用/禁用全局链路追踪功能",
			Group: "链路追踪", GroupOrder: 6},
		{Key: "traceSamplingThreshold", Label: "采样阈值", Type: "int", DefaultValue: 50000,
			Description: "span 数量超过此阈值后开始采样，减少存储开销",
			Group: "链路追踪", GroupOrder: 6},
		{Key: "traceMaxAgeDays", Label: "数据保留天数", Type: "int", DefaultValue: 7,
			Description: "追踪数据最多保留的天数，超过后自动清理",
			Group: "链路追踪", GroupOrder: 6},
		{Key: "traceMaxRows", Label: "最大行数", Type: "int", DefaultValue: 1000000,
			Description: "追踪数据库最大行数，超过后自动清理旧数据",
			Group: "链路追踪", GroupOrder: 6},
		{Key: "traceVacuumThresholdMB", Label: "VACUUM 阈值", Type: "int", DefaultValue: 512,
			Description: "追踪数据库文件超过此大小（MB）时触发 VACUUM 压缩",
			Group: "链路追踪", GroupOrder: 6},
	}
}

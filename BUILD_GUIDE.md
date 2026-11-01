# 打包指南

## 重构后的打包方法

### 方法 1: 使用 Wails 打包 (推荐)

**在项目根目录下运行**:

```bash
wails build
```

Wails 会自动:
- 读取 `wails.json` 配置
- 找到 `cmd/companion/main.go` 作为入口
- 编译前端资源 (在 `cmd/companion/frontend/` 下)
- 打包生成可执行文件到 `build/bin/` 目录

### 方法 2: 使用 Go 编译

```bash
# 基本编译
go build -o clouddrive-companion.exe ./cmd/companion

# 优化编译 (减小体积)
go build -ldflags="-s -w -H windowsgui" -trimpath -o clouddrive-companion.exe ./cmd/companion

# 使用 UPX 进一步压缩 (可选)
upx --best --lzma clouddrive-companion.exe
```

### 方法 3: 开发模式

```bash
wails dev
```

## 重要配置说明

### wails.json 配置

已更新为:
```json
{
  "frontend:dir": "./cmd/companion/frontend",
  "wailsjsdir": "./cmd/companion/frontend",
  "main:build": "go build -o bin/companion.exe ./cmd/companion"
}
```

### 目录结构

```
项目根目录/
├── cmd/companion/
│   ├── main.go          ← 入口文件
│   ├── icon.ico         ← 图标资源
│   └── frontend/        ← 前端资源
│       ├── dist/        ← 编译后的前端
│       ├── src/         ← 前端源码
│       └── package.json
├── internal/            ← Go 业务代码
├── build/               ← 编译输出
│   └── bin/
│       └── clouddrive-companion-withWails2.exe
├── wails.json
└── go.mod
```

## 已删除的旧文件

以下文件已从根目录删除 (已迁移到 `internal/` 包中):
- ✅ `main.go` → `cmd/companion/main.go`
- ✅ `app.go` → `internal/app/`
- ✅ `config.go` → `internal/config/`
- ✅ `companion_api.go` → `internal/api/`
- ✅ `http_server.go` → `internal/server/` 和 `internal/uploader/`
- ✅ `http_server.zip` (备份文件)

## 打包前检查清单

- [x] 确保 `cmd/companion/frontend/dist/` 有编译好的前端文件
- [x] 确保 `cmd/companion/icon.ico` 存在
- [x] 确保 `go.mod` 依赖完整
- [x] 旧的根目录 `.go` 文件已删除

## 运行打包

### 第一次打包

```bash
# 1. 确保在项目根目录(仓库克隆/解压后的目录)
cd clouddrive-companion

# 2. 安装前端依赖 (如果还没有)
cd cmd/companion/frontend
npm install
cd ../../..

# 3. 执行打包
wails build
```

### 后续打包

```bash
# 在项目根目录直接运行
wails build
```

## 打包产物

编译成功后，可执行文件位于:
```
build/bin/clouddrive-companion-withWails2.exe
```

## 常见问题

### Q: 报错找不到 frontend?
**A**: 确保 `cmd/companion/frontend/` 目录存在并包含前端文件

### Q: 报错找不到 icon.ico?
**A**: 确保 `cmd/companion/icon.ico` 文件存在

### Q: 编译失败?
**A**: 
1. 确保在项目根目录执行
2. 检查 `go.mod` 依赖是否完整: `go mod tidy`
3. 清理缓存重试: `go clean -cache`

### Q: 想要开发模式实时调试?
**A**: 使用 `wails dev` 命令

## 部署说明

打包后的 `.exe` 文件需要配合以下文件运行:
- `config.json` (配置文件，首次运行自动生成)
- `tasks/` 目录 (任务清单，自动创建)

建议发布时提供示例 `config.json` 文件。

## 注册 URI Scheme (可选)

```bash
.\clouddrive-companion-withWails2.exe --register
```

这样可以通过 `my-clouddrive://` 协议启动应用。

## 运行时数据目录

应用首次启动后会在 `AppSupportDir` 自动创建以下子目录：

| 子目录 | 用途 |
|--------|------|
| `tasks/` | 任务清单（manifest）+ 完成日志 + 失败日志 |
| `http_log/` | HTTP 请求日志数据库（`http_logs.db`） |
| `task_trace/` | **任务级链路追踪数据库（`traces.db`）** |
| `runtime/` | WebView2 运行时（如果启用本地运行时） |

`AppSupportDir` 路径：
- **Windows**：与 `clouddrive-companion.exe` 同级
- **Linux**：`~/.config/clouddrive-companion/`

## 项目目录结构

```
项目根/
├── main.go                                    入口
├── icon.ico
├── config.json                                运行时生成
├── internal/
│   ├── api/                                   后端 HTTP 客户端
│   ├── app/                                   Wails App + Bind 方法
│   ├── config/                                配置加载与持久化
│   ├── server/                                本地 HTTP server + httplog/applog
│   ├── tracer/                                ★ 任务级链路追踪
│   │   ├── tracer.go                          内核 API
│   │   ├── store.go / store_query.go          SQLite 持久化层
│   │   ├── module.go / sampler.go             开关 + 采样
│   │   ├── handlers.go                        /task-traces/* HTTP 端点
│   │   ├── types.go / config_bridge.go        类型与配置桥接
│   │   └── e2e/                               端到端集成测试
│   ├── tray/                                  系统托盘
│   ├── uploader/                              上传管理器
│   └── utils/                                 工具函数
├── frontend/
│   ├── src/components/
│   │   ├── Dashboard.vue                      主面板
│   │   ├── FileSelector.vue
│   │   ├── ScanProgressOverlay.vue
│   │   └── trace/                             ★ 任务级链路追踪 UI
│   │       ├── TraceModal.vue                 主入口 + 状态机
│   │       ├── TraceListView.vue              视图 1：任务列表
│   │       ├── TraceTopSpansView.vue          视图 2：顶层 span + 甘特图
│   │       ├── TraceChildSpansView.vue        视图 3：子 span + 甘特图
│   │       ├── TraceGantt.vue                 ECharts 甘特图组件
│   │       ├── TraceSettingsPanel.vue         采集设置面板
│   │       ├── TraceDetailDrawer.vue          右侧详情抽屉
│   │       ├── api.js                         Wails Bind 封装
│   │       ├── tokens.css                     CSS 设计 Token
│   │       ├── stage_meta.js                  stage 元数据 + 格式化
│   │       └── gantt_helpers.js               甘特图纯函数工具
│   └── wailsjs/                               Wails 自动生成的绑定
└── doc/
    ├── start.md
    ├── task-tracing.md                        ★ 任务追踪用户文档
    └── ...
```

## 任务级链路追踪

详见 [`doc/task-tracing.md`](doc/task-tracing.md)。

**前端依赖**：项目已加入 `echarts@^5`（按需 import 模块，仅打开"链路追踪"modal 时才加载）。

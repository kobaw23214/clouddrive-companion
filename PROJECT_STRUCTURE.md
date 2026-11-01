# 项目结构说明

## 📁 目录结构总览

```
clouddrive-companion-withWails2/
│
├── main.go                     # 程序入口
├── icon.ico                    # 应用图标
├── wails.json                  # Wails 配置文件
├── go.mod                      # Go 模块定义
├── go.sum                      # Go 依赖锁定
│
├── internal/                   # 核心业务逻辑（Go）
│   ├── api/                    # API 客户端
│   ├── app/                    # Wails 应用核心
│   ├── config/                 # 配置管理
│   ├── server/                 # HTTP 服务器
│   ├── tray/                   # 系统托盘
│   ├── uploader/               # 上传模块
│   └── utils/                  # 工具函数
│
├── frontend/                   # Wails 前端（Vue）
│   ├── src/                    # 源代码
│   ├── dist/                   # 编译输出
│   └── wailsjs/                # Wails Go 绑定
│
├── frontend_browser_sse/       # 独立浏览器前端（Vue + SSE）
│   └── src/                    # 源代码
│
├── build/                      # 编译输出目录
│   ├── bin/                    # 可执行文件
│   └── setup/                  # 安装包输出
│
├── runtime/                    # WebView2 运行时（可选）
├── setup_inno/                 # Inno Setup 安装脚本
└── doc/                        # 文档资源
```

---

## 📂 核心目录详解

### `internal/` - Go 后端核心

| 目录 | 文件 | 说明 |
|------|------|------|
| `api/` | `client.go` | 与后端 Java 服务通信的 HTTP 客户端 |
| `app/` | `app.go` | Wails 应用主逻辑、生命周期管理 |
| | `file_selector.go` | 文件/文件夹选择器，元数据收集 |
| `config/` | `config.go` | 配置结构定义、应用目录管理 |
| | `loader.go` | 配置文件加载（JSON） |
| `server/` | `server.go` | HTTP 服务器启动、路由注册 |
| | `handlers.go` | HTTP 接口处理函数 |
| `tray/` | `tray.go` | Windows 系统托盘图标和菜单 |
| `uploader/` | `types.go` | 上传相关的类型定义 |
| | `manager.go` | 上传任务管理器 |
| | `task.go` | 单个上传任务逻辑 |
| | `chunk.go` | 文件分片上传处理 |
| | `poller.go` | 后端状态轮询 |
| `utils/` | `file.go` | 文件操作工具函数 |
| | `md5.go` | MD5 计算（支持大文件分块） |
| | `network.go` | 网络状态监控（端口、连接数） |

---

### `frontend/` - Wails 嵌入式前端

```
frontend/
├── src/
│   ├── App.vue                 # 根组件
│   ├── main.js                 # 入口文件
│   ├── style.css               # 全局样式
│   ├── components/
│   │   ├── Dashboard.vue       # 主面板（任务列表、状态监控）
│   │   └── FileSelector.vue    # 文件选择组件
│   └── assets/                 # 静态资源（字体、图片）
├── wailsjs/                    # Wails 自动生成的 Go 绑定
├── dist/                       # 编译输出（嵌入到 Go 二进制）
├── vite.config.js              # Vite 配置
└── package.json                # NPM 依赖
```

---

### `frontend_browser_sse/` - 独立浏览器前端

独立的 Vue + TypeScript 前端，通过 SSE 接收上传进度，可部署到 Web 服务器。

```
frontend_browser_sse/
├── src/
│   ├── App.vue
│   ├── main.ts
│   ├── style.css
│   └── components/
│       ├── UploadComponent.vue # 上传组件（SSE 进度）
│       └── HelloWorld.vue
├── vite.config.ts
├── tsconfig.json
└── package.json
```

---

### `setup_inno/` - 安装包脚本

| 文件 | 说明 |
|------|------|
| `go.iss` | Inno Setup 主脚本（包含 WebView2 运行时） |
| `go_old.iss` | 旧版脚本备份 |
| `README.md` | 打包说明 |

---

### `build/` - 编译输出

```
build/
├── bin/
│   ├── clouddrive-companion.exe    # 主程序
│   └── config.json                  # 配置文件
└── setup/
    └── CloudDriveCompanion-Setup.exe  # 安装包
```

---

### `runtime/` - WebView2 运行时

可选目录，用于在未安装 WebView2 的 Windows 系统上运行。

| 文件 | 说明 |
|------|------|
| `msedgewebview2.exe` | WebView2 核心 |
| `*.dll` | 运行时依赖库 |

程序启动时会自动检测：
1. 优先使用 `runtime/` 目录下的本地运行时
2. 回退到系统安装的 WebView2

---

## 📄 根目录文件说明

| 文件 | 说明 |
|------|------|
| `main.go` | 程序入口，Wails 应用初始化 |
| `icon.ico` | 应用图标（嵌入到 exe） |
| `wails.json` | Wails 项目配置 |
| `go.mod` | Go 模块定义 |
| `go.sum` | Go 依赖版本锁定 |
| `README.md` | 项目介绍 |
| `PROJECT_STRUCTURE.md` | 本文件 |

---

## 🔧 配置文件

### `wails.json`

```json
{
  "name": "clouddrive-companion",
  "outputfilename": "clouddrive-companion",
  "frontend:dir": "frontend",
  "frontend:install": "npm install",
  "frontend:build": "npm run build"
}
```

### `config.json`（运行时配置）

```json
{
  "api_base_url": "http://your-backend-server",
  "uri_scheme": "my-clouddrive",
  "listen_port": 34567
}
```

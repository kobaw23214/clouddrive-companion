# CloudDrive Companion

> 云盘上传客户端伴侣程序 - 基于 Wails2 + Vue3 构建的桌面应用

## 📖 简介

CloudDrive Companion 是一个 Windows 桌面应用程序，用于辅助网页端进行大文件上传。它通过 URI Scheme 协议与浏览器通信，支持：

- 📁 **文件/文件夹选择** - 调用系统原生对话框
- 📤 **分片上传** - 支持大文件断点续传
- 🔄 **进度同步** - 通过 SSE 实时推送上传进度
- 🖥️ **系统托盘** - 后台运行，最小化到托盘
- 📊 **状态监控** - 实时显示网络状态、端口占用

## ✨ 功能特性

### 核心功能
- **URI Scheme 启动** - 通过 `my-clouddrive://` 协议从浏览器唤起
- **大文件支持** - 分片上传，支持 GB 级文件
- **断点续传** - 任务中断后可恢复上传
- **追加选择** - 浏览器回传上次已选文件列表，客户端恢复预选勾选（整目录自动折叠）
- **并发控制** - 智能控制并发数，防止端口耗尽

### 界面功能
- 实时上传速度显示
- 任务队列管理（暂停/恢复/取消）
- 网络状态监控（Goroutines、TCP 连接、端口占用）
- 系统托盘菜单

## 🛠️ 技术栈

| 层级 | 技术 |
|------|------|
| 后端 | Go 1.21+ |
| 前端 | Vue 3 + Vite |
| 框架 | Wails v2 |
| UI 渲染 | WebView2 |
| 系统托盘 | systray |

## 📁 项目结构

详细结构请查看 [PROJECT_STRUCTURE.md](./PROJECT_STRUCTURE.md)

```
clouddrive-companion-withWails2/
├── main.go                 # 程序入口
├── internal/               # Go 后端核心模块
├── frontend/               # Wails 嵌入式前端
├── frontend_browser_sse/   # 独立浏览器前端
├── setup_inno/             # 安装包脚本
└── runtime/                # WebView2 运行时（可选）
```

## 🚀 快速开始

### 环境要求

- Go 1.21+
- Node.js 18+
- Wails CLI v2
- Windows 10/11

### 安装 Wails CLI

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

### 开发模式

```bash
# 启动开发服务器（支持热重载）
wails dev
```

### 编译打包

```bash
# 编译生产版本
wails build

# 输出位置：build/bin/clouddrive-companion.exe
```

### 创建安装包

1. 安装 [Inno Setup](https://jrsoftware.org/isinfo.php)
2. 打开 `setup_inno/go.iss`
3. 编译生成安装包

## ⚙️ 配置说明

### 运行时配置 (`config.json`)

```json
{
  "api_base_url": "http://your-backend:8080",
  "uri_scheme": "my-clouddrive",
  "listen_port": 34567
}
```

| 配置项 | 说明 | 默认值 |
|--------|------|--------|
| `api_base_url` | 后端 API 地址 | - |
| `uri_scheme` | URI 协议名称 | `my-clouddrive` |
| `listen_port` | 本地 HTTP 服务端口 | `34567` |

### WebView2 运行时

程序支持两种 WebView2 运行模式：

1. **系统安装版** - 使用系统已安装的 WebView2（推荐）
2. **本地运行时** - 将 WebView2 Fixed Version 放入 `runtime/` 目录

程序启动时会自动检测并选择合适的运行时。

## 📡 API 接口

本地 HTTP 服务（默认端口 34567）提供以下接口：

| 接口 | 方法 | 说明 |
|------|------|------|
| `/ping` | GET | 健康检查 |
| `/get-metadata` | GET | 获取已选择的文件元数据 |
| `/start-upload` | POST | 启动上传任务 |
| `/resume-upload` | POST | 恢复上传任务 |
| `/reselect` | POST | 重新选择文件；带 `preselect` body 时恢复上次已选列表（追加场景，见下文） |
| `/openClient` | POST | 显示客户端主面板 |
| `/pending-tasks` | GET | 获取待续传任务列表 |
| `/shutdown` | POST | 关闭程序 |

### 追加选择（预选恢复）

浏览器端保存用户上次确认的文件列表后，用户点"追加"时把列表回传客户端，客户端打开文件选择器并把上次选过的文件恢复为勾选状态，用户补选后一起提交。

**设计要点：**

- **URI 不携带列表**：`my-clouddrive://select-files` 保持原样（URI 经命令行传递有 32KB 硬上限，装不下几千条路径）。客户端未运行时先 URI 拉起，等 `/ping` 就绪后再 POST 列表，与热启动汇合。
- **契约最小化**：`POST /reselect` 的 body 只需一组 `absolutePath` 字符串（文件/目录均可），`fileName`/`fileSize` 等字段客户端恢复时自行从磁盘获取；空 body 时行为与旧版"重新选择"完全一致。
- **目录折叠**：某目录当前磁盘上的全部常规文件都在列表里时，恢复为"该目录被勾选"一行显示，而不是平铺几百条文件；已删除的文件进 `missing` 返回给浏览器提示。

```json
// POST /reselect
{ "preselect": ["E:/资料/合同/a.pdf", "D:/photos/IMG_001.jpg"] }

// 响应
{ "ok": true, "received": 1523, "dirs": [{ "path": "E:/资料/合同", "fileCount": 1520 }], "fileCount": 2, "missing": [] }
```

> 完整的时序图、契约细节、浏览器集成伪代码和维护注意事项见 [doc/preselect-append-handoff.md](./doc/preselect-append-handoff.md)。

## 🔗 URI Scheme

注册 `my-clouddrive://` 协议后，可通过以下方式唤起程序：

| URI | 说明 |
|-----|------|
| `my-clouddrive://select-files` | 启动程序并打开文件选择对话框 |
| `my-clouddrive://open-client` | 启动程序并直接显示主面板（Dashboard） |
| `my-clouddrive://close-client` | 关闭正在运行的客户端（不显示任何界面） |

```
my-clouddrive://select-files
my-clouddrive://open-client
my-clouddrive://close-client
```

浏览器访问以上链接时，若程序未运行，操作系统会通过注册表找到程序并启动；若已运行，程序会被唤起并显示对应界面。

### 关闭客户端（close-client）

`my-clouddrive://close-client` 用于从浏览器侧关闭客户端，等价于用户在托盘里点击退出。

它的执行过程与另外两个动作不同：操作系统同样会新起一个进程，但这个进程**不会初始化 WebView2、不启动本地 HTTP 服务、不显示任何窗口**，只做一件事——让已运行的实例退出，然后自己也退出。因此调用后不会有窗口闪现。

关闭策略是两级的：

1. **优雅关闭**：调用运行中实例的本地 `/shutdown` 接口，由它自己走退出流程；随后轮询等待端口释放（最多 5 秒）确认已退干净。
2. **强制兜底**：`/shutdown` 连不上（实例本来就没在运行）或返回异常（进程卡死）时，回退到 `taskkill` 结束同名进程及其 WebView2 子进程树。

这样保证 close-client 的语义是「调用完成后客户端一定不在运行」。若客户端原本就没运行，调用是**幂等**的，不会有任何副作用。

> ⚠️ 关闭是立即生效的，**不会等待正在进行的上传任务**。未完成的分片进度已落盘到 `tasks/` 目录，下次启动可从待续传列表恢复。若希望上传完再关，请在浏览器侧先确认任务已完成。

`close-client` 不接受 `server` 参数（携带也会被忽略），因为它不涉及后端调用。

如果只是想让窗口收起来而不退出进程，用托盘图标或窗口关闭按钮即可（窗口关闭默认是隐藏到托盘，不是退出）。

### 通过 URI 传入后端服务器地址（多地部署）

后端服务是多地部署的，不同机房/环境的 IP、端口不同。除了手动修改安装目录下的 `config.json`，现在还支持在浏览器拉起客户端时，通过 URI 的 `server` 查询参数把后端地址直接传给客户端：

```
my-clouddrive://select-files?server=http://192.168.1.100:8081
my-clouddrive://open-client?server=http://192.168.1.100:8081
```

`server` 取值支持以下三种形式（客户端会自动规整）：

| 形式 | 示例 |
|------|------|
| 带协议 + IP + 端口 | `http://192.168.1.100:8081` |
| HTTPS | `https://backend.example.com:8443` |
| 仅 IP + 端口（默认按 http 处理） | `192.168.1.100:8081` |

> 浏览器端拼接时记得对 `server` 的值做 `encodeURIComponent` 编码，避免 `://` 和端口冒号破坏 URI 结构。

**优先级与兼容规则：**

1. **URI 带了 `server`** → 客户端只在**内存**中使用该地址，**优先级高于** `config.json`；进程退出即失效，**不会写回** `config.json`。
2. **URI 未带 `server`** → 客户端沿用 `config.json` 中的地址，即旧版本行为，**完全兼容**。

客户端只用 `server` 的 **协议 + 主机 + 端口** 替换 `config.json` 中四个后端接口地址（chunk / complete / command / check）的 scheme/host 部分，接口**路径保持不变**（如 `/dataImport/upload/chunk`）。因为多地部署通常只换 IP/端口，路径是一致的。

> ⚠️ 内存覆盖在客户端**启动时**从 URI 解析。若客户端已在运行，再次用带 `server` 的 URI 唤起不会刷新已运行实例的内存地址（正常启动流程会先关闭旧实例再以新参数启动，因此一般无需关心）。如需切换到不同后端，建议先退出客户端再用新 `server` 重新拉起。

> 前端示例代码见 [`frontend_browser_sse/src/components/UploadComponent.vue`](./frontend_browser_sse/src/components/UploadComponent.vue) 中的 `BACKEND_SERVER` 常量与 `buildClientUri()` 函数。

## 📝 License

MIT License



这个项目是我的网盘的客户端项目
- wails 的 后端 go代码(internal/)
- wails 的 前端 代码(frontend/)
- 模拟网页端测试用的代码(frontend_browser_sse/)
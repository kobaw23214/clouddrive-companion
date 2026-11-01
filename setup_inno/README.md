# 打包说明（Wails 构建 + Inno Setup 安装包）

本项目需要针对**两类目标系统**分别出包，两条产线的差别集中在三处：Go 工具链、WebView2 运行时目录、安装包的 `RuntimeDir` 参数。

| | Windows 10 / 11 | Windows 7 SP1 / 8 / 8.1 |
|---|---|---|
| 构建目标 | `-Target modern`（默认） | `-Target win7` |
| Go 工具链 | 官方 Go（当前 1.25.x） | Win7 补丁工具链（go-legacy-win7） |
| WebView2 运行时目录 | `runtime/`（最新固定版本） | `runtime_win7/`（**必须** 109.0.1518.x） |
| 安装包参数 | `/DRuntimeDir=runtime`（默认） | `/DRuntimeDir=runtime_win7` |
| 产物可运行范围 | Win10+ | Win7 SP1 ~ Win11 |

---

## 一、快速开始

推荐统一用 `scripts/build.ps1`，它把工具链切换、运行时目录校验、产物验证、安装包编译串在一起。

```powershell
# Win10+ 版本：构建 exe + 自检工具
.\scripts\build.ps1

# Win10+ 版本：连安装包一起出
.\scripts\build.ps1 -Installer

# Win7 版本（需先准备补丁工具链，见第三节）
.\scripts\build.ps1 -Target win7 -Installer

# 不需要自检工具时
.\scripts\build.ps1 -NoSelfCheck
```

产物位置：

- 主程序 `build/bin/clouddrive-companion.exe`
- 环境自检工具 `build/bin/win7-selfcheck.exe`
- 安装包 `build/setup/CloudDriveCompanion-Setup.exe`

脚本会自动做这些校验：工具链是否为补丁版、运行时目录版本号与构建目标是否匹配（打包了 109 却构建 modern，或反过来，都会告警）、产物实际嵌入的 Go 版本。

---

## 二、Windows 10 / 11 打包（常规产线）

### 1. 构建 exe

```powershell
# 用脚本（推荐）
.\scripts\build.ps1

# 或手动
wails build -trimpath
```

### 2. 确保 `runtime/` 存在

项目根目录下 `runtime/` 需包含完整的 WebView2 Fixed Version Runtime（约 150MB），关键文件 `runtime\msedgewebview2.exe` 必须存在。也支持 `runtime\EBWebView\msedgewebview2.exe` 的子目录结构。

当前仓库里是 `144.0.3719.93`。

### 3. 编译安装包

```powershell
# 用脚本
.\scripts\build.ps1 -Installer

# 或手动（在项目根目录执行，go.iss 里 SourceDir=.. 已处理相对路径）
ISCC.exe setup_inno\go.iss
```

`RuntimeDir` 不传时默认为 `runtime`，所以常规产线无需额外参数。

---

## 三、Windows 7 SP1 打包

### 为什么需要单独一条产线

三个互相独立的限制，缺一个都跑不起来：

| 层 | 限制 | 处理方式 |
|---|---|---|
| WebView2 运行时 | 微软 2022-12 宣布 Runtime **109 是支持 Win7/8/8.1 的最后版本**，Win7 上最终版为 109.0.1518.140 | 单独打包 `runtime_win7/`，程序按系统版本选择 |
| Go 工具链 | **Go 1.20 是最后一个支持 Win7 的官方版本**，Go 1.21+ 产物在 Win7 上启动即失败 | 用 Win7 补丁工具链编译，依赖树不动 |
| 前端 | Chromium 109 之后的 JS/CSS 特性不可用 | `frontend/vite.config.js` 已锁 `build.target: 'chrome109'` |

Wails 2.10.2 本身不用改：`go-webview2` 的 `pkg/edge/capabilities.go` 用 `CompareBrowserVersions` 对每个新特性做版本门禁（外部拖放、捏合缩放、滑动导航在 109 上返回 `UnsupportedCapabilityError` 而非崩溃），启动主路径只用 `ICoreWebView2` / `_2` / `_3` / `Environment` / `Controller` / `Settings` 这些 109 全覆盖的接口；Win10 专属的 DWM 深色标题栏调用都在 `win32.IsWindowsVersionAtLeast(10, 0, 17763)` 之后。`build/windows/wails.exe.manifest` 已含 Win7 的 `dpiAware` 回退声明。

### 1. 准备 Win7 补丁工具链

两个活跃维护的选择，都是官方 Go 源码的 fork，revert 掉破坏 Win7 的若干上游提交（`ProcessPrng` → `RtlGenRandom`、恢复 `LoadLibraryA` 兜底、恢复 `sysSocket` 兜底、Win7 控制台句柄补丁、`Process.Wait` 的 5ms sleep）：

- [thongtech/go-legacy-win7](https://github.com/thongtech/go-legacy-win7) — 跟到 Go 1.26.x，README 声明测试范围 Win7 RTM 到 Win11 25H2
- [XTLS/go-win7](https://github.com/XTLS/go-win7) — Xray-core 项目自用并验证

选与 `go.mod` 里 `go 1.25.0` 对齐的版本（1.25.x）。

```powershell
# 1. 下载 windows_amd64 压缩包（非 -race 版），核对 release 页给出的 SHA-256
# 2. 解压到固定路径，例如 C:\go-legacy-win7
# 3. 设置环境变量（一次性）
setx GO_LEGACY_WIN7_ROOT C:\go-legacy-win7
```

**不要**覆盖系统里的官方 Go。`build.ps1` 只在当前进程内切换 `GOROOT` / `PATH`，官方工具链保持可用。脚本同时设 `GOTOOLCHAIN=local`，防止 `go` 按 `go.mod` 的版本指令自动下载官方工具链把补丁版顶替掉——这一步漏了，产物就又跑不了 Win7。

供应链提示：这是第三方工具链。若有安全审计要求，改为从 fork 源码自行构建（`src/make.bat`），或退回"官方 Go 1.20 + Wails 2.7.1"的全降级方案（代价是主干被锁在 Go 1.20，且需降级 `modernc.org/sqlite`≤v1.33.1、`golang.org/x/sys`≈v0.20.0）。

### 2. 准备 WebView2 109 固定版本

1. 获取 **WebView2 Runtime 109.0.1518.140 Fixed Version (x64)** 的 cab 包。官方下载页只保留最近两个固定版本，109 需从归档渠道获取
2. 解压到项目根目录的 `runtime_win7/`，确认 `runtime_win7\msedgewebview2.exe` 存在
3. `runtime_win7/` 已在 `.gitignore` 中，不入库

`runtime/` 保持现状，供 Win10+ 使用。

### 3. 构建 + 打包

```powershell
# 一步到底
.\scripts\build.ps1 -Target win7 -Installer

# 或手动
$env:GOROOT = "C:\go-legacy-win7"
$env:PATH = "$env:GOROOT\bin;$env:PATH"
$env:GOTOOLCHAIN = "local"
go version                      # 确认输出含 win7 / legacy 标记
wails build -platform windows/amd64
ISCC.exe /DRuntimeDir=runtime_win7 setup_inno\go.iss
```

---

## 四、运行时选择逻辑（为什么两个包能共用一套代码）

`main.go` 的 `checkAndGetWebviewPath()` 按**真实系统版本**分流（用 `RtlGetVersion`，不受应用清单兼容性垫片影响，`GetVersionEx` 会把 Win10 谎报成 6.2）：

- **Win7/8/8.1**：优先 `runtime_win7/`，其次系统 WebView2。**永不**退回 `runtime/`——那里的新版本在这些系统上必然启动失败，日志会说明为何跳过
- **Win10+**：优先 `runtime/`，其次系统 WebView2

这样避免了两个反向事故：Win7 用户拿到新版运行时直接白屏，以及 Win10 用户被 109 降级内核。

对应的 `go.iss` 侧：

- `#define RuntimeDir` 默认 `runtime`，可用 `/DRuntimeDir=runtime_win7` 覆盖
- `Source: "{#RuntimeDir}\*"; DestDir: "{app}\{#RuntimeDir}"` —— 目录名原样保留到安装目录，供上面的逻辑识别
- `MinVersion=6.1sp1` 声明最低系统要求
- `[UninstallDelete]` 两个目录名都清理，避免换包后残留

---

## 五、装完之后的验证

### 自动自检

`cmd/win7check` 编译出的 `build/bin/win7-selfcheck.exe` 用**与主程序相同的工具链**编译，随安装包一起分发（`go.iss` 里带 `skipifsourcedoesntexist`，缺失不阻断打包）。在目标机器的安装目录下运行：

```
win7-selfcheck.exe                     # 检测自身所在目录
win7-selfcheck.exe -dir "C:\Program Files\CloudDrive Companion"
win7-selfcheck.exe -json               # 机器可读输出，便于回收
```

退出码 0 表示无 FAIL 项；非 JSON 模式会在检测目录留一份 `win7-selfcheck-<时间>.log`。

自动覆盖的检查项：

| 检查 | 说明 |
|---|---|
| 系统版本 / Service Pack | `RtlGetVersion`；Win7 未装 SP1 直接 FAIL |
| 系统位数与架构 | 仅提供 64 位安装包 |
| Go 运行时版本 | **这项本身就是工具链验证**——官方 Go 1.21+ 的产物在 Win7 上跑不到这里 |
| KB4474419 | Win7 安装 WebView2 所需的 SHA-2 代码签名支持补丁 |
| WebView2 固定版本目录 | 按系统挑对目录；旧系统上只有 `runtime/` 时 FAIL（装错包了） |
| PE 最低系统版本 | 读 `msedgewebview2.exe` / `msedge.dll` 的 `MajorSubsystemVersion`。Chromium 110+ 声明 10.0，109 声明 6.1，Windows 加载器据此拒绝加载——这是"运行时能否在本机加载"最直接的判据 |
| 系统 WebView2 版本 | 回退路径是否可用 |
| SQLite 建库读写 | 用主程序同一个 `modernc.org/sqlite` 驱动实际建库、写入、读取（WAL），这是 Win7 方案最主要的未知项 |
| URI 协议注册 | HKLM/HKCU 都查，并校验注册的 exe 是否真实存在 |
| 本地服务端口 | 尝试监听；被占用时区分"主程序已在运行"和"被别的进程占了" |
| 系统命令行工具 | `tasklist` / `taskkill` / `netstat` / `netsh`，单实例控制与端口统计依赖 |

**不要**用 `msedgewebview2.exe --version` 判断运行时可用性——它没有这个命令行开关，无论版本匹配与否都以非零码退出（实测 exit status 13），无法区分。

### 仍需人工确认

- [ ] 系统托盘图标显示、右键菜单可点（`getlantern/systray`，自检工具标为 MANUAL）
- [ ] 主窗口正常渲染无白屏（日志确认命中的运行时目录）
- [ ] 网页唤起客户端 → 文件选择 → 扫描进度动画
- [ ] 链路追踪 UI 的 ECharts 甘特图能渲染（`chrome109` 目标的实际验证）
- [ ] 上传全流程含断点续传
- [ ] "保存并重启"流程（涉及 `taskkill /F /T` 与端口释放等待）

### 已知遗留风险

Win7 上的 WebView2 停留在 109，不再收到安全更新。这是该系统上唯一可用的版本，无法规避。页面内容来自自有服务时风险可接受；若不可接受，只能改为"本地服务 + 用户系统浏览器"方案。

---

## 六、注意事项

- 打包前确认对应的运行时目录存在且完整（约 150MB），安装包体积因此较大
- `go.iss` 使用相对路径（`SourceDir=..`），在项目根目录执行 ISCC 即可，无需改配置
- 覆盖安装会先 `taskkill /F /T` 关掉正在运行的主程序及其 WebView2 子进程树

---

## 关于浏览器 PNA / LNA 放行策略（重要）

为解决"公网 HTTP 页面访问本地伴侣 localhost"被浏览器拦截的问题
（报错：`The request client is not a secure context and the resource is in more-private address space loopback`），
安装包在安装时会向注册表写入 Chrome / Edge 的本地网络访问放行策略。

涉及的变更：

1. **安装权限提升为管理员**（`PrivilegesRequired=admin`）。
   原因：浏览器策略只能写入 `HKLM\Software\Policies\...`，
   而 `HKCU\Software\Policies` 受系统保护、普通用户无法写入。
2. **URI 协议注册从 HKCU 迁到 HKLM**（对所有用户生效）。
   原因：提权后 HKCU 指向的是管理员账户 hive，可能与登录用户不一致，
   迁到 HKLM 可避免协议注册写错位置导致"浏览器唤起客户端"失效。
3. 写入的策略键（新旧各一套，覆盖不同浏览器版本）：
   - `InsecurePrivateNetworkRequestsAllowedForUrls`（老版 PNA）
   - `LocalNetworkAccessAllowedForUrls`（新版 LNA，Chrome 142+）

### 配置放行的页面源

在 `go.iss` 顶部修改 `AllowOrigins`，**多个来源用英文分号 `;` 分隔**：

```
#define AllowOrigins "http://116.78.219.26:8002;http://10.0.0.5:8080;https://web.example.com"
```

- 单个来源也可以，直接写一个即可。
- 现场地址多变时也可填 `"*"` 放行所有来源（安全性较低，本地伴侣场景可接受）。
- 多来源由 `[Code]` 段在安装时按分号拆分，逐条写入注册表（每个来源一个编号值
  `1`/`2`/`3`…，这是 Chrome/Edge 列表型策略在注册表中的标准格式）。
- 卸载时 `[Code]` 段会自动清除这些策略键。
- 覆盖安装时会先清空旧的策略子键再重写，避免残留过期来源。

### 局限性（务必先用 testing 工具实测）

- 策略对"非受管理的个人版 Chrome/Edge"通常也生效（本机策略），
  但若现场是定制安全浏览器，可能不读取这些键。
- 新版 LNA（Chrome 142+）在非 HTTPS 页面下可能仍强制拦截，此时该策略也无法放行
  （即 testing 报告中的"情况3"），需另走 HTTPS / Wails 窗口等方案。
- 部署前请先用 `testing/` 目录下的检测工具确认现场浏览器属于哪种情况。

### Windows 7 上的额外说明

Win7 能装的最高版本是 Chrome 109 / Edge 109，**早于** LNA（Chrome 142+）引入的时间点，所以只有老版 PNA 策略 `InsecurePrivateNetworkRequestsAllowedForUrls` 生效，新版 LNA 键写了不会被读取（无害）。也就是说 Win7 上不会遇到 testing 报告里的"情况3"。

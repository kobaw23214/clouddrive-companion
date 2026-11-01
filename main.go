package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"clouddrive-companion-withWails2/internal/api"
	"clouddrive-companion-withWails2/internal/app"
	"clouddrive-companion-withWails2/internal/config"
	"clouddrive-companion-withWails2/internal/server"
	"clouddrive-companion-withWails2/internal/tracer"
	"clouddrive-companion-withWails2/internal/tray"
	"clouddrive-companion-withWails2/internal/uploader"
	"clouddrive-companion-withWails2/internal/utils"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"golang.org/x/sys/windows/registry"
)

/**
go build -ldflags="-H windowsgui" -v -o clouddrive-companion.exe .\main.go

-s: 去除符号表(symbol table)
-w: 去除DWARF调试信息
go build -ldflags="-s -w -H windowsgui" -v -o clouddrive-companion.exe .\main.go

go build -ldflags="-s -w -H windowsgui" -trimpath -v -o clouddrive-companion.exe .\main.go
upx --best --lzma clouddrive-companion.exe

.\clouddrive-companion.exe --register

-- 安装 wails(v2)
go install github.com/wailsapp/wails/v2/cmd/wails@latest

初始化项目
wails init -n clouddrive-companion -t vue

-- 开发模式
wails dev

-- 打包
wails build
*/

// restartFlag 是"保存并重启"时传给新实例的启动参数。
// 新实例看到它就知道：老实例正在退出，跳过"通知已有实例"的探测，先等端口释放。
// 该常量同时被 internal/app 的 SaveConfigAndRestart 使用（值需保持一致）。
const restartFlag = "--restarted"

//go:embed all:frontend/dist
var assets embed.FS

//go:embed icon.ico
var icon []byte

func main() {
	// 检查启动方式：非 URI 启动（即用户直接双击 exe）
	directLaunchMode := len(os.Args) <= 1 || !strings.HasPrefix(os.Args[1], "my-clouddrive://")

	// 是否由"保存并重启"拉起的新实例。
	// 这种情况下老实例正在退出，必须跳过"通知已有实例"的探测——否则可能探测到
	// 尚未完全关闭的老实例并让本实例直接退出，导致重启后程序整体消失。
	restartedMode := false
	for _, arg := range os.Args[1:] {
		if arg == restartFlag {
			restartedMode = true
			break
		}
	}

	// ========== 第一步：初始化配置和日志 ==========
	config.SetupAppSupportDir()
	config.SetupLogging()
	config.LoadConfig()
	uploader.SetupTaskManifestDir()

	// ========== 第一步半：close-client 指令（只关客户端，不启动界面） ==========
	// 浏览器唤起 my-clouddrive://close-client 时，操作系统同样是"新起一个进程"，
	// 但这个进程的唯一职责是让已运行的实例退出，然后自己也退出：
	// 不初始化 WebView2、不启动本地服务、不显示任何窗口。
	// 必须放在单实例检测之前，否则会被下面的 killExistingInstances / 通知逻辑抢先处理。
	if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], config.AppConfig.URIScheme+"://") &&
		parseURIAction(os.Args[1]) == "close-client" {
		log.Printf("Application started via URI Scheme: %s", os.Args[1])
		log.Println("启动模式: close-client (关闭已运行的客户端后立即退出)")
		shutdownExistingInstances()
		log.Println("close-client 处理完成，本实例退出。")
		if config.LogFile != nil {
			config.LogFile.Close()
		}
		os.Exit(0)
	}

	// ========== 第二步：单实例检测与控制 ==========
	if restartedMode {
		// 等老实例把端口、WebView2 资源释放干净，再继续启动，避免端口占用/渲染器冲突。
		log.Println("检测到重启启动参数：等待旧实例退出...")
		waitForPortFree(config.AppConfig.LocalServerPort, 5*time.Second)
	} else if !directLaunchMode {
		// 正常启动模式：必须关闭旧实例，避免 WebView2 冲突。
		// 优先走 /shutdown 让旧实例优雅退出（正常清理托盘图标、flush 数据）；
		// 连不上或卡住才回退 taskkill 强杀——强杀的旧实例来不及移除托盘图标，
		// 会在通知区域留下"右键没反应"的幽灵图标。
		shutdownExistingInstances()
	} else {
		log.Println("双击启动模式：检测是否已有实例...")
		// 已有实例在跑时，不要再起一个（会和 WebView2 打架），
		// 而是通过本地 HTTP 服务让它把上传面板显示出来。
		port := config.AppConfig.LocalServerPort
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/openClient", port))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				log.Println("✅ 成功通知已有实例显示上传面板，本实例直接退出。")
				os.Exit(0)
			}
		}
		log.Println("无响应或未找到已有实例，将作为新实例启动上传面板。")
	}

	// 初始化 HTTP 请求日志的磁盘存储（与 tasks/、runtime/ 同级）
	httpLogStoreReady := true
	if err := server.InitHTTPLogStore(filepath.Join(config.AppSupportDir, "http_log")); err != nil {
		log.Printf("Warning: 初始化 HTTP 日志存储失败: %v", err)
		httpLogStoreReady = false
	}

	// 初始化任务级链路追踪存储（与 http_log/、tasks/ 同级）
	// 失败仅记录日志，不阻塞启动 —— 追踪是辅助能力，不应让上传不可用。
	if err := tracer.Init(filepath.Join(config.AppSupportDir, "task_trace")); err != nil {
		log.Printf("Warning: 初始化任务追踪失败: %v", err)
	}

	// 初始化 API 客户端
	api.InitClient()

	// 接线出站请求日志：api 包通过回调把日志交给 server 落库（避免 api → server 的 import 环）
	if httpLogStoreReady {
		api.SetOutboundLogger(server.LogOutboundHTTP)
		// 接线 trace SSE 连接生命周期日志：tracer 包通过回调把 SSE 建立/断开
		// 打点交给 server 落库（/task-traces/* 被 middleware 排除，需显式打点）
		tracer.SetSSEConnLogger(server.LogSSEConnEvent)
	}

	// 创建 App 实例
	application := app.NewApp()

	// 检查启动方式（用配置中的 URIScheme 重新校准）
	wasLaunchedByURI := false
	openClientMode := false
	if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], config.AppConfig.URIScheme+"://") {
		log.Printf("Application started via URI Scheme: %s", os.Args[1])
		wasLaunchedByURI = true
		if strings.Contains(os.Args[1], "open-client") {
			openClientMode = true
			log.Println("启动模式: open-client (直接显示主面板)")
		}

		// ── 解析 URI 中的 server 参数，应用后端地址内存覆盖 ──
		// 前端可在拉起客户端时携带服务器地址，优先级高于 config.json，例如：
		//   my-clouddrive://select-files?server=http://192.168.1.100:8081
		//   my-clouddrive://open-client?server=192.168.1.100:8081
		// 没传 server 时不覆盖，沿用 config.json（兼容旧版本调用方式）。
		applyServerOverrideFromURI(os.Args[1])
	} else {
		log.Println("Application started directly. Opening upload dashboard.")
	}

	// 检查并获取 WebView2 路径（带详细日志）
	log.Println("========== WebView2 运行时检测开始 ==========")
	webviewPath := checkAndGetWebviewPath()
	usingLocalWebview := webviewPath != ""
	if webviewPath != "" {
		log.Printf("✅ 将使用程序目录下的 WebView2 运行时: %s", webviewPath)
	} else {
		log.Println("✅ 将使用系统已安装的 WebView2 运行时")
	}
	log.Println("========== WebView2 运行时检测完成 ==========")

	// 尝试启动应用，如果使用本地 WebView2 失败，则回退到系统版本
	err := runWailsApp(application, wasLaunchedByURI, openClientMode, webviewPath, usingLocalWebview)

	// 兜底 flush：wails.Run 正常退出时调用一次 Shutdown，
	// 通过 a.Quit() 路径退出时也会调用，二次调用是幂等的。
	tracer.Shutdown()

	defer config.LogFile.Close()

	if err != nil {
		log.Fatal(err)
	}
}

// runWailsApp 启动 Wails 应用，如果本地 WebView2 失败则回退到系统版本
func runWailsApp(application *app.App, wasLaunchedByURI bool, openClientMode bool, webviewPath string, usingLocalWebview bool) error {
	// 是否为双击直启模式（无 URI 参数）。此时默认展示上传面板（Dashboard），
	// 配置面板改由面板内的齿轮图标进入，因此窗口尺寸统一用面板的大尺寸。
	isDirectLaunchMode := !wasLaunchedByURI

	// 根据启动模式设定窗口参数
	windowTitle := "上传客户端伴侣"
	windowWidth := 1024
	windowHeight := 768
	// URI 启动时窗口先隐藏（由具体流程再显示）；双击启动需要立即可见。
	startHidden := !wasLaunchedByURI
	if isDirectLaunchMode {
		startHidden = false
	}

	// 创建应用配置函数，便于重用
	createAppOptions := func(webviewBrowserPath string) *options.App {
		return &options.App{
			Title:       windowTitle,
			Width:       windowWidth,
			Height:      windowHeight,
			StartHidden: startHidden,
			AssetServer: &assetserver.Options{
				Assets: assets,
			},
			Bind: []interface{}{
				application,
			},
			OnStartup: func(ctx context.Context) {
				application.OnStartup(ctx, wasLaunchedByURI, openClientMode)

				if runtime.GOOS == "windows" {
					log.Println("检测到 Windows，正在启动系统托盘...")
					// Start 内部有 once 保护：本地 WebView2 失败回退系统版本时
					// OnStartup 会二次执行，不会因此起出第二个托盘图标。
					tray.Start(application, icon)
				} else {
					log.Printf("检测到 %s (非 Windows)，将跳过系统托盘加载。", runtime.GOOS)
				}
			},
			OnBeforeClose: func(ctx context.Context) (prevent bool) {
				return application.OnBeforeClose()
			},
			Windows: &windows.Options{
				DisableWindowIcon:                 false,
				DisableFramelessWindowDecorations: false,
				WebviewBrowserPath:                webviewBrowserPath,
			},
		}
	}

	// 第一次尝试：使用指定的 WebView2 路径
	log.Println("========== 尝试启动应用 ==========")
	err := wails.Run(createAppOptions(webviewPath))

	// 如果使用本地 WebView2 失败，尝试回退到系统版本
	if err != nil && usingLocalWebview {
		log.Printf("❌ 使用本地 WebView2 启动失败: %v", err)
		log.Println("========== 回退到系统 WebView2 重试 ==========")
		log.Println("正在清理本地 WebView2 进程...")

		// 等待一段时间，确保进程完全退出
		time.Sleep(2 * time.Second)

		// 再次关闭可能存在的实例（包括 WebView2 相关进程）
		killExistingInstances()

		// 额外清理 WebView2 相关进程
		log.Println("正在清理 WebView2 相关进程...")
		cleanupWebView2Processes()

		// 再等待一段时间
		time.Sleep(1 * time.Second)

		// 使用系统 WebView2 重试
		log.Println("✅ 使用系统 WebView2 重新启动应用...")
		err = wails.Run(createAppOptions(""))

		if err != nil {
			log.Printf("❌ 使用系统 WebView2 启动也失败: %v", err)
			return err
		} else {
			log.Println("✅ 使用系统 WebView2 启动成功")
		}
	} else if err != nil {
		// 如果本来就用系统版本还失败，直接返回错误
		return err
	}

	return nil
}

// WebView2 固定版本运行时目录名。
//
// 为什么要分两个目录：
//
//	微软在 2022-12 宣布 WebView2 Runtime 109 是支持 Win7/8/8.1 的最后版本，
//	之后的运行时（例如当前 runtime/ 里的 144.x）在这些系统上根本无法启动。
//	所以按系统版本选择不同的固定版本目录：
//	  - Win10+     → runtime/       （可用最新固定版本）
//	  - Win7/8/8.1 → runtime_win7/  （必须是 109.0.1518.x）
const (
	runtimeDirModern = "runtime"
	runtimeDirLegacy = "runtime_win7"
)

// checkAndGetWebviewPath 检测系统 WebView2 状态，决定 Wails 使用哪个运行时。
//
// 返回空字符串表示使用系统安装的 WebView2，返回路径表示使用程序目录下的固定版本。
//
// 选择策略按系统版本区分：
//   - Win7/8/8.1：优先 runtime_win7/（109 固定版本），其次系统版本。
//     绝不使用 runtime/，因为那里的新版本在这些系统上必然启动失败。
//   - Win10+：优先 runtime/（沿用既有行为），其次系统版本。
func checkAndGetWebviewPath() string {
	log.Println("[步骤 1/5] 开始检测 WebView2 运行时状态...")

	// 1. 获取程序运行目录
	exePath, err := os.Executable()
	if err != nil {
		log.Printf("❌ [步骤 1] 获取程序路径失败: %v", err)
		return ""
	}
	exeDir := filepath.Dir(exePath)
	log.Printf("📁 [步骤 1] 程序运行目录: %s", exeDir)

	// 识别真实系统版本，决定该找哪个运行时目录
	legacyOS := utils.IsLegacyWindows()
	log.Printf("🖥️  [步骤 1] 系统版本: %s (%s)", utils.WindowsFriendlyName(), utils.WindowsVersionString())

	preferredDir := runtimeDirModern
	if legacyOS {
		preferredDir = runtimeDirLegacy
		log.Printf("   → 旧版 Windows：WebView2 最高只支持 109，将查找 %s/", runtimeDirLegacy)
	}

	// 2. 优先检查程序目录下的固定版本运行时
	log.Printf("[步骤 2/5] 优先检查程序目录下的 %s 文件夹...", preferredDir)
	if localRuntime := findLocalWebview2Runtime(exeDir, preferredDir); localRuntime != "" {
		log.Printf("✅ [步骤 2] 使用程序目录下的固定版本运行时: %s", localRuntime)
		return localRuntime
	}
	log.Println("   → 将回退到系统 WebView2")

	if legacyOS {
		// 旧系统上即使 runtime/ 存在也不能用，这里明确说明为什么跳过。
		if _, statErr := os.Stat(filepath.Join(exeDir, runtimeDirModern)); statErr == nil {
			log.Printf("⚠️  [步骤 2] 检测到 %s/ 存在，但当前系统无法运行该版本，已跳过", runtimeDirModern)
			log.Printf("   → 旧版 Windows 需要 109 版本的固定运行时（放在 %s/）", runtimeDirLegacy)
		}
	}

	// 3. 如果本地没有，检查系统注册表（作为回退方案）
	log.Println("[步骤 3/5] 本地固定版本运行时不可用，检查系统 WebView2 注册表...")
	registryPath := `SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`
	k, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		registryPath,
		registry.QUERY_VALUE,
	)
	if err != nil {
		log.Printf("❌ [步骤 3] 未检测到 WebView2 注册表项: %s", registryPath)
		log.Printf("   错误详情: %v", err)
		log.Println("   → 系统未安装 WebView2 或版本过旧")
	} else {
		defer k.Close()
		log.Printf("✅ [步骤 3] 找到 WebView2 注册表项")

		// 4. 读取版本号
		log.Println("[步骤 4/5] 读取系统 WebView2 版本号...")
		version, _, err := k.GetStringValue("pv")
		if err != nil || version == "" {
			log.Printf("⚠️  [步骤 4] 读取版本号失败: %v", err)
			log.Println("   → 注册表存在但版本信息无效")
		} else {
			log.Printf("✅ [步骤 4] 检测到系统 WebView2 版本: %s", version)
			log.Println("   → 本地固定版本不可用，将使用系统 WebView2 作为回退方案")
			if legacyOS {
				log.Println("   → 提示：旧版 Windows 上系统 WebView2 应为 109.0.1518.x")
			}
			return "" // 使用系统版本
		}
	}

	// 5. 本地和系统都没有，返回空（让 Wails 处理错误）
	log.Println("[步骤 5/5] 最终决定...")
	log.Printf("❌ [步骤 5] 程序目录下未找到可用的 %s 文件夹，且系统也未安装 WebView2", preferredDir)
	log.Println("   → 程序可能无法启动")
	if legacyOS {
		log.Println("   → 旧版 Windows 请安装 WebView2 Runtime 109.0.1518.x（最后一个支持该系统的版本）")
		log.Printf("   → 或确认安装包已附带 %s/ 目录", runtimeDirLegacy)
	} else {
		log.Println("   → 建议：重新运行安装程序，确保 runtime 文件夹被正确安装")
	}
	return ""
}

// findLocalWebview2Runtime 在 exeDir/dirName 下查找固定版本运行时。
// 命中则返回该目录的绝对路径，否则返回空字符串。
//
// 兼容两种解压结构：
//
//	结构1: <dir>/msedgewebview2.exe             (Fixed Version 直接解压)
//	结构2: <dir>/EBWebView/msedgewebview2.exe   (某些版本的子目录结构)
func findLocalWebview2Runtime(exeDir, dirName string) string {
	runtimeDir := filepath.Join(exeDir, dirName)

	if _, err := os.Stat(runtimeDir); err != nil {
		log.Printf("ℹ️  程序目录下未找到 %s 文件夹: %s", dirName, runtimeDir)
		return ""
	}
	log.Printf("✅ 发现程序目录下的 %s 文件夹: %s", dirName, runtimeDir)

	candidates := []string{
		filepath.Join(runtimeDir, "msedgewebview2.exe"),
		filepath.Join(runtimeDir, "EBWebView", "msedgewebview2.exe"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			log.Printf("✅ 验证通过: 找到 msedgewebview2.exe 于 %s", candidate)
			return runtimeDir
		}
	}

	log.Printf("⚠️  警告: %s 文件夹存在但缺少关键文件 msedgewebview2.exe", dirName)
	for _, candidate := range candidates {
		log.Printf("   检查的路径: %s", candidate)
	}
	return ""
}

// waitForPortFree 等待本地服务端口被释放，用于"保存并重启"场景。
//
// 老实例退出需要一点时间（flush 追踪队列、关闭 SQLite、释放 WebView2），
// 这段时间内它的 HTTP 端口仍被占用。新实例先在这里轮询等待，端口空出来
// 再继续启动，避免 ListenAndServe 直接报端口占用。
// 超过 timeout 仍未释放则记录警告并继续（老实例可能异常残留，交由后续流程处理）。
func waitForPortFree(port int, timeout time.Duration) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if err != nil {
			// 连不上 = 端口已释放
			log.Printf("旧实例已退出（端口 %d 已释放），继续启动。", port)
			return
		}
		conn.Close()
		time.Sleep(200 * time.Millisecond)
	}

	log.Printf("警告: 等待端口 %d 释放超时，仍继续启动。", port)
}

// parseURIAction 解析 URI 中的动作名（scheme 后面那一段），统一转小写。
//
//	my-clouddrive://select-files              → "select-files"
//	my-clouddrive://open-client?server=...    → "open-client"
//	my-clouddrive://close-client              → "close-client"
//
// 大多数情况下动作落在 URL 的 Host 部分；少数调用方会写成三斜杠形式
// （my-clouddrive:///close-client），此时动作在 Path 上，这里一并兜住。
// 解析失败返回空字符串，调用方按"无动作"处理。
func parseURIAction(rawURI string) string {
	u, err := url.Parse(rawURI)
	if err != nil {
		log.Printf("[uri] 解析 URI 失败: %v", err)
		return ""
	}
	if u.Host != "" {
		return strings.ToLower(u.Host)
	}
	return strings.ToLower(strings.Trim(u.Path, "/"))
}

// shutdownExistingInstances 让正在运行的客户端实例退出，供 close-client 使用。
//
// 优先走"打招呼"的方式：调用已运行实例的本地 /shutdown 接口，由它自己走退出流程，
// 然后轮询等端口释放，确认真的退干净了。
// 若接口连不上（实例本来就没在跑）或返回异常（进程卡死），回退到 taskkill 强杀进程树，
// 保证 close-client 的语义是"调用完之后客户端一定不在运行"。
func shutdownExistingInstances() {
	port := config.AppConfig.LocalServerPort
	client := &http.Client{Timeout: 2 * time.Second}

	resp, err := client.Post(fmt.Sprintf("http://127.0.0.1:%d/shutdown", port), "application/json", nil)
	if err != nil {
		log.Printf("未连接到运行中的实例 (%v)，改用进程级清理兜底。", err)
	} else {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			log.Printf("✅ 已通知运行中的实例退出，等待端口 %d 释放...", port)
			waitForPortFree(port, 5*time.Second)
			return
		}
		log.Printf("/shutdown 返回状态码 %d，改用进程级清理兜底。", resp.StatusCode)
	}

	// 兜底：直接结束同名进程及其 WebView2 子进程树。
	killExistingInstances()
}

// killExistingInstances 关闭已运行的程序实例
// 必须在程序启动最开始执行，避免多个实例导致的 WebView2 冲突
func killExistingInstances() {
	// 获取当前进程 ID，避免杀死自己
	currentPID := os.Getpid()

	// 使用 WMIC 查找同名进程（排除自己）
	// 命令: wmic process where "name='clouddrive-companion.exe'" get processid
	exeName := "clouddrive-companion.exe"

	// 方法1: 使用 tasklist 检查是否有其他实例在运行
	checkCmd := exec.Command("tasklist", "/FI", fmt.Sprintf("IMAGENAME eq %s", exeName), "/FO", "CSV", "/NH")
	checkCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := checkCmd.Output()

	if err != nil {
		// tasklist 执行失败，跳过检查
		return
	}

	// 解析输出，查找其他实例的 PID
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, exeName) {
			continue
		}

		// CSV 格式: "clouddrive-companion.exe","1234","Console","1","12,345 K"
		parts := strings.Split(line, ",")
		if len(parts) >= 2 {
			pidStr := strings.Trim(parts[1], "\"")
			var pid int
			fmt.Sscanf(pidStr, "%d", &pid)

			// 跳过当前进程
			if pid == currentPID || pid == 0 {
				continue
			}

			// 发现其他实例，尝试关闭（包括其 WebView2 子进程）
			fmt.Printf("发现已运行的实例 (PID: %d)，正在关闭及其子进程...\n", pid)

			// 使用 taskkill /F /T 关闭指定 PID 的进程及其子进程树
			// /F = 强制终止
			// /T = 终止进程树（包括 WebView2 子进程）
			killCmd := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid))
			killCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			killCmd.Run()
		}
	}

	// 短暂等待，确保进程完全退出
	time.Sleep(500 * time.Millisecond)
}

// cleanupWebView2Processes 清理 WebView2 相关进程
func cleanupWebView2Processes() {
	// 清理 msedgewebview2.exe 进程
	processes := []string{"msedgewebview2.exe", "msedge.exe"}
	for _, procName := range processes {
		killCmd := exec.Command("taskkill", "/F", "/IM", procName, "/T")
		killCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		killCmd.Run() // 忽略错误，因为进程可能不存在
	}
}

// applyServerOverrideFromURI 从 my-clouddrive:// 的 URI 中解析 server 查询参数，
// 并应用为后端地址的内存覆盖（仅本次进程有效，不写盘）。
//
// 支持的 URI 形式（server 参数可选）：
//
//	my-clouddrive://select-files
//	my-clouddrive://select-files?server=http://192.168.1.100:8081
//	my-clouddrive://open-client?server=https://backend.example.com:8443
//	my-clouddrive://open-client?server=192.168.1.100:8081   （缺省补 http://）
//
// 解析失败或未携带 server 时不覆盖，继续沿用 config.json，保证与旧版本完全兼容。
func applyServerOverrideFromURI(rawURI string) {
	u, err := url.Parse(rawURI)
	if err != nil {
		log.Printf("[override] 解析 URI 失败，忽略 server 覆盖: %v", err)
		return
	}

	server := u.Query().Get("server")
	if server == "" {
		log.Println("[override] URI 未携带 server 参数，使用 config.json 中的后端地址。")
		return
	}

	if err := config.SetBackendOverride(server); err != nil {
		log.Printf("[override] server 参数无效，回退使用 config.json: %v", err)
		return
	}
	log.Printf("[override] 已采用 URI 传入的后端地址覆盖: %s", config.GetBackendOverrideBase())
}

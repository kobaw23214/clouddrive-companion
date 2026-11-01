// win7check 是 CloudDrive Companion 的运行环境自检工具。
//
// 用途：在目标机器（尤其是 Windows 7 SP1）上一次性验证程序运行所需的
// 全部前置条件，把 doc/windows7-support.md 里的人工验证清单自动化。
//
// 它刻意做成独立可执行文件，并且用与主程序相同的工具链编译 ——
// 这样"它自己能不能在这台机器上跑起来"本身就是对 Go 工具链的第一道验证：
// 用官方 Go 1.21+ 编译的产物在 Win7 上根本无法启动。
//
// 用法：
//
//	win7-selfcheck.exe                 # 在程序安装目录下运行
//	win7-selfcheck.exe -dir "C:\Program Files\CloudDrive Companion"
//	win7-selfcheck.exe -port 34567     # 覆盖待检测的本地服务端口
//	win7-selfcheck.exe -json           # 输出机器可读结果
//
// 退出码：0 = 无失败项；1 = 存在 FAIL 项。
package main

import (
	"database/sql"
	"debug/pe"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------- 结果模型

type status string

const (
	statusPass   status = "PASS"
	statusFail   status = "FAIL"
	statusWarn   status = "WARN"
	statusSkip   status = "SKIP"
	statusManual status = "MANUAL"
)

type result struct {
	Name    string `json:"name"`
	Status  status `json:"status"`
	Detail  string `json:"detail"`
	Advice  string `json:"advice,omitempty"`
	Elapsed string `json:"elapsed"`
}

type report struct {
	Timestamp string   `json:"timestamp"`
	AppDir    string   `json:"appDir"`
	Results   []result `json:"results"`
}

// ---------------------------------------------------------------- 运行参数

var (
	flagDir  = flag.String("dir", "", "程序安装目录（默认为本工具所在目录）")
	flagPort = flag.Int("port", 0, "待检测的本地服务端口（默认读取 config.json，回退 34567）")
	flagJSON = flag.Bool("json", false, "以 JSON 输出结果")
)

// 与 main.go 保持一致的运行时目录名
const (
	runtimeDirModern = "runtime"
	runtimeDirLegacy = "runtime_win7"
)

// legacyMaxWebview2Major 是 Win7/8/8.1 上 WebView2 的最高主版本号。
// 微软 2022-12 封版：109 是支持这些系统的最后一个版本。
const legacyMaxWebview2Major = 109

func main() {
	flag.Parse()

	appDir := *flagDir
	if appDir == "" {
		if exe, err := os.Executable(); err == nil {
			appDir = filepath.Dir(exe)
		} else {
			appDir, _ = os.Getwd()
		}
	}

	rep := report{
		Timestamp: time.Now().Format(time.RFC3339),
		AppDir:    appDir,
	}

	checks := []struct {
		name string
		fn   func(appDir string) (status, string, string)
	}{
		{"操作系统版本 / Service Pack", checkOSVersion},
		{"系统位数与架构", checkArch},
		{"Go 运行时版本（工具链验证）", checkGoRuntime},
		{"SHA-2 代码签名补丁 (KB4474419)", checkSHA2Update},
		{"WebView2 固定版本目录", checkLocalRuntimeDir},
		{"WebView2 运行时与系统兼容性（PE 最低系统版本）", checkRuntimeMinimumOS},
		{"系统安装的 WebView2 版本", checkSystemWebview2},
		{"SQLite 建库读写 (modernc.org/sqlite)", checkSQLite},
		{"URI 协议注册", checkURIScheme},
		{"本地服务端口可用性", checkLocalPort},
		{"依赖的系统命令行工具", checkExternalTools},
		{"系统托盘", checkTray},
	}

	for _, c := range checks {
		start := time.Now()
		st, detail, advice := c.fn(appDir)
		rep.Results = append(rep.Results, result{
			Name:    c.name,
			Status:  st,
			Detail:  detail,
			Advice:  advice,
			Elapsed: time.Since(start).Truncate(time.Millisecond).String(),
		})
	}

	failed := writeOutput(rep)
	if failed > 0 {
		os.Exit(1)
	}
}

// ---------------------------------------------------------------- 输出

func writeOutput(rep report) int {
	failed := 0
	for _, r := range rep.Results {
		if r.Status == statusFail {
			failed++
		}
	}

	if *flagJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
		return failed
	}

	fmt.Println("========================================================")
	fmt.Println(" CloudDrive Companion 运行环境自检")
	fmt.Println("========================================================")
	fmt.Printf(" 时间     : %s\n", rep.Timestamp)
	fmt.Printf(" 检测目录 : %s\n", rep.AppDir)
	fmt.Println("--------------------------------------------------------")

	counts := map[status]int{}
	for _, r := range rep.Results {
		counts[r.Status]++
		fmt.Printf("[%-6s] %s\n", r.Status, r.Name)
		for _, line := range strings.Split(r.Detail, "\n") {
			if line != "" {
				fmt.Printf("         %s\n", line)
			}
		}
		if r.Advice != "" {
			fmt.Printf("         → %s\n", r.Advice)
		}
	}

	fmt.Println("--------------------------------------------------------")
	fmt.Printf(" 通过 %d  失败 %d  警告 %d  跳过 %d  待人工确认 %d\n",
		counts[statusPass], counts[statusFail], counts[statusWarn],
		counts[statusSkip], counts[statusManual])
	if failed == 0 {
		fmt.Println(" 结论：无阻塞性问题。WARN / MANUAL 项请逐条确认。")
	} else {
		fmt.Println(" 结论：存在阻塞性问题，程序可能无法正常运行。")
	}
	fmt.Println("========================================================")

	// 同时落一份日志到检测目录，便于远程支持时回收
	logPath := filepath.Join(rep.AppDir, fmt.Sprintf("win7-selfcheck-%s.log", time.Now().Format("20060102-150405")))
	if data, err := json.MarshalIndent(rep, "", "  "); err == nil {
		if err := os.WriteFile(logPath, data, 0o644); err == nil {
			fmt.Printf(" 详细结果已写入: %s\n", logPath)
		}
	}

	return failed
}

// ---------------------------------------------------------------- 各项检查

// checkOSVersion 校验系统版本与 Service Pack。
//
// 用 RtlGetVersion 而不是 GetVersionEx：后者受应用清单兼容性垫片影响，
// 在未声明 supportedOS 的程序里可能把 Win10 谎报成 6.2。
func checkOSVersion(string) (status, string, string) {
	v := windows.RtlGetVersion()
	detail := fmt.Sprintf("内核版本 %d.%d.%d, Service Pack %d.%d",
		v.MajorVersion, v.MinorVersion, v.BuildNumber, v.ServicePackMajor, v.ServicePackMinor)

	switch {
	case v.MajorVersion >= 10:
		return statusPass, detail + "（Windows 10 / 11，支持最新 WebView2）", ""
	case v.MajorVersion == 6 && v.MinorVersion == 1:
		if v.ServicePackMajor < 1 {
			return statusFail, detail + "（Windows 7，未安装 SP1）",
				"WebView2 与 Go 运行时均要求 Windows 7 SP1，请先安装 Service Pack 1"
		}
		return statusPass, detail + "（Windows 7 SP1，WebView2 上限为 109）", ""
	case v.MajorVersion == 6 && (v.MinorVersion == 2 || v.MinorVersion == 3):
		return statusPass, detail + "（Windows 8 / 8.1，WebView2 上限为 109）", ""
	default:
		return statusFail, detail + "（低于 Windows 7）",
			"不受支持的系统版本，最低要求 Windows 7 SP1"
	}
}

func checkArch(string) (status, string, string) {
	detail := fmt.Sprintf("程序架构 %s，系统架构 %s",
		runtime.GOARCH, os.Getenv("PROCESSOR_ARCHITECTURE"))
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return statusFail, detail, "安装包仅提供 64 位版本，请使用 64 位系统"
	}
	return statusPass, detail, ""
}

// checkGoRuntime 报告编译本工具的 Go 版本。
//
// 这项检查的价值在于：能看到这行输出，就说明该工具链的产物可以在本机启动。
// 官方 Go 1.21+ 的产物在 Win7 上会在运行时初始化阶段直接失败，跑不到这里。
func checkGoRuntime(string) (status, string, string) {
	v := windows.RtlGetVersion()
	detail := fmt.Sprintf("本工具由 %s 编译，已在本机成功启动", runtime.Version())

	if v.MajorVersion >= 10 {
		return statusPass, detail, ""
	}
	return statusPass,
		detail + "\n旧版 Windows 上能启动即证明该工具链的产物可用（官方 Go 1.21+ 会在此之前就失败）",
		"请确认主程序 exe 与本工具由同一套工具链编译"
}

// checkSHA2Update 检查 Win7 安装 WebView2 / Edge 所需的 SHA-2 代码签名支持补丁。
// 缺少 KB4474419 时，运行时安装包的签名校验会失败。
func checkSHA2Update(string) (status, string, string) {
	v := windows.RtlGetVersion()
	if !(v.MajorVersion == 6 && v.MinorVersion == 1) {
		return statusSkip, "仅 Windows 7 需要此补丁", ""
	}

	out, err := runCommand("wmic", "qfe", "get", "HotFixID")
	if err != nil {
		return statusWarn, fmt.Sprintf("无法枚举已安装补丁: %v", err),
			"请手动确认 KB4474419（SHA-2 代码签名支持）已安装"
	}
	if strings.Contains(out, "KB4474419") {
		return statusPass, "已安装 KB4474419（SHA-2 代码签名支持）", ""
	}
	return statusWarn, "未在已安装补丁列表中找到 KB4474419",
		"缺少该补丁时 WebView2 运行时安装包签名校验会失败；若运行时已随安装包附带则影响有限"
}

// checkLocalRuntimeDir 校验随程序附带的 WebView2 固定版本目录，
// 并核对其版本号与当前系统是否匹配。
func checkLocalRuntimeDir(appDir string) (status, string, string) {
	v := windows.RtlGetVersion()
	legacy := v.MajorVersion == 6 && v.MinorVersion >= 1

	expectedDir := runtimeDirModern
	if legacy {
		expectedDir = runtimeDirLegacy
	}

	dir, exe := findRuntime(appDir, expectedDir)
	if dir == "" {
		detail := fmt.Sprintf("未找到 %s\\ 或其中缺少 msedgewebview2.exe", expectedDir)
		advice := "程序将回退到系统安装的 WebView2"
		if legacy {
			// 旧系统上如果只有 modern 目录，明确指出这是配错了包
			if other, _ := findRuntime(appDir, runtimeDirModern); other != "" {
				return statusFail,
					detail + fmt.Sprintf("\n但发现了 %s\\（面向 Windows 10+），当前系统无法运行该版本", runtimeDirModern),
					"装错安装包了：旧版 Windows 需要附带 runtime_win7\\（WebView2 109.0.1518.x）的版本"
			}
			advice = "旧版 Windows 需要系统已装 WebView2 109.0.1518.x，否则程序无法启动"
		}
		return statusWarn, detail, advice
	}

	version := runtimeVersionFromManifest(dir)
	detail := fmt.Sprintf("目录: %s\n可执行文件: %s\n版本: %s", dir, exe, version)

	if legacy && version != "未知" {
		if major := majorOf(version); major > legacyMaxWebview2Major {
			return statusFail, detail,
				fmt.Sprintf("当前系统最高支持 WebView2 %d，该版本无法启动，请替换为 109.0.1518.x", legacyMaxWebview2Major)
		}
	}
	if !legacy && majorOf(version) == legacyMaxWebview2Major {
		return statusWarn, detail,
			"Windows 10+ 上打包了 109 版本，等于把内核降级到 2023 年的版本"
	}
	return statusPass, detail, ""
}

// checkRuntimeMinimumOS 读取运行时关键二进制的 PE 头，比对其声明的最低系统版本。
//
// 这是判断"这份运行时能否在本机加载"最直接的依据：Windows 加载器会拒绝
// MajorSubsystemVersion 高于当前系统的映像。Chromium 110 起把该字段提到 10.0，
// 109 仍是 6.1，所以在 Win7 上放了新版运行时时这里会明确失败，
// 不必等到主程序启动白屏才发现。
//
// 注意不能用 `msedgewebview2.exe --version` 探测：它没有这个命令行开关，
// 无论版本是否匹配都会以非零码退出，无法区分。
func checkRuntimeMinimumOS(appDir string) (status, string, string) {
	v := windows.RtlGetVersion()
	expectedDir := runtimeDirModern
	if v.MajorVersion == 6 && v.MinorVersion >= 1 {
		expectedDir = runtimeDirLegacy
	}

	dir, exe := findRuntime(appDir, expectedDir)
	if dir == "" {
		return statusSkip, "未找到本地固定版本运行时，跳过兼容性检查", ""
	}

	// 除主进程外也检查 msedge.dll —— 真正承载渲染的模块
	targets := []string{exe}
	for _, name := range []string{"msedge.dll", "msedge_elf.dll"} {
		candidate := filepath.Join(filepath.Dir(exe), name)
		if _, err := os.Stat(candidate); err == nil {
			targets = append(targets, candidate)
		}
	}

	var lines []string
	worstStatus := statusPass
	var advice string

	for _, target := range targets {
		major, minor, err := peSubsystemVersion(target)
		if err != nil {
			lines = append(lines, fmt.Sprintf("%s: 无法解析 PE 头 (%v)", filepath.Base(target), err))
			if worstStatus == statusPass {
				worstStatus = statusWarn
				advice = "无法确认二进制的最低系统版本，请人工核对运行时版本"
			}
			continue
		}

		required := fmt.Sprintf("%d.%d", major, minor)
		if uint32(major) > v.MajorVersion ||
			(uint32(major) == v.MajorVersion && uint32(minor) > v.MinorVersion) {
			lines = append(lines, fmt.Sprintf("%s: 要求系统 >= %s，本机 %d.%d —— 加载器会拒绝",
				filepath.Base(target), required, v.MajorVersion, v.MinorVersion))
			worstStatus = statusFail
			advice = "该运行时版本无法在本机加载，请替换为 WebView2 109.0.1518.x（Win7/8/8.1 的最后版本）"
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: 要求系统 >= %s，本机 %d.%d —— 兼容",
			filepath.Base(target), required, v.MajorVersion, v.MinorVersion))
	}

	return worstStatus, strings.Join(lines, "\n"), advice
}

// checkSystemWebview2 读取系统安装的 WebView2 版本（回退方案是否可用）。
func checkSystemWebview2(string) (status, string, string) {
	const clientKey = `SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`

	k, err := registry.OpenKey(registry.LOCAL_MACHINE, clientKey, registry.QUERY_VALUE)
	if err != nil {
		return statusWarn, "注册表中未找到 WebView2 运行时",
			"系统未安装 WebView2；若本地固定版本可用则不影响启动，否则程序无法运行"
	}
	defer k.Close()

	version, _, err := k.GetStringValue("pv")
	if err != nil || version == "" {
		return statusWarn, "注册表项存在但版本号无效", "建议重装 WebView2 运行时"
	}

	v := windows.RtlGetVersion()
	legacy := v.MajorVersion == 6 && v.MinorVersion >= 1
	detail := fmt.Sprintf("系统 WebView2 版本: %s", version)

	if legacy && majorOf(version) > legacyMaxWebview2Major {
		return statusWarn, detail,
			fmt.Sprintf("旧版 Windows 上该版本号异常（应为 %d.x），回退路径可能不可用", legacyMaxWebview2Major)
	}
	return statusPass, detail, ""
}

// checkSQLite 用主程序同一个驱动做一次真实的建库、写入、读取。
//
// 为什么单独验：modernc.org/sqlite 是纯 Go 实现，理论上跨版本无碍，
// 但其 libc 层的 Windows 适配没有针对 Win7 的公开验证，属于本方案的主要未知项。
func checkSQLite(appDir string) (status, string, string) {
	tmpDir, err := os.MkdirTemp(appDir, "selfcheck-sqlite-")
	if err != nil {
		// 安装目录可能只读，退回系统临时目录
		tmpDir, err = os.MkdirTemp("", "selfcheck-sqlite-")
		if err != nil {
			return statusFail, fmt.Sprintf("无法创建临时目录: %v", err), "检查磁盘权限与剩余空间"
		}
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "selfcheck.db")
	// 与 internal/tracer 一致：WAL 模式 + 单连接
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return statusFail, fmt.Sprintf("sql.Open 失败: %v", err), "SQLite 驱动在本机不可用，追踪与日志功能将无法工作"
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		return statusFail, fmt.Sprintf("建表失败: %v", err), "SQLite 无法在本机建库"
	}
	if _, err := db.Exec(`INSERT INTO t (name) VALUES (?)`, "win7"); err != nil {
		return statusFail, fmt.Sprintf("写入失败: %v", err), "SQLite 无法写入"
	}

	var name string
	if err := db.QueryRow(`SELECT name FROM t WHERE id = 1`).Scan(&name); err != nil {
		return statusFail, fmt.Sprintf("读取失败: %v", err), "SQLite 无法读取"
	}
	if name != "win7" {
		return statusFail, fmt.Sprintf("读取结果不符: 期望 win7，实际 %q", name), "SQLite 行为异常"
	}

	var journalMode string
	_ = db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode)

	return statusPass, fmt.Sprintf("建库 / 写入 / 读取全部成功，journal_mode=%s", journalMode), ""
}

// checkURIScheme 校验 my-clouddrive:// 协议注册，以及注册的 exe 是否真实存在。
func checkURIScheme(appDir string) (status, string, string) {
	scheme := readSchemeFromConfig(appDir)
	subKey := `Software\Classes\` + scheme + `\shell\open\command`

	type source struct {
		root registry.Key
		name string
	}
	sources := []source{
		{registry.LOCAL_MACHINE, "HKLM"},
		{registry.CURRENT_USER, "HKCU"},
	}

	for _, s := range sources {
		k, err := registry.OpenKey(s.root, subKey, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		command, _, err := k.GetStringValue("")
		k.Close()
		if err != nil || command == "" {
			continue
		}

		exePath := extractExeFromCommand(command)
		detail := fmt.Sprintf("%s 已注册 %s://\n命令: %s", s.name, scheme, command)
		if exePath == "" {
			return statusWarn, detail, "无法从注册命令中解析出 exe 路径，请人工确认"
		}
		if _, err := os.Stat(exePath); err != nil {
			return statusFail, detail + fmt.Sprintf("\n目标不存在: %s", exePath),
				"协议指向的 exe 不存在，网页唤起会失败。重新运行安装程序可修复"
		}
		return statusPass, detail + fmt.Sprintf("\n目标存在: %s", exePath), ""
	}

	return statusFail, fmt.Sprintf("HKLM 与 HKCU 下均未找到 %s:// 协议注册", scheme),
		"网页无法唤起客户端。以管理员身份重新运行安装程序，或执行 clouddrive-companion.exe --register"
}

// checkLocalPort 验证本地服务端口能否监听。
// 端口被占用时主程序的 HTTP 服务起不来，网页与客户端的通信会全断。
func checkLocalPort(appDir string) (status, string, string) {
	port := *flagPort
	if port == 0 {
		port = readPortFromConfig(appDir)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		ln.Close()
		return statusPass, fmt.Sprintf("端口 %d 可用", port), ""
	}

	// 端口占用：区分"主程序已在运行"和"被别的进程占了"
	if conn, dialErr := net.DialTimeout("tcp", addr, 2*time.Second); dialErr == nil {
		conn.Close()
		return statusWarn, fmt.Sprintf("端口 %d 已被占用（有进程正在监听）", port),
			"如果主程序正在运行，这是预期结果；否则请用 netstat -ano 查明占用进程"
	}
	return statusFail, fmt.Sprintf("端口 %d 无法监听: %v", port, err),
		"修改 config.json 中的 localServerPort，或释放该端口"
}

// checkExternalTools 校验主程序依赖的系统命令行工具。
// 这些工具用于单实例控制（tasklist/taskkill）和端口统计（netstat/netsh）。
func checkExternalTools(string) (status, string, string) {
	tools := []struct {
		name string
		args []string
	}{
		{"tasklist", []string{"/FI", "IMAGENAME eq explorer.exe", "/FO", "CSV", "/NH"}},
		{"taskkill", []string{"/?"}},
		{"netstat", []string{"-an"}},
		{"netsh", []string{"int", "ipv4", "show", "dynamicport", "tcp"}},
	}

	var missing []string
	var okList []string
	for _, t := range tools {
		if _, err := exec.LookPath(t.name); err != nil {
			missing = append(missing, t.name+"(未找到)")
			continue
		}
		if _, err := runCommand(t.name, t.args...); err != nil {
			missing = append(missing, fmt.Sprintf("%s(执行失败: %v)", t.name, err))
			continue
		}
		okList = append(okList, t.name)
	}

	detail := "可用: " + strings.Join(okList, ", ")
	if len(missing) > 0 {
		return statusWarn, detail + "\n异常: " + strings.Join(missing, ", "),
			"单实例控制或端口统计可能失效，不影响上传主流程"
	}
	return statusPass, detail, ""
}

// checkTray 无法自动验证，托盘图标是否显示需要肉眼确认。
func checkTray(string) (status, string, string) {
	if _, err := runCommand("tasklist", "/FI", "IMAGENAME eq explorer.exe", "/FO", "CSV", "/NH"); err != nil {
		return statusWarn, "无法确认 explorer.exe 状态", "托盘依赖资源管理器 shell，请人工确认"
	}
	return statusManual,
		"托盘图标是否显示、右键菜单是否可用，需要启动主程序后肉眼确认",
		"启动主程序，检查通知区域图标及其菜单项（显示上传面板 / 退出）"
}

// ---------------------------------------------------------------- 工具函数

// runCommand 执行外部命令并返回合并输出。隐藏控制台窗口，避免自检时黑框闪烁。
func runCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		out, err = cmd.CombinedOutput()
		close(done)
	}()

	select {
	case <-done:
		return string(out), err
	case <-time.After(30 * time.Second):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return "", fmt.Errorf("执行超时（30s）")
	}
}

// findRuntime 在 appDir/dirName 下定位 msedgewebview2.exe，
// 兼容直接解压和 EBWebView 子目录两种结构。
// 返回运行时目录与可执行文件路径，未找到时均为空。
func findRuntime(appDir, dirName string) (string, string) {
	dir := filepath.Join(appDir, dirName)
	if _, err := os.Stat(dir); err != nil {
		return "", ""
	}
	for _, candidate := range []string{
		filepath.Join(dir, "msedgewebview2.exe"),
		filepath.Join(dir, "EBWebView", "msedgewebview2.exe"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return dir, candidate
		}
	}
	return "", ""
}

// runtimeVersionFromManifest 从固定版本目录里的 <版本号>.manifest 文件名推断版本。
func runtimeVersionFromManifest(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "未知"
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".manifest") {
			return strings.TrimSuffix(e.Name(), ".manifest")
		}
	}
	return "未知"
}

// peSubsystemVersion 读取 PE 可选头里的 MajorSubsystemVersion / MinorSubsystemVersion，
// 即该映像声明的最低运行系统版本（Win7 = 6.1，Win10 = 10.0）。
func peSubsystemVersion(path string) (major, minor uint16, err error) {
	f, err := pe.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	switch oh := f.OptionalHeader.(type) {
	case *pe.OptionalHeader64:
		return oh.MajorSubsystemVersion, oh.MinorSubsystemVersion, nil
	case *pe.OptionalHeader32:
		return oh.MajorSubsystemVersion, oh.MinorSubsystemVersion, nil
	default:
		return 0, 0, fmt.Errorf("无法识别的 PE 可选头类型")
	}
}

// majorOf 取版本串的主版本号，解析失败返回 0。
func majorOf(version string) int {
	parts := strings.Split(strings.TrimSpace(version), ".")
	if len(parts) == 0 {
		return 0
	}
	major := 0
	for _, ch := range parts[0] {
		if ch < '0' || ch > '9' {
			return 0
		}
		major = major*10 + int(ch-'0')
	}
	return major
}

// extractExeFromCommand 从注册表里的 open command 值中取出 exe 路径。
// 典型值形如："C:\path\app.exe" "%1"
func extractExeFromCommand(command string) string {
	command = strings.TrimSpace(command)
	if strings.HasPrefix(command, `"`) {
		if end := strings.Index(command[1:], `"`); end >= 0 {
			return command[1 : end+1]
		}
		return ""
	}
	if idx := strings.Index(strings.ToLower(command), ".exe"); idx >= 0 {
		return command[:idx+4]
	}
	return ""
}

// readPortFromConfig 从安装目录的 config.json 读取本地服务端口，失败时用默认值。
func readPortFromConfig(appDir string) int {
	const defaultPort = 34567
	var cfg struct {
		LocalServerPort int `json:"localServerPort"`
	}
	if data, err := os.ReadFile(filepath.Join(appDir, "config.json")); err == nil {
		if err := json.Unmarshal(data, &cfg); err == nil && cfg.LocalServerPort > 0 {
			return cfg.LocalServerPort
		}
	}
	return defaultPort
}

// readSchemeFromConfig 从安装目录的 config.json 读取 URI 协议名，失败时用默认值。
func readSchemeFromConfig(appDir string) string {
	const defaultScheme = "my-clouddrive"
	var cfg struct {
		URIScheme string `json:"uriScheme"`
	}
	if data, err := os.ReadFile(filepath.Join(appDir, "config.json")); err == nil {
		if err := json.Unmarshal(data, &cfg); err == nil && cfg.URIScheme != "" {
			return cfg.URIScheme
		}
	}
	return defaultScheme
}

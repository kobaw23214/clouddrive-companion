package app

import (
	"clouddrive-companion-withWails2/internal/server"
	"clouddrive-companion-withWails2/internal/utils"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// FileInfo 文件信息结构
type FileInfo struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	IsDir        bool   `json:"isDir"`
	Size         int64  `json:"size"`
	ModifiedTime int64  `json:"modifiedTime"`
}

// knownFolder 一个「快捷入口」目录的解析结果（由平台相关的 shellKnownFolders 产出）
type knownFolder struct {
	Name string
	Path string
	Err  error
}

// volumeDescCache 缓存每个卷的环境描述。网络盘上 GetVolumeInformation 可能较慢，
// 而同一个卷的类型不会变，按卷根缓存一次即可。
var volumeDescCache sync.Map // volumeRoot(string) -> desc(string)

func volumeDesc(path string) string {
	root := filepath.VolumeName(path)
	if cached, ok := volumeDescCache.Load(root); ok {
		return cached.(string)
	}
	desc := describeVolume(path)
	volumeDescCache.Store(root, desc)
	return desc
}

// quickAccessEntries 解析根视图（「此电脑」）顶部的快捷入口：桌面 / 下载 / 文档。
//
// 优先走 Shell 已知文件夹 API（见 fsprobe_windows.go 里的详细说明——重定向场景下
// 拼 %USERPROFILE% 会指到空壳目录，是「原生对话框看得见、本选择器看不见」的主因）。
// Shell 解析全部失败时回退到旧的 HOME 拼接方式，保证至少还有一个桌面入口。
//
// 顺带把「下载」「文档」也做成入口：原先根视图只有桌面 + A-Z 盘符，
// 用户常用位置进不去，只能靠手打路径。
func (a *App) quickAccessEntries() []FileInfo {
	sep := string(os.PathSeparator)
	seen := make(map[string]bool)
	result := make([]FileInfo, 0, 3)

	add := func(name, path string) {
		if path == "" {
			return
		}
		st, err := os.Stat(path)
		if err != nil || !st.IsDir() {
			log.Printf("[fs] 快捷入口不可用 name=%s path=%q err=%v", name, path, err)
			return
		}
		key := strings.ToLower(filepath.Clean(path))
		if seen[key] {
			return
		}
		seen[key] = true
		if !strings.HasSuffix(path, sep) {
			path += sep
		}
		result = append(result, FileInfo{Name: name, Path: path, IsDir: true})
	}

	for _, kf := range shellKnownFolders() {
		if kf.Err != nil {
			log.Printf("[fs] Shell 已知文件夹解析失败 name=%s err=%v", kf.Name, kf.Err)
			continue
		}
		// 这行日志是排查「桌面指错目录」的关键证据：拿它和原生对话框地址栏对账
		log.Printf("[fs] Shell 已知文件夹 name=%s path=%q", kf.Name, kf.Path)
		add(kf.Name, kf.Path)
	}

	if len(result) == 0 {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Printf("[fs] 无法定位用户主目录: %v", err)
			return result
		}
		log.Printf("[fs] Shell 已知文件夹全部不可用，回退到 HOME 拼接 home=%q", home)
		for _, candidate := range []string{
			filepath.Join(home, "Desktop"),
			filepath.Join(home, "桌面"),
		} {
			if st, err := os.Stat(candidate); err == nil && st.IsDir() {
				add("桌面", candidate)
				break
			}
		}
	}

	return result
}

// ListDirectory 列出目录（单层，不递归）。
//
// 每次调用都实时 os.ReadDir，没有任何缓存。日志按调用打一行摘要，便于现场对账
// 「点了刷新按钮到底有没有真的读盘」——这是区分「前端没重新请求」和
// 「读了但确实没这个条目」的第一道判据。
func (a *App) ListDirectory(path string) ([]FileInfo, error) {
	start := time.Now()
	fileInfos := make([]FileInfo, 0)
	sep := string(os.PathSeparator)

	if path == "" || path == "/" {
		quick := a.quickAccessEntries()
		fileInfos = append(fileInfos, quick...)

		drives := make([]string, 0, 26)
		for c := 'A'; c <= 'Z'; c++ {
			drive := string(c) + ":"
			if f, err := os.Stat(drive); err == nil && f.IsDir() {
				drives = append(drives, drive)
				fileInfos = append(fileInfos, FileInfo{
					Name:  drive,
					Path:  drive + sep,
					IsDir: true,
				})
			}
		}
		log.Printf("[fs] ListDirectory root 快捷入口=%d 盘符=%v cost=%s",
			len(quick), drives, time.Since(start))
		return fileInfos, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		log.Printf("[fs] ListDirectory 失败 path=%q %s err=%v", path, volumeDesc(path), err)
		return nil, err
	}

	var skippedSystem, infoFailed int
	for _, entry := range entries {
		name := entry.Name()

		// 只按精确名单排除卷根下的系统目录。两者本身也带 HIDDEN+SYSTEM，
		// 显式列出是为了在属性读不到时仍能过滤掉。
		//
		// 注意：这里**故意不再**按「名字以 . 或 $ 开头」过滤。原实现会把用户自己
		// 建的 .xxx / $xxx 目录一并藏掉，而它们在系统原生对话框里是可见的——
		// 这正是「原生看得见、选择器看不见」的候选原因之一。
		if strings.EqualFold(name, "System Volume Information") ||
			strings.EqualFold(name, "$RECYCLE.BIN") {
			skippedSystem++
			continue
		}

		var size, mtime int64
		info, err := entry.Info()
		if err != nil {
			// 关键改动：不再因为取不到属性就丢弃条目。
			// 原实现在这里直接 continue，是整个函数里唯一能让目录项凭空消失
			// 且不留任何痕迹的地方。宁可少显示大小/时间，也不能让条目不见。
			infoFailed++
			log.Printf("[fs] 条目属性读取失败，仍保留该项 dir=%q name=%q err=%v", path, name, err)
		} else {
			if isHiddenSystemEntry(info) {
				skippedSystem++
				continue
			}
			size = info.Size()
			mtime = info.ModTime().UnixMilli()
		}

		absPath := filepath.Join(path, name)
		if entry.IsDir() && !strings.HasSuffix(absPath, sep) {
			absPath += sep
		}

		fileInfos = append(fileInfos, FileInfo{
			Name:         name,
			Path:         absPath,
			IsDir:        entry.IsDir(),
			Size:         size,
			ModifiedTime: mtime,
		})
	}

	log.Printf("[fs] ListDirectory path=%q %s 原始=%d 返回=%d 跳过系统项=%d 属性失败=%d cost=%s",
		path, volumeDesc(path), len(entries), len(fileInfos), skippedSystem, infoFailed, time.Since(start))

	return fileInfos, nil
}

// GetDesktopPath 获取桌面路径（前端「桌面」按钮用）。
// 走 quickAccessEntries，与根视图里的桌面入口保持同一解析结果。
func (a *App) GetDesktopPath() (string, error) {
	for _, entry := range a.quickAccessEntries() {
		if entry.Name == "桌面" {
			return entry.Path, nil
		}
	}
	return "", fmt.Errorf("未能定位到桌面文件夹")
}

// SubmitSelection 提交选择
func (a *App) SubmitSelection(paths []string) error {
	log.Printf("Wails UI 提交了 %d 个选择: %v", len(paths), paths)

	if a.ctx == nil {
		log.Println("错误：应用上下文未初始化")
		return fmt.Errorf("application context not initialized")
	}

	if len(paths) == 0 {
		log.Println("没有选择任何文件，操作取消")
		return a.CancelSelection()
	}

	// 在启动 goroutine 之前先开启扫描会话，确保浏览器立即查询时也能看到 scanning 状态
	server.BeginScanSession(generateScanTaskId())

	// 异步执行扫描
	go a.performScan(paths)
	return nil
}

// performScan 在 goroutine 中执行文件扫描
func (a *App) performScan(paths []string) {
	defer a.appState.Store(StateIdle)

	sep := string(os.PathSeparator)
	var allSelectedFiles []utils.FullFileInfo

	// 累计已扫描文件数和子文件夹数（跨所有选中路径）
	accumulatedFiles := 0
	accumulatedDirs := 0

	for _, path := range paths {
		cleanPath := strings.TrimSuffix(path, sep)
		baseFiles := accumulatedFiles
		baseDirs := accumulatedDirs
		progressCb := func(filesFound int, dirsFound int, currentPath string) {
			totalFiles := baseFiles + filesFound
			totalDirs := baseDirs + dirsFound
			// 同时更新会话状态（给浏览器轮询用）和发 Wails 事件（给本地窗口动画用）
			server.UpdateScanProgress(totalFiles, totalDirs, currentPath)
			wailsRuntime.EventsEmit(a.ctx, "scan:progress", map[string]interface{}{
				"filesFound":  totalFiles,
				"dirsFound":   totalDirs,
				"currentPath": currentPath,
			})
		}
		files, dirs, scanTraceId, scanSpanId, err := utils.GenerateFileListWithMetadata(cleanPath, progressCb)
		if err != nil {
			log.Printf("处理路径 %s 时出错: %v", cleanPath, err)
			server.FailScanSession(err.Error())
			wailsRuntime.EventsEmit(a.ctx, "scan:error", map[string]string{
				"message": err.Error(),
			})
			return
		}
		allSelectedFiles = append(allSelectedFiles, files...)
		accumulatedFiles += len(files)
		accumulatedDirs += dirs
		// 把当前路径的扫描 trace 信息记到会话里：多路径选择时只保留最后一条，
		// 等用户点上传时通过 ConsumeScanTrace 取走并入 upload trace。
		// 这是个折中——前面几条 scan trace 仍以独立"目录扫描"显示；多路径
		// 同时上传的精确合并较复杂，留给将来优化。
		server.SetScanTrace(scanTraceId, scanSpanId, time.Now().UnixMilli())
	}

	for i := range allSelectedFiles {
		allSelectedFiles[i].Metadata.Index = i
	}
	log.Printf("总共选中 %d 个文件", len(allSelectedFiles))

	// 把结果写入共享列表
	server.SessionMutex.Lock()
	server.CurrentFileList = allSelectedFiles
	server.SessionMutex.Unlock()

	// 标记扫描完成，浏览器下一次轮询就能看到 completed 并拉数据
	server.CompleteScanSession(accumulatedFiles, accumulatedDirs)

	// 通知 Wails 前端切换到 preparing 阶段
	wailsRuntime.EventsEmit(a.ctx, "scan:complete", map[string]interface{}{
		"totalFiles": len(allSelectedFiles),
	})

	// 通知前端复位扫描遮罩。这是**单向通知**，不承担隐藏窗口的职责
	// —— 原因见 app.go OnStartup 里关于 doHide/doCancel 的说明。
	wailsRuntime.EventsEmit(a.ctx, "doHide")

	// 直接隐藏窗口，不依赖前端监听器是否还活着
	a.HideWindow()
}

// generateScanTaskId 生成扫描会话 ID
func generateScanTaskId() string {
	return fmt.Sprintf("scan-%d", time.Now().UnixNano())
}

// CancelSelection 取消选择
func (a *App) CancelSelection() error {
	log.Println("Wails UI 已取消选择")

	if a.ctx == nil {
		log.Println("错误：应用上下文未初始化")
		return fmt.Errorf("application context not initialized")
	}

	defer a.appState.Store(StateIdle)

	// 标记扫描会话已取消，浏览器下一次查询就能看到
	server.CancelScanSession()

	// 通知前端复位扫描遮罩。这是**单向通知**，不承担隐藏窗口的职责
	// —— 原因见 app.go OnStartup 里关于 doHide/doCancel 的说明。
	wailsRuntime.EventsEmit(a.ctx, "doCancel")

	// 直接隐藏窗口，与点窗口 X（OnBeforeClose）走同一套动作，
	// 不依赖前端监听器是否还活着。
	a.HideWindow()

	return nil
}

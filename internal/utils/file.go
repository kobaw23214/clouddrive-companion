package utils

import (
	"clouddrive-companion-withWails2/internal/config"
	"clouddrive-companion-withWails2/internal/tracer"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type FileMetadata struct {
	Index        int    `json:"index"`
	FileName     string `json:"fileName"`
	RelativePath string `json:"relativePath,omitempty"`
	FileSize     int64  `json:"fileSize,omitempty"`
	AbsolutePath string `json:"absolutePath,omitempty"`
}

type FullFileInfo struct {
	Metadata     FileMetadata
	AbsolutePath string
	RelativePath string
	FileSize     int64
}

// ScanProgressCallback 扫描进度回调函数类型
type ScanProgressCallback func(filesFound int, dirsFound int, currentPath string)

// GenerateFileListWithMetadata 生成文件列表及元数据
//
// 链路追踪：扫描默认走独立的 scan trace。后续如果用户对该次扫描结果发起上传，
// 调用方应把返回的 (traceId, scanSpanId) 透传给 upload 流程，由 Manager.Start
// 通过 RetagTrace 把这条 scan trace 并入 upload trace —— 那时整个 trace 的
// trace_type 会被改成 upload，外层列表只显示一条"文件上传"，点进去能看到
// file_scan + metadata_send + upload_task 三段并列的完整时间轴。
//
// 返回值：
//   - fileList   扫描出的文件列表
//   - dirsFound  扫描到的子目录数量（不含根目录本身）
//   - traceId    本次扫描的 traceId（tracer 关闭时仍返回非空，便于占位透传）
//   - scanSpanId 扫描顶层 span 的 spanId（tracer 关闭/失败时为空）
//   - err        扫描错误
func GenerateFileListWithMetadata(selectedPath string, progressCb ScanProgressCallback) (fileList []FullFileInfo, dirsFound int, traceId string, scanSpanId string, err error) {
	traceId, _ = tracer.NewTraceId("")
	scanSpanId = tracer.Start(traceId, "", 1, tracer.TraceTypeScan, tracer.StageFileScan, selectedPath)
	defer func() {
		if scanSpanId == "" {
			return
		}
		if err != nil {
			tracer.Fail(scanSpanId, err.Error())
			return
		}
		tracer.EndWithExtra(scanSpanId, fmt.Sprintf(
			`{"filesFound":%d,"dirsFound":%d}`, len(fileList), dirsFound))
	}()

	startTime := time.Now()
	info, statErr := os.Stat(selectedPath)
	if statErr != nil {
		err = statErr
		return nil, 0, traceId, scanSpanId, err
	}

	if !info.IsDir() {
		// 单文件入口：直接收集，不进入递归子 span
		fileSize := info.Size()
		slashedPath := filepath.ToSlash(selectedPath)
		fileName := filepath.Base(slashedPath)

		metadata := FileMetadata{
			Index:        0,
			FileName:     fileName,
			AbsolutePath: slashedPath,
		}
		if *config.AppConfig.SendRelativePath {
			metadata.RelativePath = fileName
		}
		if *config.AppConfig.SendFileSize {
			metadata.FileSize = fileSize
		}

		fileList = append(fileList, FullFileInfo{
			Metadata:     metadata,
			AbsolutePath: slashedPath,
			RelativePath: fileName,
			FileSize:     fileSize,
		})
	} else {
		// 目录入口：递归扫描，每进入一个子目录就建一条 file_scan 子 span
		baseDir := selectedPath
		parentDir := filepath.Dir(baseDir)
		ctx := &scanCtx{
			parentDir:   parentDir,
			progressCb:  progressCb,
			currentIdx:  0,
			fileList:    &fileList,
			totalDirs:   &dirsFound,
		}
		// 根目录用扫描顶层 span 作 parent，让前端进入 trace 后第一眼就能看到
		// 选中目录下的直接子目录列表。
		walkErr := scanDir(baseDir, scanSpanId, ctx)
		// 走完后再补发一次最终值，避免最后一个不足 50 的尾巴漏掉
		if walkErr == nil && progressCb != nil {
			progressCb(ctx.currentIdx, dirsFound, baseDir)
		}
		if walkErr != nil {
			err = walkErr
			return nil, 0, traceId, scanSpanId, err
		}
	}

	elapsed := time.Since(startTime).Seconds()
	log.Printf("File scan complete, found %d files, %d dirs. Total time: %.2f seconds.", len(fileList), dirsFound, elapsed)

	return fileList, dirsFound, traceId, scanSpanId, nil
}

// scanCtx 把 scanDir 递归过程中需要共享的状态打包成一个结构体，
// 避免函数签名长得像 IDE 的提示框。
type scanCtx struct {
	parentDir  string                // 用于把绝对路径转成相对路径（与单文件分支保持一致）
	progressCb ScanProgressCallback  // 每 50 文件回调一次
	currentIdx int                   // 累计文件序号（FileMetadata.Index）

	fileList  *[]FullFileInfo // 输出：累积文件列表
	totalDirs *int            // 输出：累积子目录数（不含根）
}

// scanDir 递归扫描 dirPath，并在 tracer 中建立对应的子 span。
//
// 每个目录的 span：
//   - filename = 该目录相对于 ctx.parentDir 的路径（与文件 RelativePath 同风格）
//   - extra    = {"filesHere":N, "subdirsHere":M}  仅统计本目录直接含的，不计递归
//   - 失败     = 该目录的 ReadDir 失败时 Fail 该 span，但不中断整体扫描
//
// 返回错误的语义保持跟原 filepath.Walk 实现一致：仅在最顶层 / 不可恢复的情况下
// 抛错；子目录权限错误等非致命问题只 log warn 后继续。
func scanDir(dirPath string, parentSpanId string, ctx *scanCtx) error {
	relDir, _ := filepath.Rel(ctx.parentDir, dirPath)
	spanId := tracer.StartChild(parentSpanId, tracer.StageFileScan, filepath.ToSlash(relDir))

	entries, readErr := os.ReadDir(dirPath)
	if readErr != nil {
		log.Printf("Warning: Accessing path %s failed: %v", dirPath, readErr)
		// 读不到当前目录的 entries → 标记本 span 失败，但返回 nil 让上层继续
		if spanId != "" {
			tracer.Fail(spanId, readErr.Error())
		}
		return nil
	}

	// 排序：保持跨平台输出顺序稳定（filepath.Walk 默认按 lex 顺序，这里显式做一次）
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var filesHere, subdirsHere int
	var subDirs []string
	for _, e := range entries {
		full := filepath.Join(dirPath, e.Name())
		switch {
		case e.IsDir():
			subdirsHere++
			subDirs = append(subDirs, full)
		case e.Type().IsRegular():
			info, infoErr := e.Info()
			if infoErr != nil {
				log.Printf("Warning: stat %s failed: %v", full, infoErr)
				continue
			}
			filesHere++
			relPath, _ := filepath.Rel(ctx.parentDir, full)
			relPathSlashed := filepath.ToSlash(relPath)
			fileSize := info.Size()
			fileName := filepath.Base(full)

			metadata := FileMetadata{
				Index:        ctx.currentIdx,
				FileName:     fileName,
				AbsolutePath: filepath.ToSlash(full),
			}
			if *config.AppConfig.SendRelativePath {
				metadata.RelativePath = relPathSlashed
			}
			if *config.AppConfig.SendFileSize {
				metadata.FileSize = fileSize
			}

			*ctx.fileList = append(*ctx.fileList, FullFileInfo{
				Metadata:     metadata,
				AbsolutePath: filepath.ToSlash(full),
				RelativePath: relPathSlashed,
				FileSize:     fileSize,
			})
			ctx.currentIdx++
			if ctx.progressCb != nil && ctx.currentIdx > 0 && ctx.currentIdx%50 == 0 {
				ctx.progressCb(ctx.currentIdx, *ctx.totalDirs, full)
			}
		}
	}

	// 子目录递归在统计完成后做：让本 span 的"本目录文件 + 子目录数"在 extra 里准确，
	// 也让父 span 时间窗口完整覆盖所有子目录扫描。
	for _, sub := range subDirs {
		*ctx.totalDirs++
		if subErr := scanDir(sub, spanId, ctx); subErr != nil {
			// scanDir 当前永远返回 nil（错误内部已处理），保留接口便于将来需要时使用
			log.Printf("Warning: sub scan %s: %v", sub, subErr)
		}
	}

	if spanId != "" {
		tracer.EndWithExtra(spanId, fmt.Sprintf(
			`{"filesHere":%d,"subdirsHere":%d}`, filesHere, subdirsHere))
	}
	return nil
}

// FilterFilesByIndices 根据索引过滤文件列表
func FilterFilesByIndices(allFiles []FullFileInfo, selectedIndices []int) []FullFileInfo {
	var result []FullFileInfo
	if len(allFiles) == 0 || len(selectedIndices) == 0 {
		return result
	}

	selectedIndexMap := make(map[int]struct{}, len(selectedIndices))
	for _, idx := range selectedIndices {
		selectedIndexMap[idx] = struct{}{}
	}

	for i, file := range allFiles {
		if _, ok := selectedIndexMap[i]; ok {
			result = append(result, file)
		}
	}
	return result
}

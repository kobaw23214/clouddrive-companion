package utils

import (
	"clouddrive-companion-withWails2/internal/config"
	"clouddrive-companion-withWails2/internal/tracer"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withTracerForUtilsTest 在测试期间为 utils 包提供一个独立、干净的 tracer 环境。
func withTracerForUtilsTest(t *testing.T) *tracer.Store {
	t.Helper()

	// 先准备 config 默认值
	yes := true
	no := false
	zero := 0
	old := config.AppConfig
	config.AppConfig = config.Config{
		ChunkSize:              5 * 1024 * 1024,
		SendRelativePath:       &yes,
		SendFileSize:           &no,
		TraceEnabled:           true,
		TraceModuleEnabled:     map[string]bool{},
		TraceSamplingThreshold: &zero,
		TraceMaxAgeDays:        &zero,
		TraceMaxRows:           &zero,
		TraceVacuumThresholdMB: &zero,
	}
	t.Cleanup(func() { config.AppConfig = old })

	// 强制 tracer 重新初始化到测试目录
	tracer.Shutdown()
	dir := t.TempDir()
	if err := tracer.Init(dir); err != nil {
		t.Fatalf("tracer.Init failed: %v", err)
	}
	t.Cleanup(func() { tracer.Shutdown() })
	return tracer.StoreRef()
}

// helper：等待 tracer 后台 writer flush 后再查询
func waitForFlush() {
	time.Sleep(700 * time.Millisecond)
}

func TestGenerateFileListWithMetadata_EmitsScanSpan_Directory(t *testing.T) {
	store := withTracerForUtilsTest(t)

	// 构造临时目录：1 个文件 + 1 个子目录（含 1 个文件）
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	subDir := filepath.Join(dir, "sub")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "b.txt"), []byte("world"), 0644); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}

	files, dirsFound, _, _, err := GenerateFileListWithMetadata(dir, nil)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 files, got %d", len(files))
	}
	if dirsFound != 1 {
		t.Errorf("expected dirsFound=1, got %d", dirsFound)
	}

	// 验证 tracer 中产生了 1 条 scan trace
	waitForFlush()
	summaries, total, err := store.ListTraces(tracer.ListTracesQuery{
		TraceType: tracer.TraceTypeScan,
		Page:      1, Size: 50,
	})
	if err != nil {
		t.Fatalf("ListTraces: %v", err)
	}
	if total != 1 || len(summaries) != 1 {
		t.Fatalf("expected 1 scan trace, got total=%d len=%d", total, len(summaries))
	}
	if summaries[0].HasFailure {
		t.Error("scan should be successful")
	}
	// 1 顶层 file_scan + 2 子目录 span（根目录递归 span + sub 子目录 span）= 3
	if summaries[0].SpanCount != 3 {
		t.Errorf("scan trace should have 3 spans (1 root + 2 dir), got %d", summaries[0].SpanCount)
	}

	// 验证顶层 span 内容：stage、filename、extra
	tops, err := store.GetTopSpans(summaries[0].TraceID)
	if err != nil {
		t.Fatalf("GetTopSpans: %v", err)
	}
	if len(tops) != 1 {
		t.Fatalf("expected 1 top span, got %d", len(tops))
	}
	sp := tops[0]
	if sp.Stage != tracer.StageFileScan {
		t.Errorf("expected stage=%s, got %s", tracer.StageFileScan, sp.Stage)
	}
	if sp.Filename != dir {
		t.Errorf("filename should be the scanned dir, got %q", sp.Filename)
	}
	if !sp.Success {
		t.Error("scan span should be success=true")
	}
	// 顶层 span 的 extra：filesFound=2, dirsFound=1
	expectedTopExtra := `{"filesFound":2,"dirsFound":1}`
	if sp.Extra != expectedTopExtra {
		t.Errorf("top span extra mismatch:\n  got:  %s\n  want: %s", sp.Extra, expectedTopExtra)
	}

	// 顶层下应有 1 条目录子 span（根目录的递归 span）
	rootKids, err := store.GetChildren(sp.SpanID)
	if err != nil {
		t.Fatalf("GetChildren root: %v", err)
	}
	if len(rootKids) != 1 {
		t.Fatalf("expected 1 child span under top, got %d", len(rootKids))
	}
	rootDirSpan := rootKids[0]
	if rootDirSpan.Stage != tracer.StageFileScan {
		t.Errorf("expected child stage=file_scan, got %s", rootDirSpan.Stage)
	}
	// 根目录的递归 span 自己看到的：1 个文件（a.txt）+ 1 个子目录（sub）
	expectedRootDirExtra := `{"filesHere":1,"subdirsHere":1}`
	if rootDirSpan.Extra != expectedRootDirExtra {
		t.Errorf("root-dir span extra mismatch:\n  got:  %s\n  want: %s",
			rootDirSpan.Extra, expectedRootDirExtra)
	}

	// 根目录递归 span 下应再有 1 条 sub 目录的 span
	subKids, err := store.GetChildren(rootDirSpan.SpanID)
	if err != nil {
		t.Fatalf("GetChildren sub: %v", err)
	}
	if len(subKids) != 1 {
		t.Fatalf("expected 1 grand child (sub dir), got %d", len(subKids))
	}
	subDirSpan := subKids[0]
	if subDirSpan.Stage != tracer.StageFileScan {
		t.Errorf("sub span stage wrong: %s", subDirSpan.Stage)
	}
	expectedSubExtra := `{"filesHere":1,"subdirsHere":0}`
	if subDirSpan.Extra != expectedSubExtra {
		t.Errorf("sub-dir span extra mismatch:\n  got:  %s\n  want: %s",
			subDirSpan.Extra, expectedSubExtra)
	}
}

func TestGenerateFileListWithMetadata_EmitsScanSpan_SingleFile(t *testing.T) {
	store := withTracerForUtilsTest(t)

	dir := t.TempDir()
	target := filepath.Join(dir, "single.txt")
	if err := os.WriteFile(target, []byte("solo"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	files, dirsFound, _, _, err := GenerateFileListWithMetadata(target, nil)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("expected 1 file, got %d", len(files))
	}
	if dirsFound != 0 {
		t.Errorf("single file scan should have dirsFound=0, got %d", dirsFound)
	}

	waitForFlush()
	summaries, _, _ := store.ListTraces(tracer.ListTracesQuery{TraceType: tracer.TraceTypeScan})
	if len(summaries) != 1 {
		t.Fatalf("expected 1 scan trace, got %d", len(summaries))
	}
}

func TestGenerateFileListWithMetadata_FailsOnMissingPath(t *testing.T) {
	store := withTracerForUtilsTest(t)

	missing := filepath.Join(t.TempDir(), "no-such-path")
	files, dirsFound, _, _, err := GenerateFileListWithMetadata(missing, nil)
	if err == nil {
		t.Fatal("expected error for missing path")
	}
	if files != nil {
		t.Errorf("expected nil files on error, got %v", files)
	}
	if dirsFound != 0 {
		t.Errorf("expected dirsFound=0 on error, got %d", dirsFound)
	}

	// 失败也应有 1 条 trace，HasFailure=true
	waitForFlush()
	summaries, _, _ := store.ListTraces(tracer.ListTracesQuery{TraceType: tracer.TraceTypeScan})
	if len(summaries) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(summaries))
	}
	if !summaries[0].HasFailure {
		t.Error("missing-path scan should be marked as failed")
	}

	tops, _ := store.GetTopSpans(summaries[0].TraceID)
	if len(tops) != 1 {
		t.Fatalf("expected 1 span, got %d", len(tops))
	}
	if tops[0].ErrorMessage == "" {
		t.Error("error message should be populated for failed scan")
	}
}

func TestGenerateFileListWithMetadata_DisabledTracer_StillFunctions(t *testing.T) {
	withTracerForUtilsTest(t)
	// 关掉 scan 模块；扫描必须仍然正常工作
	tracer.SetModuleEnabled(tracer.TraceTypeScan, false)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("ok"), 0644); err != nil {
		t.Fatalf("write x.txt: %v", err)
	}

	files, dirsFound, _, _, err := GenerateFileListWithMetadata(dir, nil)
	if err != nil {
		t.Fatalf("scan failed when tracer disabled: %v", err)
	}
	if len(files) != 1 || dirsFound != 0 {
		t.Errorf("scan results wrong: files=%d dirsFound=%d", len(files), dirsFound)
	}
	// 不验证 db 行数（tracer 关闭可能不写 trace；这里关键是不崩、不错）
}


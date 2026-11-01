package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// withTempAppDir 在临时目录里准备好 AppSupportDir，并在测试结束时还原全局变量
func withTempAppDir(t *testing.T) string {
	t.Helper()
	old := AppSupportDir
	dir := t.TempDir()
	AppSupportDir = dir
	t.Cleanup(func() {
		AppSupportDir = old
		AppConfig = Config{}
	})
	return dir
}

func TestLoadConfig_CreatesDefaultWhenMissing(t *testing.T) {
	dir := withTempAppDir(t)

	LoadConfig()

	configPath := filepath.Join(dir, "config.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected default config.json to be created, got: %v", err)
	}
	if !AppConfig.TraceEnabled {
		t.Errorf("TraceEnabled should default to true, got false")
	}
	if AppConfig.TraceModuleEnabled == nil {
		t.Error("TraceModuleEnabled must not be nil")
	}
	if AppConfig.TraceSamplingThreshold == nil || *AppConfig.TraceSamplingThreshold != 50_000 {
		t.Errorf("TraceSamplingThreshold default expected 50000, got %v", AppConfig.TraceSamplingThreshold)
	}
	if AppConfig.TraceMaxAgeDays == nil || *AppConfig.TraceMaxAgeDays != 7 {
		t.Errorf("TraceMaxAgeDays default expected 7, got %v", AppConfig.TraceMaxAgeDays)
	}
}

func TestLoadConfig_BackfillsMissingTraceFieldsForOldConfig(t *testing.T) {
	dir := withTempAppDir(t)

	// 模拟老版本 config.json：完全没有 trace 相关字段
	old := DefaultConfig
	old.TraceEnabled = false
	old.TraceModuleEnabled = nil
	old.TraceSamplingThreshold = nil
	old.TraceMaxAgeDays = nil
	old.TraceMaxRows = nil
	old.TraceVacuumThresholdMB = nil
	// 手动写出"老格式"——只保留非 trace 字段
	raw := map[string]interface{}{
		"uriScheme":      old.URIScheme,
		"chunkSize":      old.ChunkSize,
		"maxWorkers":     old.MaxWorkers,
		"localServerPort": old.LocalServerPort,
	}
	data, _ := json.MarshalIndent(raw, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0644); err != nil {
		t.Fatalf("failed to seed config.json: %v", err)
	}

	LoadConfig()

	if !AppConfig.TraceEnabled {
		t.Error("missing traceEnabled in old config should default to true")
	}
	if AppConfig.TraceModuleEnabled == nil {
		t.Error("TraceModuleEnabled must be backfilled to empty map")
	}
	if AppConfig.TraceSamplingThreshold == nil || *AppConfig.TraceSamplingThreshold != 50_000 {
		t.Error("TraceSamplingThreshold must be backfilled to 50000")
	}
}

func TestLoadConfig_ExplicitFalseTraceEnabledRespected(t *testing.T) {
	dir := withTempAppDir(t)

	cfg := DefaultConfig
	cfg.TraceEnabled = false
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0644); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	LoadConfig()

	if AppConfig.TraceEnabled {
		t.Error("explicit traceEnabled=false should be preserved, got true")
	}
}

func TestLoadConfig_ExplicitZeroSamplingThresholdRespected(t *testing.T) {
	dir := withTempAppDir(t)

	zero := 0
	cfg := DefaultConfig
	cfg.TraceSamplingThreshold = &zero
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0644); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	LoadConfig()

	if AppConfig.TraceSamplingThreshold == nil {
		t.Fatal("explicit 0 should be preserved as &0, not backfilled to default")
	}
	if *AppConfig.TraceSamplingThreshold != 0 {
		t.Errorf("expected 0, got %d", *AppConfig.TraceSamplingThreshold)
	}
}

func TestSave_PersistsAndRoundTrips(t *testing.T) {
	dir := withTempAppDir(t)
	LoadConfig() // 创建默认配置

	// 改一些字段
	AppConfig.TraceEnabled = false
	AppConfig.TraceModuleEnabled["upload"] = false
	threshold := 12345
	AppConfig.TraceSamplingThreshold = &threshold

	if err := Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// 重新加载，验证持久化生效
	AppConfig = Config{}
	LoadConfig()

	if AppConfig.TraceEnabled {
		t.Error("TraceEnabled change not persisted")
	}
	if AppConfig.TraceModuleEnabled["upload"] {
		t.Error("TraceModuleEnabled[upload]=false not persisted")
	}
	if AppConfig.TraceSamplingThreshold == nil || *AppConfig.TraceSamplingThreshold != 12345 {
		t.Errorf("TraceSamplingThreshold not persisted: %v", AppConfig.TraceSamplingThreshold)
	}
	// 再次确认 config.json 文件确实存在且非空
	info, err := os.Stat(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("config.json missing after Save: %v", err)
	}
	if info.Size() == 0 {
		t.Error("config.json is empty after Save")
	}
}

func TestSave_FailsWithoutAppSupportDir(t *testing.T) {
	old := AppSupportDir
	AppSupportDir = ""
	t.Cleanup(func() { AppSupportDir = old })

	if err := Save(); err == nil {
		t.Error("expected error when AppSupportDir is empty")
	}
}

func TestSave_AtomicReplace_NoTmpRemains(t *testing.T) {
	dir := withTempAppDir(t)
	LoadConfig()

	if err := Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	tmp := filepath.Join(dir, "config.json.tmp")
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("config.json.tmp should not remain after successful Save, stat err=%v", err)
	}
}

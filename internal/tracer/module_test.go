package tracer

import (
	"clouddrive-companion-withWails2/internal/config"
	"errors"
	"testing"
)

// resetModuleEnv 让每个测试都从干净的 config 与 persistFn stub 开始。
func resetModuleEnv(t *testing.T) {
	t.Helper()
	oldGlobal := config.AppConfig.TraceEnabled
	oldMap := config.AppConfig.TraceModuleEnabled
	oldPersist := persistFn

	config.AppConfig.TraceEnabled = true
	config.AppConfig.TraceModuleEnabled = map[string]bool{}
	persistFn = func() error { return nil }

	t.Cleanup(func() {
		config.AppConfig.TraceEnabled = oldGlobal
		config.AppConfig.TraceModuleEnabled = oldMap
		persistFn = oldPersist
	})
}

func TestModuleManager_Register_DefaultEnabled(t *testing.T) {
	resetModuleEnv(t)
	m := newModuleManager()
	m.register("upload", "上传链路", "测试用")

	mods := m.list()
	if len(mods) != 1 {
		t.Fatalf("expected 1 module, got %d", len(mods))
	}
	if !mods[0].Enabled {
		t.Error("newly registered module should be enabled by default")
	}
}

func TestModuleManager_Register_RespectsPersistedDisabled(t *testing.T) {
	resetModuleEnv(t)
	config.AppConfig.TraceModuleEnabled["upload"] = false

	m := newModuleManager()
	m.register("upload", "上传链路", "")

	if m.isEnabled("upload") {
		t.Error("should respect persisted disabled state")
	}
}

func TestModuleManager_Register_DuplicateNoOverwrite(t *testing.T) {
	resetModuleEnv(t)
	m := newModuleManager()
	m.register("upload", "first", "")
	// 模拟期间用户禁用
	m.setEnabled("upload", false)

	// 重复注册不应覆盖
	m.register("upload", "first", "")
	if m.isEnabled("upload") {
		t.Error("duplicate register must not reset Enabled state")
	}
}

func TestModuleManager_Register_EmptyCodeIgnored(t *testing.T) {
	resetModuleEnv(t)
	m := newModuleManager()
	m.register("", "", "")
	if len(m.list()) != 0 {
		t.Error("empty code should be ignored")
	}
}

func TestModuleManager_IsEnabled_GlobalOverridesModule(t *testing.T) {
	resetModuleEnv(t)
	m := newModuleManager()
	m.register("upload", "", "")

	config.AppConfig.TraceEnabled = false
	if m.isEnabled("upload") {
		t.Error("global off must force module result to false")
	}
}

func TestModuleManager_IsEnabled_UnknownModuleDefaultsTrue(t *testing.T) {
	resetModuleEnv(t)
	m := newModuleManager()
	if !m.isEnabled("never-registered") {
		t.Error("unregistered traceType should default to enabled")
	}
}

func TestModuleManager_SetEnabled_PersistsToConfig(t *testing.T) {
	resetModuleEnv(t)
	persisted := false
	persistFn = func() error {
		persisted = true
		return nil
	}

	m := newModuleManager()
	m.register("upload", "", "")

	if !m.setEnabled("upload", false) {
		t.Fatal("setEnabled returned false for existing module")
	}
	if !persisted {
		t.Error("persistFn should have been called")
	}
	if v := config.AppConfig.TraceModuleEnabled["upload"]; v != false {
		t.Errorf("config.TraceModuleEnabled[upload] should be false, got %v", v)
	}
}

func TestModuleManager_SetEnabled_UnknownReturnsFalse(t *testing.T) {
	resetModuleEnv(t)
	m := newModuleManager()
	if m.setEnabled("nope", true) {
		t.Error("setEnabled for unknown module should return false")
	}
}

func TestModuleManager_SetEnabled_PersistFailureKeepsRuntimeState(t *testing.T) {
	resetModuleEnv(t)
	persistFn = func() error { return errors.New("disk full") }

	m := newModuleManager()
	m.register("upload", "", "")

	if !m.setEnabled("upload", false) {
		t.Fatal("setEnabled should return true even when persist fails")
	}
	if m.isEnabled("upload") {
		t.Error("runtime state should be updated even if persist failed (UI consistency)")
	}
}

func TestModuleManager_List_SnapshotIsolation(t *testing.T) {
	resetModuleEnv(t)
	m := newModuleManager()
	m.register("upload", "", "")

	snap := m.list()
	snap[0].Enabled = false // 改快照不应影响内部状态

	if !m.isEnabled("upload") {
		t.Error("list() must return a snapshot, not a live reference")
	}
}

func TestSetGlobalEnabled_PersistsAndReturnsTrue(t *testing.T) {
	resetModuleEnv(t)
	persisted := false
	persistFn = func() error {
		persisted = true
		return nil
	}

	if !setGlobalEnabled(false) {
		t.Error("expected setGlobalEnabled to return true on persist success")
	}
	if config.AppConfig.TraceEnabled {
		t.Error("config.TraceEnabled should be false")
	}
	if !persisted {
		t.Error("persistFn should have been called")
	}
}

func TestSetGlobalEnabled_PersistFailureReturnsFalse(t *testing.T) {
	resetModuleEnv(t)
	persistFn = func() error { return errors.New("io err") }

	ok := setGlobalEnabled(false)
	if ok {
		t.Error("persist failure should make setGlobalEnabled return false")
	}
	// 即便如此，运行时状态已经修改（与 setEnabled 行为一致）
	if config.AppConfig.TraceEnabled {
		t.Error("runtime state should still be updated for UI consistency")
	}
}

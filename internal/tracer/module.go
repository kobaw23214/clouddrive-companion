package tracer

import (
	"clouddrive-companion-withWails2/internal/config"
	"log"
	"sync"
)

// ModuleManager 维护所有可开关的追踪模块。
//
// 与 config.AppConfig 联动：
//   - 模块的 Enabled 字段是运行时的真实状态
//   - register 时从 config.TraceModuleEnabled[code] 读初始值，未配置视为 true
//   - setEnabled 时把变更回写到 config.AppConfig.TraceModuleEnabled 并调用 config.Save()
//   - isEnabled 时同时检查全局开关 config.TraceEnabled
//
// 之所以保留一份内存视图（而不是每次都查 config），是因为 isEnabled 在
// Start/StartChild 的热路径上被频繁调用，避免触发额外的 map 复制和锁竞争。
type ModuleManager struct {
	mu      sync.RWMutex
	modules map[string]*TraceModule
}

// persistFn 把 config.AppConfig 写回 config.json；提取为变量以便单元测试 stub。
var persistFn = config.Save

func newModuleManager() *ModuleManager {
	return &ModuleManager{modules: make(map[string]*TraceModule)}
}

// register 注册一个追踪模块。已存在的 code 会被忽略（不覆盖既有 Enabled）。
//
// 初始 Enabled 来自 config.TraceModuleEnabled[code]，缺省为 true。
func (m *ModuleManager) register(code, name, description string) {
	if code == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.modules[code]; ok {
		return
	}
	enabled := readPersistedEnabled(code)
	m.modules[code] = &TraceModule{
		Code:        code,
		Name:        name,
		Description: description,
		Enabled:     enabled,
	}
	log.Printf("[tracer] 注册模块 %s (%s) enabled=%t", code, name, enabled)
}

// readPersistedEnabled 从 config 中读取持久化开关；缺省为 true。
func readPersistedEnabled(code string) bool {
	if config.AppConfig.TraceModuleEnabled == nil {
		return true
	}
	if v, ok := config.AppConfig.TraceModuleEnabled[code]; ok {
		return v
	}
	return true
}

// isEnabled 判断某 traceType 当前是否启用。
//
//	1. 全局开关关 → false
//	2. 模块未注册 → 默认 true（"未知 traceType 不应被悄悄丢弃"）
//	3. 模块已注册 → 看 module.Enabled
func (m *ModuleManager) isEnabled(traceType string) bool {
	if !config.AppConfig.TraceEnabled {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if mod, ok := m.modules[traceType]; ok {
		return mod.Enabled
	}
	return true
}

// setEnabled 切换某模块的开关，并持久化到 config.json。
//
// 返回 false 表示模块不存在；此时 config 不会被修改。
func (m *ModuleManager) setEnabled(code string, enabled bool) bool {
	m.mu.Lock()
	mod, ok := m.modules[code]
	if !ok {
		m.mu.Unlock()
		return false
	}
	mod.Enabled = enabled
	m.mu.Unlock()

	// 同步到 config.AppConfig 并落盘
	if config.AppConfig.TraceModuleEnabled == nil {
		config.AppConfig.TraceModuleEnabled = map[string]bool{}
	}
	config.AppConfig.TraceModuleEnabled[code] = enabled
	if err := persistFn(); err != nil {
		log.Printf("[tracer] 持久化模块开关失败 (%s=%t): %v", code, enabled, err)
		// 注意：内存状态已经修改，但磁盘没写成功；下次重启会丢失这次切换。
		// 这里不回滚内存状态，保持运行时行为符合用户预期（用户在 UI 上看到的就是新值）。
	}
	return true
}

// list 返回所有已注册模块的快照（拷贝）。
func (m *ModuleManager) list() []TraceModule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]TraceModule, 0, len(m.modules))
	for _, v := range m.modules {
		out = append(out, *v)
	}
	return out
}

// setGlobalEnabled 切换全局总开关并持久化。返回 false 表示持久化失败。
//
// 与 setEnabled 不同的是，全局开关不存在"模块不存在"的失败分支。
func setGlobalEnabled(enabled bool) bool {
	config.AppConfig.TraceEnabled = enabled
	if err := persistFn(); err != nil {
		log.Printf("[tracer] 持久化全局开关失败 (enabled=%t): %v", enabled, err)
		return false
	}
	return true
}

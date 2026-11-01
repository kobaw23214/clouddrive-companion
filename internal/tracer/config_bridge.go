package tracer

import "clouddrive-companion-withWails2/internal/config"

// 本文件集中所有"从 config.AppConfig 读取 tracer 相关配置"的访问器，
// 便于：
//   1. 测试时 stub（store / sampler 测试都以函数指针注入）
//   2. 区分"指针字段为 nil"与"显式 0"两种语义
//
// 命名约定：currentXxx 表示读取当前值；返回 0 含义视字段而定。

// currentMaxAgeDays 保留天数。返回 0 表示禁用按时间清理。
func currentMaxAgeDays() int {
	if config.AppConfig.TraceMaxAgeDays == nil {
		return 0
	}
	return *config.AppConfig.TraceMaxAgeDays
}

// currentMaxRows 最大行数。返回 0 表示禁用按行数清理。
func currentMaxRows() int {
	if config.AppConfig.TraceMaxRows == nil {
		return 0
	}
	return *config.AppConfig.TraceMaxRows
}

// currentVacuumThresholdMB VACUUM 触发阈值（MB）。返回 0 表示禁用 VACUUM。
func currentVacuumThresholdMB() int {
	if config.AppConfig.TraceVacuumThresholdMB == nil {
		return 0
	}
	return *config.AppConfig.TraceVacuumThresholdMB
}

/**
 * Stage 元数据：中文显示名 + 配色映射 + traceType 元数据。
 *
 * 与 internal/tracer/types.go 中的 stage 枚举常量保持一一对应；
 * 后端新增 stage 时前端仅需更新这两张表。
 *
 * 颜色不直接写 hex，而是引用 tokens.css 中的 CSS 变量，
 * 保证主题切换（日后可能加暗色）只需改一个地方。
 */

// ─── stage 枚举（与后端 internal/tracer/types.go 同步）───
export const STAGES = Object.freeze({
  FILE_SCAN:        'file_scan',
  METADATA_SEND:    'metadata_send',
  UPLOAD_TASK:      'upload_task',
  UPLOAD_FILE:      'upload_file',
  MD5_CALC:         'md5_calc',
  CHECK_CHUNKS:     'check_chunks',
  CHUNK_UPLOAD:     'chunk_upload',
  SERVER_MERGE:     'server_merge',
  NOTIFY_COMPLETE:  'notify_complete',
  OTHER:            'other',
});

/**
 * 中文显示名。fallback 见 stageLabel()。
 */
export const STAGE_LABELS = Object.freeze({
  [STAGES.FILE_SCAN]:       '目录扫描',
  [STAGES.METADATA_SEND]:   '发送文件列表',
  [STAGES.UPLOAD_TASK]:     '上传任务',
  [STAGES.UPLOAD_FILE]:     '文件上传',
  [STAGES.MD5_CALC]:        'MD5 计算',
  [STAGES.CHECK_CHUNKS]:    '查询已传分片',
  [STAGES.CHUNK_UPLOAD]:    '分片上传',
  [STAGES.SERVER_MERGE]:    '服务端合并',
  [STAGES.NOTIFY_COMPLETE]: '通知后端合并',
  [STAGES.OTHER]:           '其他',
});

/**
 * Stage 配色：值是 CSS 变量名（不含 var(...) 包装），
 * 调用方通过 stageColor(stage) 拿到完整 CSS 表达式。
 */
const STAGE_COLOR_VARS = Object.freeze({
  [STAGES.FILE_SCAN]:       '--tt-stage-scan',
  [STAGES.METADATA_SEND]:   '--tt-stage-meta',
  [STAGES.UPLOAD_TASK]:     '--tt-stage-task',
  [STAGES.UPLOAD_FILE]:     '--tt-stage-file',
  [STAGES.MD5_CALC]:        '--tt-stage-md5',
  [STAGES.CHECK_CHUNKS]:    '--tt-stage-check',
  [STAGES.CHUNK_UPLOAD]:    '--tt-stage-chunk',
  [STAGES.SERVER_MERGE]:    '--tt-stage-merge',
  [STAGES.NOTIFY_COMPLETE]: '--tt-stage-notify',
  [STAGES.OTHER]:           '--tt-stage-other',
});

/**
 * 返回 stage 的中文显示名。未知 stage 直接返回原字符串作为 fallback。
 */
export function stageLabel(stage) {
  if (!stage) return '—';
  return STAGE_LABELS[stage] || stage;
}

/**
 * 返回 stage 对应的 CSS 表达式（用于 inline style 或 ECharts itemStyle.color）。
 *
 * 示例：
 *   element.style.background = stageColor('md5_calc');
 *   echartsItemStyle.color = stageColor(stage);
 */
export function stageColor(stage) {
  const cssVarName = STAGE_COLOR_VARS[stage] || STAGE_COLOR_VARS[STAGES.OTHER];
  return `var(${cssVarName})`;
}

/**
 * 给 ECharts 等不能直接吃 CSS 变量的库用：把 stage 解析为真实的颜色字符串。
 *
 * 由于 CSS 变量取值依赖 DOM 上下文，这里需要传入一个挂载点元素以便正确求值；
 * 若不传则使用 documentElement（:root），通常已经能拿到 tokens.css 定义的值。
 */
export function resolveStageColor(stage, hostEl) {
  const cssVarName = STAGE_COLOR_VARS[stage] || STAGE_COLOR_VARS[STAGES.OTHER];
  const el = hostEl || (typeof document !== 'undefined' ? document.documentElement : null);
  if (!el) return '#cbd5e1';
  return getComputedStyle(el).getPropertyValue(cssVarName).trim() || '#cbd5e1';
}

// ─── traceType 元数据 ───
export const TRACE_TYPES = Object.freeze({
  UPLOAD: 'upload',
  SCAN:   'scan',
});

export const TRACE_TYPE_LABELS = Object.freeze({
  [TRACE_TYPES.UPLOAD]: '文件上传',
  [TRACE_TYPES.SCAN]:   '目录扫描',
});

export function traceTypeLabel(type) {
  return TRACE_TYPE_LABELS[type] || type || '未知';
}

// ─── 时间 / 耗时格式化（与 Dashboard.vue 现有 formatMs 风格一致）───

/**
 * 把毫秒数格式化为可读字符串：
 *   < 1s      → "123 ms"
 *   < 60s     → "1.5 s"
 *   < 60min   → "2.3 min"
 *   else      → "1.2 h"
 */
export function formatMs(ms) {
  if (ms === null || ms === undefined || ms < 0) return '—';
  const n = Number(ms);
  if (!Number.isFinite(n)) return '—';
  if (n < 1000) return `${Math.round(n)} ms`;
  if (n < 60_000) return `${(n / 1000).toFixed(1)} s`;
  if (n < 3_600_000) return `${(n / 60_000).toFixed(1)} min`;
  return `${(n / 3_600_000).toFixed(1)} h`;
}

/**
 * 时间戳（毫秒）→ 本地时间字符串 "HH:mm:ss"
 */
export function formatTimeShort(ms) {
  if (!ms) return '—';
  const d = new Date(ms);
  return d.toLocaleTimeString('zh-CN', { hour12: false });
}

/**
 * 时间戳（毫秒）→ 本地完整时间 "YYYY-MM-DD HH:mm:ss"
 */
export function formatTimeFull(ms) {
  if (!ms) return '—';
  const d = new Date(ms);
  const pad = (x) => String(x).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ` +
         `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

/**
 * 字节数 → 人类可读
 */
export function formatBytes(bytes) {
  if (bytes === null || bytes === undefined) return '—';
  const n = Number(bytes);
  if (!Number.isFinite(n)) return '—';
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

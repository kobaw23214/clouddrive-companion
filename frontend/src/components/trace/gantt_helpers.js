/**
 * TraceGantt 内部使用的纯函数工具，提取出来便于单元测试。
 *
 * 设计：把不依赖 ECharts/DOM 的逻辑剥离出来，让 TraceGantt.vue 自己只关心
 * ECharts 生命周期与 option 拼装；本文件仅处理"数据 → 渲染参数"的纯计算。
 */

/**
 * 转义 HTML，避免 span.extra / errorMessage 中的 < > & 注入 tooltip。
 */
export function escapeHtml(s) {
  if (s === null || s === undefined) return '';
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

/**
 * hex/CSS 颜色 → rgba()。
 *
 *   #2563eb, 0.12 → "rgba(37,99,235,0.12)"
 *   "#abc",   0.5  → "rgba(170,187,204,0.5)"
 *   非法输入       → 默认蓝 fallback
 *
 * 已经是 rgba/hsl 等格式时原样返回。
 */
export function hexToRgba(c, alpha) {
  if (!c || typeof c !== 'string') return `rgba(37, 99, 235, ${alpha})`;
  const v = c.trim();
  if (v.startsWith('#')) {
    let h = v.slice(1);
    if (h.length === 3) h = h.split('').map((x) => x + x).join('');
    if (h.length !== 6) return `rgba(37, 99, 235, ${alpha})`;
    const r = parseInt(h.slice(0, 2), 16);
    const g = parseInt(h.slice(2, 4), 16);
    const b = parseInt(h.slice(4, 6), 16);
    if ([r, g, b].some(Number.isNaN)) return `rgba(37, 99, 235, ${alpha})`;
    return `rgba(${r},${g},${b},${alpha})`;
  }
  return v;
}

/**
 * 计算甘特图 x 轴的 [min, max, pad]。
 *
 * @param {Array} spans
 * @param {Object} parentWindow  可选 { startMs, endMs }
 * @param {number} now           当前时间，用于 in-progress span 的 endMs 兜底
 */
export function computeTimeRange(spans, parentWindow, now) {
  const ts = [];
  let hasInProgress = false;
  for (const s of spans || []) {
    if (Number.isFinite(s.startMs)) ts.push(s.startMs);
    if (Number.isFinite(s.endMs) && s.endMs > 0) {
      ts.push(s.endMs);
    } else if (Number.isFinite(s.startMs)) {
      // in-progress span：endMs<=0 视作延伸到 now
      hasInProgress = true;
    }
  }
  if (parentWindow) {
    if (Number.isFinite(parentWindow.startMs)) ts.push(parentWindow.startMs);
    if (Number.isFinite(parentWindow.endMs) && parentWindow.endMs > 0) {
      ts.push(parentWindow.endMs);
    }
  }
  if (hasInProgress) ts.push(now);
  if (!ts.length) {
    return { min: now, max: now, pad: 1 };
  }
  const min = Math.min(...ts);
  const max = Math.max(...ts);
  const span = Math.max(1, max - min);
  return { min, max, pad: span * 0.05 };
}

/**
 * 把 span 转为 ECharts custom series 一行数据。
 *
 * 返回值的 value 数组顺序固定：
 *   0 yIdx
 *   1 startMs
 *   2 endMs（in-progress 用 now 兜底）
 *   3 durationMs
 *   4 stage
 *   5 label
 *   6 spanId
 *   7 success(0/1)
 *   8 inProgress(0/1)
 */
export function buildSeriesPoint(span, yIdx, label, now) {
  const inProgress = !span.endMs || span.endMs <= 0;
  const start = Number.isFinite(span.startMs) ? span.startMs : 0;
  const end = inProgress ? now : Math.max(start, span.endMs);
  return {
    name: span.spanId || `span-${yIdx}`,
    value: [
      yIdx,
      start,
      end,
      span.durationMs ?? (end - start),
      span.stage,
      label,
      span.spanId,
      span.success === false ? 0 : 1,
      inProgress ? 1 : 0,
    ],
    span,
  };
}

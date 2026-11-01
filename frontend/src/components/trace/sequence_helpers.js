/**
 * TraceSequence（HTTP 交互时序图）内部使用的纯函数工具，提取出来便于单元测试。
 *
 * 与 gantt_helpers.js 的设计一致：把不依赖 ECharts/DOM 的逻辑剥离出来，
 * TraceSequence.vue 只关心 ECharts 生命周期与 option 拼装；
 * 本文件仅处理"HTTP 日志事件 → 渲染参数"的纯计算。
 *
 * 事件字段（camelCase，与 internal/server.HTTPRequestLog 的 JSON tag 一致）：
 *   id / timestampMs / method / path / query / statusCode / durationMs /
 *   direction('in'|'out') / peer / taskId
 */

/**
 * 泳道（固定顺序，自上而下）：
 *   0 浏览器   —— 入站请求的发起方（浏览器上传页）
 *   1 客户端   —— 本地服务（:34567），入站的接收方 / 出站的发起方
 *   2 远端后端 —— 出站请求的接收方
 */
export const LANES = Object.freeze(['浏览器', '客户端', '远端后端']);

/**
 * direction → 泳道下标对。
 *   'in'  : 浏览器(0) → 客户端(1)
 *   'out' : 客户端(1) → 远端后端(2)
 *   其他/缺失：按 'in' 处理（本地服务收到的请求是最常见情况）
 */
export function lanePair(direction) {
  return direction === 'out' ? { from: 1, to: 2 } : { from: 0, to: 1 };
}

/**
 * 状态码分级：
 *   'fail' —— statusCode >= 400，或 0 / 非法值（0 表示网络错误，未拿到响应）
 *   'ok'   —— 其余（2xx/3xx/1xx）
 */
export function statusKind(statusCode) {
  const c = Number(statusCode);
  if (!Number.isFinite(c) || c <= 0 || c >= 400) return 'fail';
  return 'ok';
}

/**
 * 事件配色（返回 tokens.css 中的 CSS 变量名，由组件在 ECharts 侧解析成真颜色）。
 *   失败           → --tt-seq-fail（红）
 *   出站（out）正常 → --tt-seq-out（蓝）
 *   入站（in）正常  → --tt-seq-in（绿）
 */
export function colorVarFor(direction, kind) {
  if (kind === 'fail') return '--tt-seq-fail';
  return direction === 'out' ? '--tt-seq-out' : '--tt-seq-in';
}

/**
 * direction 的可读标签（tooltip / 过滤选项共用）。
 */
export function directionLabel(direction) {
  return direction === 'out' ? '客户端 → 远端后端' : '浏览器 → 客户端';
}

/**
 * 事件结束时刻 = timestampMs + max(durationMs, 0)。
 * timestampMs 非法时返回 NaN（调用方负责跳过）。
 */
export function eventEndMs(log) {
  if (!log) return NaN;
  const start = Number(log.timestampMs);
  if (!Number.isFinite(start)) return NaN;
  const dur = Number(log && log.durationMs);
  return start + (Number.isFinite(dur) && dur > 0 ? dur : 0);
}

/**
 * 把事件数组转为 ECharts custom series 数据。
 *
 * 没有时间戳（timestampMs 非法）的事件无法落图，直接跳过。
 *
 * 返回项的 value 数组顺序固定：
 *   0 fromLane   发起方泳道下标
 *   1 toLane     接收方泳道下标
 *   2 startMs    请求发出时刻
 *   3 endMs      处理完成时刻（startMs + durationMs）
 *   4 durationMs 处理耗时（>= 0）
 *   5 kind       'ok' | 'fail'
 *   6 direction  'in' | 'out'
 *   7 colorVar   tokens.css 变量名
 * 原始事件挂在返回项的 log 字段上，供 tooltip / 点击回调使用。
 */
export function buildSequencePoints(events) {
  const out = [];
  for (const log of events || []) {
    if (!log) continue;
    const startMs = Number(log.timestampMs);
    if (!Number.isFinite(startMs)) continue;
    const endMs = eventEndMs(log);
    const kind = statusKind(log.statusCode);
    const { from, to } = lanePair(log.direction);
    out.push({
      name: `${log.method || ''} ${log.path || ''}`.trim() || `event-${out.length}`,
      value: [
        from,
        to,
        startMs,
        endMs,
        Math.max(0, endMs - startMs),
        kind,
        log.direction || 'in',
        colorVarFor(log.direction, kind),
      ],
      log,
    });
  }
  return out;
}

/**
 * 计算时序图 X 轴的 [min, max, pad]。
 * 覆盖所有事件的 [timestampMs, timestampMs + durationMs] 区间。
 * 没有可落图事件时返回 null（组件此时显示空态，不渲染图表）。
 */
export function computeSequenceTimeRange(events) {
  const ts = [];
  for (const log of events || []) {
    if (!log) continue;
    const s = Number(log.timestampMs);
    if (!Number.isFinite(s)) continue;
    ts.push(s);
    ts.push(eventEndMs(log));
  }
  if (!ts.length) return null;
  const min = Math.min(...ts);
  const max = Math.max(...ts);
  const span = Math.max(1, max - min);
  return { min, max, pad: span * 0.05 };
}

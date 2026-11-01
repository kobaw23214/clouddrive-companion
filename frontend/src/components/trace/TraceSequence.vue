<template>
  <!--
    HTTP 交互时序图（泳道式，X 轴 = 时间）
      - 三条泳道（固定顺序）：浏览器 / 客户端 / 远端后端
      - direction='in'  → 浏览器 → 客户端；direction='out' → 客户端 → 远端后端
      - 每个请求-响应事件：
          timestampMs 处            发起方泳道 → 接收方泳道的垂直实线箭头
          [start, start+duration]   接收方泳道上的处理横条（圆角矩形，风格同 TraceGantt）
          start+duration 处         接收方 → 发起方的虚线返回箭头
      - 配色：入站绿 / 出站蓝 / 失败红（tokens.css 的 --tt-seq-*）
      - 交互与 TraceGantt 一致：顶部 slider dataZoom + 滚轮平移 Y + Ctrl+滚轮缩放 X
      - hover 显示事件详情 tooltip，点击 emit('event-click', log)
  -->
  <div ref="hostRef" class="trace-seq-host" :style="hostStyle">
    <!-- 顶部 X 时间窗信息条：常驻显示 [起始 — 持续 — 结束] -->
    <div v-if="hasData" class="trace-seq-zoom-info">
      <span class="trace-seq-zoom-left tt-num">{{ zoomLabels.start }}</span>
      <span class="trace-seq-zoom-mid tt-meta">{{ zoomLabels.middle }}</span>
      <span class="trace-seq-zoom-right tt-num">{{ zoomLabels.end }}</span>
    </div>

    <div ref="chartRef" class="trace-seq-chart"></div>

    <div v-if="!hasData" class="tt-empty trace-seq-empty">
      <span class="tt-meta">{{ loading ? '加载中…' : emptyText }}</span>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, watch, nextTick } from 'vue';
import * as echarts from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { CustomChart } from 'echarts/charts';
import { GridComponent, TooltipComponent, DataZoomComponent } from 'echarts/components';
import { formatMs, formatTimeShort, formatTimeFull } from './stage_meta.js';
import { escapeHtml, hexToRgba } from './gantt_helpers.js';
import {
  LANES,
  buildSequencePoints,
  computeSequenceTimeRange,
  directionLabel,
} from './sequence_helpers.js';

echarts.use([CanvasRenderer, CustomChart, GridComponent, TooltipComponent, DataZoomComponent]);

const props = defineProps({
  /** HTTP 日志事件数组（internal/server.HTTPRequestLog 的 JSON 形态） */
  events: { type: Array, default: () => [] },

  /** 加载中标记：空态时显示"加载中…"而不是空占位 */
  loading: { type: Boolean, default: false },

  /**
   * 组件高度：数字按 px、字符串原样用作 CSS height；
   * 0 / 未传时由父容器决定（host 是 flex 列，图表区 flex:1）。
   */
  height: { type: [Number, String], default: 0 },

  /** 空态文案 */
  emptyText: { type: String, default: '暂无 HTTP 交互记录' },
});

const emit = defineEmits(['event-click']);

const hostRef = ref(null);
const chartRef = ref(null);
let chart = null;

// 缓存数据，便于事件回调与 renderItem 闭包使用
let timeRange = null;   // { min, max, pad }
let seqColors = {};     // tokens 变量名 → 解析后的真实颜色

const hasData = computed(() => Array.isArray(props.events) && props.events.length > 0);

const hostStyle = computed(() => {
  if (!props.height) return {};
  const h = typeof props.height === 'number' ? `${props.height}px` : String(props.height);
  return { height: h };
});

// 顶部时间信息条（左：起始；中：持续；右：结束）
const zoomLabels = reactive({ start: '—', middle: '—', end: '—' });

function getCss(varName, fallback = '') {
  if (!hostRef.value) return fallback;
  return getComputedStyle(hostRef.value).getPropertyValue(varName).trim() || fallback;
}

// ─── ECharts option ───
function buildOption() {
  if (!hasData.value) return {};

  const range = computeSequenceTimeRange(props.events);
  if (!range) return {};
  timeRange = range;
  const { min: minTs, max: maxTs, pad: padX } = range;

  const data = buildSequencePoints(props.events);

  // 颜色在 buildOption 时解析一次（依赖 DOM 上下文），renderItem 直接用真颜色
  seqColors = {
    '--tt-seq-in':   getCss('--tt-seq-in', '#10b981'),
    '--tt-seq-out':  getCss('--tt-seq-out', '#2563eb'),
    '--tt-seq-fail': getCss('--tt-seq-fail', '#ef4444'),
  };

  const colorTextSecondary = getCss('--tt-text-secondary', '#475569');
  const colorTextMuted     = getCss('--tt-text-muted', '#94a3b8');
  const colorBorderSoft    = getCss('--tt-border-soft', '#e5e7eb');
  const colorSurfaceBase   = getCss('--tt-surface-base', '#ffffff');
  const colorSurfaceSunken = getCss('--tt-surface-sunken', '#f1f5f9');
  const colorAccent        = getCss('--tt-accent', '#2563eb');
  const fontMono           = getCss('--tt-font-mono', 'ui-monospace, monospace');

  return {
    backgroundColor: 'transparent',
    animation: false,
    grid: {
      // top: 给顶部 dataZoom slider 留位置（与 TraceGantt 一致）
      top: 40,
      bottom: 28,
      left: 72,
      right: 24,
    },
    xAxis: {
      type: 'value',
      min: minTs - padX,
      max: maxTs + padX,
      axisLine: { lineStyle: { color: colorBorderSoft } },
      axisTick: { show: false },
      axisLabel: {
        color: colorTextMuted,
        fontSize: 11,
        fontFamily: fontMono,
        formatter: (v) => formatTimeShort(v),
        hideOverlap: true,
      },
      splitLine: { lineStyle: { color: colorBorderSoft, type: 'dashed' } },
    },
    yAxis: {
      type: 'category',
      data: LANES.slice(),
      inverse: true,   // 浏览器在上，远端后端在下
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: {
        color: colorTextSecondary,
        fontSize: 12,
      },
      splitLine: { show: false },
    },
    tooltip: {
      trigger: 'item',
      confine: true,
      backgroundColor: colorSurfaceBase,
      borderColor: colorBorderSoft,
      borderWidth: 1,
      padding: [10, 14],
      extraCssText: `border-radius:10px; box-shadow:0 8px 24px rgba(15,23,42,0.08);`,
      textStyle: {
        color: getCss('--tt-text-primary', '#0f172a'),
        fontSize: 12,
        fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
      },
      formatter: renderTooltip,
    },
    dataZoom: [
      // X 轴顶部 slider —— 始终可见，常驻于 chart 顶部
      {
        id: 'xSlider',
        type: 'slider',
        xAxisIndex: 0,
        height: 6,
        top: 18,
        borderColor: 'transparent',
        backgroundColor: colorSurfaceSunken,
        fillerColor: hexToRgba(colorAccent, 0.12),
        handleStyle: { color: colorAccent, borderColor: colorAccent },
        moveHandleSize: 4,
        showDetail: false,
        labelFormatter: (v) => formatTimeShort(v),
        textStyle: { color: colorTextMuted, fontSize: 10 },
        emphasis: {
          handleStyle: { shadowBlur: 6, shadowColor: hexToRgba(colorAccent, 0.3) },
        },
      },
      // Y 轴右侧 slider —— 与 TraceGantt 保持一致（3 条泳道时恒为全程，仅作视觉一致性保留）
      {
        id: 'ySlider',
        type: 'slider',
        yAxisIndex: 0,
        right: 4,
        top: 40,
        bottom: 28,
        width: 8,
        showDetail: false,
        showDataShadow: false,
        backgroundColor: colorSurfaceSunken,
        fillerColor: hexToRgba(colorAccent, 0.18),
        borderColor: 'transparent',
        handleStyle: { color: colorAccent, borderColor: colorAccent },
        start: 0,
        end: 100,
      },
      // 注：不使用 ECharts 内置的 inside dataZoom，滚轮交互由 host 上的
      //     wheel 监听器 dispatchAction 到上面两个 slider 实现（同 TraceGantt）。
    ],
    series: [
      {
        type: 'custom',
        renderItem,
        encode: { x: [2, 3], y: [0, 1] },
        data,
      },
    ],
  };
}

// ─── renderItem ───
//
// 每个事件画一组图形（group）：
//   1) 请求实线箭头：发起方泳道 → 接收方泳道（垂直，三角箭头指向上行/下行的接收方）
//   2) 处理横条：接收方泳道上 [startMs, endMs] 的圆角矩形（零耗时给最小宽度 8px）
//   3) 返回虚线箭头：endMs 处从接收方泳道回到发起方泳道
function renderItem(_params, api) {
  const fromLane = api.value(0);
  const toLane = api.value(1);
  const startMs = api.value(2);
  const endMs = api.value(3);
  const colorVar = api.value(7);
  const color = seqColors[colorVar] || '#94a3b8';

  const cFrom = api.coord([startMs, fromLane]);
  const cTo = api.coord([startMs, toLane]);
  const cEnd = api.coord([endMs, toLane]);
  const x1 = cFrom[0];
  const y1 = cFrom[1];
  const y2 = cTo[1];
  let x2 = cEnd[0];
  // 处理横条最小宽度：零耗时事件也能看到一个"点"
  if (x2 < x1 + 8) x2 = x1 + 8;

  const barH = 12;
  const halfH = barH / 2;
  // 箭头朝向：Y 轴 inverse 时接收方在下方（dir=1），防御性处理反向情况
  const dir = y2 >= y1 ? 1 : -1;
  const stroke = 'rgba(15, 23, 42, 0.18)';

  // 请求箭头终点停在处理横条的近侧边缘
  const arrowTipY = y2 - dir * halfH;

  return {
    type: 'group',
    children: [
      // 1) 请求实线
      {
        type: 'line',
        shape: { x1, y1, x2: x1, y2: arrowTipY },
        style: { stroke: color, lineWidth: 1.5, cursor: 'pointer' },
      },
      // 1b) 请求箭头三角（指向接收方泳道）
      {
        type: 'polygon',
        shape: {
          points: [
            [x1, arrowTipY],
            [x1 - 4, arrowTipY - dir * 7],
            [x1 + 4, arrowTipY - dir * 7],
          ],
        },
        style: { fill: color, cursor: 'pointer' },
      },
      // 2) 处理横条
      {
        type: 'rect',
        shape: { x: x1, y: y2 - halfH, width: x2 - x1, height: barH, r: 4 },
        style: { fill: color, stroke, lineWidth: 0.8, cursor: 'pointer' },
        emphasis: {
          style: {
            shadowBlur: 8,
            shadowColor: 'rgba(15,23,42,0.25)',
            lineWidth: 1.2,
          },
        },
      },
      // 3) 返回虚线
      {
        type: 'line',
        shape: { x1: x2, y1: y2, x2, y2: y1 },
        style: { stroke: color, lineWidth: 1, lineDash: [4, 4], opacity: 0.75, cursor: 'pointer' },
      },
      // 3b) 返回箭头三角（指向发起方泳道）
      {
        type: 'polygon',
        shape: {
          points: [
            [x2, y1],
            [x2 - 4, y1 + dir * 7],
            [x2 + 4, y1 + dir * 7],
          ],
        },
        style: { fill: color, opacity: 0.75, cursor: 'pointer' },
      },
    ],
  };
}

// ─── tooltip ───
function renderTooltip(p) {
  const log = p.data && p.data.log;
  if (!log) return '';
  const colorVar = p.data.value && p.data.value[7];
  const color = seqColors[colorVar] || '#94a3b8';
  const code = Number(log.statusCode) || 0;
  const failed = code <= 0 || code >= 400;
  const statusBadge = failed
    ? `<span style="color:#ef4444;">● ${code > 0 ? code : '网络错误'}</span>`
    : `<span style="color:#10b981;">● ${code}</span>`;

  const queryLine = log.query
    ? `<div style="margin-top:2px; color:#94a3b8; font-size:11px; max-width:320px; word-break:break-all;">?${escapeHtml(log.query)}</div>`
    : '';
  const taskLine = log.taskId
    ? `<div style="margin-top:4px; color:#475569; font-size:11px;">taskId: <span style="font-family:ui-monospace,monospace;">${escapeHtml(log.taskId)}</span></div>`
    : '';
  const peerLine = log.peer
    ? `<div style="margin-top:2px; color:#94a3b8; font-size:11px;">对端 ${escapeHtml(log.peer)}</div>`
    : '';

  return `
    <div style="display:flex; align-items:center; gap:6px; font-weight:600; font-size:13px; max-width:360px; word-break:break-all;">
      <span style="display:inline-block;width:10px;height:10px;border-radius:2px;background:${color};flex-shrink:0;"></span>
      ${escapeHtml(log.method || '—')} ${escapeHtml(log.path || '')}
    </div>
    ${queryLine}
    <div style="margin-top:4px; font-size:11px;">${statusBadge} · ${escapeHtml(directionLabel(log.direction))}</div>
    <div style="margin-top:4px; color:#475569; font-size:11px;">
      ${formatTimeFull(log.timestampMs)} · 耗时 <strong style="color:#0f172a;">${formatMs(log.durationMs)}</strong>
    </div>
    ${taskLine}
    ${peerLine}
  `;
}

// ─── 顶部时间信息条更新 ───
function updateZoomLabels(opt) {
  if (!chart || !timeRange) return;
  const o = opt || chart.getOption();
  const zooms = (o && o.dataZoom) || [];
  // 取 X 轴 slider 的可视范围
  const xz = zooms.find((z) => z && z.xAxisIndex !== undefined);
  if (!xz) return;
  const startPct = xz.start ?? 0;
  const endPct = xz.end ?? 100;
  const min = timeRange.min - timeRange.pad;
  const max = timeRange.max + timeRange.pad;
  const total = max - min;
  const sMs = min + (total * startPct) / 100;
  const eMs = min + (total * endPct) / 100;
  zoomLabels.start = formatTimeShort(sMs);
  zoomLabels.end = formatTimeShort(eMs);
  zoomLabels.middle = formatMs(eMs - sMs);
}

// ─── ECharts 生命周期 ───
function setupChart() {
  if (!chartRef.value) return;
  if (chart) chart.dispose();
  chart = echarts.init(chartRef.value, null, { renderer: 'canvas' });
  chart.setOption(buildOption(), true);
  updateZoomLabels();

  chart.on('click', (params) => {
    const log = params.data && params.data.log;
    if (log) emit('event-click', log);
  });

  // 跟踪 dataZoom 事件，刷新顶部时间信息条
  chart.on('dataZoom', () => updateZoomLabels());
}

// ─── 自定义滚轮交互（照搬 TraceGantt） ───
//
// 不用 ECharts 内置的 inside dataZoom（多个 inside 同时监听 wheel 会串扰）。
// 在 host 上自己监听 wheel：
//   - 无 Ctrl 修饰 → 上下平移 Y（dispatchAction 改 ySlider 的 start/end）
//   - 按 Ctrl     → 围绕鼠标缩放 X（改 xSlider 的 start/end）
function onWheel(e) {
  if (!chart) return;
  // ctrl/⌘ 表示 X 缩放
  const isZoom = e.ctrlKey || e.metaKey;
  e.preventDefault();
  e.stopPropagation();
  const opt = chart.getOption();
  const zooms = opt.dataZoom || [];
  if (isZoom) {
    const x = zooms.find((z) => z.id === 'xSlider');
    if (!x) return;
    const start = x.start ?? 0;
    const end = x.end ?? 100;
    const range = end - start;
    if (range <= 0) return;
    // 缩放因子：每次滚轮 ~ 15% 范围变化
    const factor = e.deltaY > 0 ? 1.15 : 1 / 1.15;
    let newRange = Math.max(0.5, Math.min(100, range * factor));
    // 围绕鼠标 X 位置作为锚点
    const rect = chartRef.value.getBoundingClientRect();
    const px = e.clientX - rect.left;
    const w = rect.width;
    const anchorPct = Math.max(0, Math.min(100, start + (range * px) / Math.max(1, w)));
    let newStart = anchorPct - (newRange * (px / Math.max(1, w)));
    let newEnd = newStart + newRange;
    if (newStart < 0) { newEnd -= newStart; newStart = 0; }
    if (newEnd > 100) { newStart -= (newEnd - 100); newEnd = 100; }
    newStart = Math.max(0, newStart);
    newEnd = Math.min(100, newEnd);
    chart.dispatchAction({ type: 'dataZoom', dataZoomId: 'xSlider', start: newStart, end: newEnd });
  } else {
    const y = zooms.find((z) => z.id === 'ySlider');
    if (!y) return;
    const start = y.start ?? 0;
    const end = y.end ?? 100;
    const range = end - start;
    if (range >= 100) return; // 全部泳道已可见
    // 平移幅度：每次 wheel ≈ 25% 当前可视范围；与浏览器滚轮节奏接近
    const step = Math.max(2, range * 0.25) * (e.deltaY > 0 ? 1 : -1);
    let newStart = start + step;
    let newEnd = end + step;
    if (newStart < 0) { newEnd -= newStart; newStart = 0; }
    if (newEnd > 100) { newStart -= (newEnd - 100); newEnd = 100; }
    chart.dispatchAction({ type: 'dataZoom', dataZoomId: 'ySlider', start: newStart, end: newEnd });
  }
}

function rebuild() {
  if (!chart) return;
  chart.setOption(buildOption(), true);
  updateZoomLabels();
}

function resize() {
  if (chart) {
    chart.resize();
    updateZoomLabels();
  }
}

defineExpose({ resize });

let resizeObserver = null;

onMounted(async () => {
  await nextTick();
  setupChart();
  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(() => {
      if (!chart) return;
      chart.resize();
      updateZoomLabels();
    });
    resizeObserver.observe(chartRef.value);
  }
  if (hostRef.value) {
    hostRef.value.addEventListener('wheel', onWheel, { passive: false });
  }
});

onUnmounted(() => {
  if (resizeObserver) resizeObserver.disconnect();
  if (hostRef.value) {
    hostRef.value.removeEventListener('wheel', onWheel);
  }
  if (chart) {
    chart.dispose();
    chart = null;
  }
});

watch(() => props.events, rebuild, { deep: false });
</script>

<style scoped>
.trace-seq-host {
  position: relative;
  width: 100%;
  height: 100%;
  min-height: 200px;
  display: flex;
  flex-direction: column;
}

/* 顶部时间信息条：与 X dataZoom slider 在同一视觉带上（样式同 TraceGantt） */
.trace-seq-zoom-info {
  position: absolute;
  top: 0;
  left: 16px;
  right: 16px;
  height: 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 11px;
  pointer-events: none;
  z-index: 2;
}
.trace-seq-zoom-left,
.trace-seq-zoom-right {
  color: var(--tt-text-secondary, #475569);
  font-size: 11px;
  font-family: var(--tt-font-mono, ui-monospace, monospace);
}
.trace-seq-zoom-mid {
  color: var(--tt-text-muted, #94a3b8);
  font-size: 11px;
}

.trace-seq-chart {
  width: 100%;
  flex: 1;
  min-height: 0;
}

.trace-seq-empty {
  position: absolute;
  inset: 0;
}
</style>

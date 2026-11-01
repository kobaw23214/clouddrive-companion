<template>
  <!--
    任务级链路追踪甘特图（Apple HIG / Meta / Netflix 风格）
      - 白底 + 浅色 tooltip + 圆角 + 阴影
      - 失败 span 红色 1.5px 描边，正常 span 同色稍暗 0.8px 细描边（防"一坨"）
      - 进行中 span 呼吸动画
      - 顶部 X 轴 dataZoom 始终可见 + 三段时间信息（起始 / 持续 / 结束）
      - 行数多时：Y 轴 inside dataZoom 提供内部滚动，鼠标滚轮直接平移 Y
      - 父-子时间窗灰带
      - 智能 Y 轴标签 + 左截断 + 悬停显示全名
      - hover 高亮联动 + 点击事件
  -->
  <div ref="hostRef" class="trace-gantt-host">
    <!-- 顶部 X 时间窗信息条：常驻显示 [起始 — 持续 — 结束] -->
    <div v-if="hasData" class="trace-gantt-zoom-info">
      <span class="trace-gantt-zoom-left tt-num">{{ zoomLabels.start }}</span>
      <span class="trace-gantt-zoom-mid tt-meta">{{ zoomLabels.middle }}</span>
      <span class="trace-gantt-zoom-right tt-num">{{ zoomLabels.end }}</span>
    </div>

    <div ref="chartRef" class="trace-gantt-chart"></div>

    <!-- Y 轴标签 hover 浮层 -->
    <div
      v-show="labelTip.visible"
      class="trace-gantt-label-tip"
      :style="{ left: labelTip.x + 'px', top: labelTip.y + 'px' }"
    >{{ labelTip.text }}</div>

    <div v-if="!hasData" class="trace-gantt-empty">
      <span class="tt-meta">没有可视化的 span</span>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, watch, nextTick } from 'vue';
import * as echarts from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { CustomChart } from 'echarts/charts';
import { GridComponent, TooltipComponent, DataZoomComponent } from 'echarts/components';
import { stageLabel, resolveStageColor, formatMs, formatTimeShort } from './stage_meta.js';
import { escapeHtml, hexToRgba, computeTimeRange, buildSeriesPoint } from './gantt_helpers.js';

echarts.use([CanvasRenderer, CustomChart, GridComponent, TooltipComponent, DataZoomComponent]);

const props = defineProps({
  /** 必传：span 数组 */
  spans: { type: Array, default: () => [] },

  /**
   * Y 轴标签策略：
   *   'auto'     —— 智能识别（默认，推荐）
   *   'filename' —— 强制用 filename
   *   'stage'    —— 强制用 stage 中文名
   *   'index'    —— 行号
   */
  labelMode: { type: String, default: 'auto' },

  /** 点击甘特条是否触发 span-click 事件 */
  clickable: { type: Boolean, default: true },

  /** 父 span 时间窗（仅子视图传），用于背景灰带 */
  parentWindow: { type: Object, default: null },

  /** 当前 hover/选中的 spanId，从父组件传入实现高亮联动 */
  hoveredSpanId: { type: String, default: '' },
});

const emit = defineEmits(['span-click', 'hover']);

const hostRef = ref(null);
const chartRef = ref(null);
let chart = null;

// 行高度常量：用于 Y 轴 dataZoom 默认窗口大小（每行约 32px）
const ROW_HEIGHT = 32;

// 缓存数据，便于事件回调中查找
let labelMap = [];        // index → 完整标签
let timeRange = null;     // { min, max, pad }
let lastZoomXRange = null;

const hasData = computed(() => Array.isArray(props.spans) && props.spans.length > 0);

// 顶部时间信息条（左：起始；中：持续；右：结束）
const zoomLabels = reactive({ start: '—', middle: '—', end: '—' });

// Y 轴标签 hover 浮层
const labelTip = reactive({ visible: false, x: 0, y: 0, text: '' });

// ─── 颜色解析 ───
function getStageColor(stage) {
  return resolveStageColor(stage, hostRef.value);
}
function getCss(varName) {
  if (!hostRef.value) return '';
  return getComputedStyle(hostRef.value).getPropertyValue(varName).trim();
}

// ─── 标签宽度估算（CJK ≈ 12px，ASCII ≈ 7px @ 12px font-size） ───
function isCjk(ch) {
  const code = ch.charCodeAt(0);
  return (
    (code >= 0x4e00 && code <= 0x9fff) ||
    (code >= 0x3000 && code <= 0x30ff) ||
    (code >= 0xff00 && code <= 0xffef)
  );
}
function textPxWidth(text) {
  let w = 0;
  for (const ch of text || '') w += isCjk(ch) ? 12 : 7;
  return w;
}
/**
 * 左截断：保留尾部，前面加省略号。
 * "迅雷下载/F06B...torrent" → "…F06B...torrent"
 */
function truncateLeft(text, maxPx) {
  if (!text) return '';
  if (textPxWidth(text) <= maxPx) return text;
  const ellipsisPx = 8;
  let acc = ellipsisPx;
  let out = '';
  for (let i = text.length - 1; i >= 0; i--) {
    const ch = text[i];
    const w = isCjk(ch) ? 12 : 7;
    if (acc + w > maxPx) break;
    acc += w;
    out = ch + out;
  }
  return '…' + out;
}

// ─── 智能 Y 轴标签 ───
//
// 优先级（高 → 低）：
//   1. props.labelMode 显式覆盖（filename / stage / index）
//   2. 当前 span 是 chunk_upload → "分片 #N"（其中 N 来自 extra.chunkNumber）
//      —— 即使在混合视图（md5 + check + chunk）里，分片那行也显示编号，
//         只显示"分片"会导致多个分片行长得一模一样。
//   3. 整组 span 都是 chunk_upload 的纯分片视图 → 同上，"分片 #N"
//   4. 多文件 + 单一 stage → filename
//   5. 单文件 + 多 stage     → stage 中文名
//   6. 兜底"filename / stage"或单独 stage
function buildLabelResolver(spans) {
  const stages = new Set();
  const filenames = new Set();
  for (const s of spans) {
    stages.add(s.stage || '');
    filenames.add(s.filename || '');
  }
  const singleStage = stages.size === 1;
  const singleFile = filenames.size === 1;

  return function resolveLabel(span) {
    switch (props.labelMode) {
      case 'filename': return span.filename || stageLabel(span.stage);
      case 'stage':    return stageLabel(span.stage);
      case 'index':    return '';
      case 'auto':
      default: {
        // 任何 chunk_upload 行都带上分片号
        if (span.stage === 'chunk_upload') {
          const n = readChunkNumber(span);
          return n > 0 ? `分片 #${n}` : stageLabel(span.stage);
        }
        // server_merge 也带上对应的最后分片号，跟 "分片 #N" 视觉上呼应
        if (span.stage === 'server_merge') {
          const n = readChunkNumber(span);
          return n > 0 ? `服务端合并 #${n}` : stageLabel(span.stage);
        }
        if (singleStage && !singleFile) {
          return span.filename || stageLabel(span.stage);
        }
        if (singleFile && !singleStage) {
          return stageLabel(span.stage);
        }
        const f = span.filename || '';
        const st = stageLabel(span.stage);
        if (f && st) return `${f} / ${st}`;
        return f || st || '—';
      }
    }
  };
}

function readChunkNumber(span) {
  if (!span || !span.extra) return 0;
  try {
    const obj = JSON.parse(span.extra);
    if (obj && Number.isFinite(obj.chunkNumber)) return obj.chunkNumber;
  } catch {}
  return 0;
}

// ─── ECharts option ───
function buildOption() {
  if (!hasData.value) return {};

  const resolveLabel = buildLabelResolver(props.spans);
  const labels = props.spans.map(resolveLabel);
  labelMap = labels.slice();
  const labelMaxLen = labels.reduce((m, t) => Math.max(m, (t || '').length), 0);

  const now = Date.now();
  const range = computeTimeRange(props.spans, props.parentWindow, now);
  timeRange = range;
  const { min: minTs, max: maxTs, pad: padX } = range;

  // 父窗口背景灰带
  const markAreaData = [];
  if (props.parentWindow && props.parentWindow.startMs) {
    const pw = props.parentWindow;
    const pwEnd = pw.endMs && pw.endMs > 0 ? pw.endMs : now;
    markAreaData.push([
      { xAxis: pw.startMs, itemStyle: { color: getCss('--tt-surface-sunken') || '#f1f5f9', opacity: 0.6 } },
      { xAxis: pwEnd },
    ]);
  }

  const data = props.spans.map((s, i) => buildSeriesPoint(s, i, labels[i], now));

  const colorTextPrimary   = getCss('--tt-text-primary')   || '#0f172a';
  const colorTextSecondary = getCss('--tt-text-secondary') || '#475569';
  const colorTextMuted     = getCss('--tt-text-muted')     || '#94a3b8';
  const colorBorderSoft    = getCss('--tt-border-soft')    || '#e5e7eb';
  const colorSurfaceBase   = getCss('--tt-surface-base')   || '#ffffff';
  const colorSurfaceSunken = getCss('--tt-surface-sunken') || '#f1f5f9';
  const colorAccent        = getCss('--tt-accent')         || '#2563eb';
  const colorFail          = getCss('--tt-status-fail')    || '#ef4444';
  const fontMono           = getCss('--tt-font-mono')      || 'ui-monospace, monospace';

  const labelWidth = Math.min(280, Math.max(140, labelMaxLen * 8 + 24));
  const labelInner = labelWidth - 24;

  // ─── Y 轴 dataZoom 初始范围 ───
  // 让默认每行 ≈ ROW_HEIGHT(32px)。
  // 估算图表可视高度（实际渲染时 ECharts 用容器高度，这里做一个近似默认值）。
  const totalRows = props.spans.length;
  // 容器 grid 内可用高度 = chart 高度 - top(40+18) - bottom(28) ≈ 容器 - 86
  // 由于我们这里拿不到容器实际高度（option 在 setOption 前），先用 60vh 近似，
  // setupChart 后再根据实际高度修正一次。
  const approxAvail =
    (typeof window !== 'undefined' ? Math.floor(window.innerHeight * 0.6) : 600) - 86;
  const visibleRows = Math.max(1, Math.floor(approxAvail / ROW_HEIGHT));
  const yEndIdx = Math.min(totalRows - 1, visibleRows - 1);
  const yStartPct = 0;
  const yEndPct = totalRows > visibleRows ? (yEndIdx / Math.max(1, totalRows - 1)) * 100 : 100;

  return {
    backgroundColor: 'transparent',
    animation: false,
    grid: {
      // top: 给顶部 dataZoom slider 留位置（slider 高 6 + top 18 = 24）
      top: 40,
      bottom: 28,
      left: labelWidth,
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
      data: labels,
      inverse: true,
      triggerEvent: true,        // 让 axisLabel 触发 mouseover/mouseout 事件
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: {
        color: colorTextSecondary,
        fontSize: 12,
        // 自己做左截断，不用 ECharts 默认右截断
        formatter: (val) => truncateLeft(String(val), labelInner),
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
        color: colorTextPrimary,
        fontSize: 12,
        fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
      },
      formatter: (p) => renderTooltip(p, getStageColor),
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
      // Y 轴右侧 slider —— 给出垂直滚动条视觉
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
        start: yStartPct, end: yEndPct,
      },
      // 注：不使用 ECharts 内置的 inside dataZoom（它们会和我们自定义的 wheel
      //     处理冲突，导致"X 也跟着平移"）。滚轮交互由 host 上的 wheel 监听器
      //     直接 dispatchAction 到上面两个 slider 实现。
    ],
    series: [
      {
        type: 'custom',
        renderItem: (params, api) => renderItem(params, api, getStageColor, colorFail),
        encode: { x: [1, 2], y: 0 },
        data,
        markArea: markAreaData.length
          ? { silent: true, data: markAreaData }
          : undefined,
      },
    ],
  };
}

// ─── renderItem ───
function renderItem(_params, api, getStageColor, colorFail) {
  const yIdx = api.value(0);
  const startMs = api.value(1);
  const endMs = api.value(2);
  const stage = api.value(4);
  const success = api.value(7) !== 0;
  const inProgress = api.value(8) === 1;

  const startCoord = api.coord([startMs, yIdx]);
  const endCoord = api.coord([endMs, yIdx]);
  const startX = startCoord[0];
  const endX = endCoord[0];
  const cy = startCoord[1];

  const fill = getStageColor(stage);
  const w = Math.max(endX - startX, 3);

  // 条形高度跟随 y band 自适应：
  //   gap = 2px（最小间隙），bar = clamp(6, band-2, 22)
  //   下限 6 保证极端缩小时仍可识别；上限 22 防止 band 过大时条形过粗
  const band = (api.size && api.size([0, 1])[1]) || 24;
  const barH = Math.max(6, Math.min(22, Math.floor(band - 2)));
  const halfH = Math.floor(barH / 2);

  const stroke = !success ? colorFail : 'rgba(15, 23, 42, 0.18)';
  const lineWidth = !success ? 1.5 : 0.8;

  const baseStyle = {
    fill,
    stroke,
    lineWidth,
    cursor: 'pointer',
  };

  if (inProgress) {
    return {
      type: 'group',
      children: [
        {
          type: 'rect',
          shape: { x: startX, y: cy - halfH, width: w, height: barH, r: 4 },
          style: { ...baseStyle },
        },
        {
          type: 'rect',
          shape: { x: startX, y: cy - halfH, width: w, height: barH, r: 4 },
          style: { fill: 'rgba(255,255,255,0.35)', stroke: 'transparent' },
          keyframeAnimation: {
            duration: 1400,
            loop: true,
            keyframes: [
              { percent: 0,   easing: 'sinusoidalInOut', style: { opacity: 0.05 } },
              { percent: 0.5, easing: 'sinusoidalInOut', style: { opacity: 0.4 } },
              { percent: 1,   easing: 'sinusoidalInOut', style: { opacity: 0.05 } },
            ],
          },
        },
      ],
    };
  }

  return {
    type: 'rect',
    shape: { x: startX, y: cy - halfH, width: w, height: barH, r: 4 },
    style: baseStyle,
    emphasis: {
      style: {
        shadowBlur: 8,
        shadowColor: 'rgba(15,23,42,0.25)',
        stroke,
        lineWidth: !success ? 2 : 1.2,
      },
    },
  };
}

// ─── tooltip ───
function renderTooltip(p, getStageColor) {
  const span = p.data && p.data.span;
  if (!span) return '';
  const stageColor = getStageColor(span.stage);
  const ok = span.success !== false;
  const okBadge = ok
    ? '<span style="color:#10b981;">● 成功</span>'
    : '<span style="color:#ef4444;">● 失败</span>';

  let extraLine = '';
  if (span.extra) {
    extraLine = `<div style="margin-top:4px; color:#94a3b8; font-size:11px; max-width:280px; word-break:break-all;">${escapeHtml(span.extra)}</div>`;
  }
  let errorLine = '';
  if (span.errorMessage) {
    errorLine = `<div style="margin-top:6px; color:#ef4444; font-size:11px; max-width:280px; word-break:break-all;">${escapeHtml(span.errorMessage)}</div>`;
  }

  const fileLine = span.filename
    ? `<div style="margin-top:4px; color:#475569; font-size:11px;">${escapeHtml(span.filename)}</div>`
    : '';

  return `
    <div style="display:flex; align-items:center; gap:6px; font-weight:600; font-size:13px;">
      <span style="display:inline-block;width:10px;height:10px;border-radius:2px;background:${stageColor};"></span>
      ${escapeHtml(stageLabel(span.stage))}
    </div>
    ${fileLine}
    <div style="margin-top:4px; color:#475569; font-size:11px;">
      ${formatTimeShort(span.startMs)} → ${formatTimeShort(span.endMs || Date.now())} ·
      <strong style="color:#0f172a;">${formatMs(span.durationMs)}</strong>
    </div>
    <div style="margin-top:4px; font-size:11px;">${okBadge}</div>
    ${extraLine}
    ${errorLine}
  `;
}

// ─── 顶部时间信息条更新 ───
function updateZoomLabels(opt) {
  if (!chart || !timeRange) return;
  const o = opt || chart.getOption();
  const zooms = (o && o.dataZoom) || [];
  // 取 X 轴 slider（第 0 项）的可视范围
  let xz = zooms.find((z) => z && z.xAxisIndex !== undefined);
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
  lastZoomXRange = { start: sMs, end: eMs };
}

// ─── ECharts 生命周期 ───
function setupChart() {
  if (!chartRef.value) return;
  if (chart) chart.dispose();
  chart = echarts.init(chartRef.value, null, { renderer: 'canvas' });
  const opt = buildOption();
  chart.setOption(opt, true);
  // 初次设置完成后修正 Y dataZoom 默认窗口（用真实容器高度）
  fixYZoomToContainerHeight();
  updateZoomLabels();

  chart.on('click', (params) => {
    if (!props.clickable) return;
    const span = params.data && params.data.span;
    if (span) emit('span-click', span);
  });

  chart.on('mouseover', (params) => {
    // 系列条
    if (params.componentType === 'series') {
      const span = params.data && params.data.span;
      if (span) emit('hover', span);
      return;
    }
    // Y 轴 axisLabel：显示完整名称浮层
    if (params.componentType === 'yAxis' && params.targetType === 'axisLabel') {
      const fullText = String(params.value || '');
      // 如果未被截断，无需弹浮层
      const labelInner = computeLabelInnerPx();
      if (textPxWidth(fullText) <= labelInner) return;
      showLabelTip(fullText, params.event);
    }
  });
  chart.on('mouseout', (params) => {
    if (!params || params.componentType === 'series') {
      emit('hover', null);
      return;
    }
    if (params.componentType === 'yAxis') {
      hideLabelTip();
    }
  });

  // 跟踪 dataZoom 事件，刷新顶部时间信息条
  chart.on('dataZoom', () => updateZoomLabels());
}

function computeLabelInnerPx() {
  const labelMaxLen = labelMap.reduce((m, t) => Math.max(m, (t || '').length), 0);
  const labelWidth = Math.min(280, Math.max(140, labelMaxLen * 8 + 24));
  return labelWidth - 24;
}

function showLabelTip(text, ev) {
  if (!hostRef.value) return;
  const hostRect = hostRef.value.getBoundingClientRect();
  const px = (ev && ev.event && ev.event.clientX) || 0;
  const py = (ev && ev.event && ev.event.clientY) || 0;
  labelTip.text = text;
  labelTip.x = Math.max(8, px - hostRect.left + 12);
  labelTip.y = Math.max(8, py - hostRect.top + 12);
  labelTip.visible = true;
}
function hideLabelTip() { labelTip.visible = false; }

// 用真实容器高度修正 Y 轴 dataZoom 的初始可视行数
function fixYZoomToContainerHeight() {
  if (!chart || !chartRef.value) return;
  const h = chartRef.value.clientHeight;
  if (!h) return;
  // grid 上下 padding ≈ 86 (top 40 + bottom 28 + 一些空隙)
  const avail = Math.max(120, h - 86);
  const visibleRows = Math.max(1, Math.floor(avail / ROW_HEIGHT));
  const total = props.spans.length;
  if (total <= visibleRows) return;
  const endIdx = Math.min(total - 1, visibleRows - 1);
  const endPct = (endIdx / Math.max(1, total - 1)) * 100;
  chart.setOption({
    dataZoom: [
      { id: 'ySlider', start: 0, end: endPct },
    ],
  });
}

// ─── 自定义滚轮交互 ───
//
// 不用 ECharts 内置的 inside dataZoom（多个 inside 同时监听 wheel 会导致
// "X 也跟着平移"等串扰）。改成：在 host 上自己监听 wheel：
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
    // 缩放因子：每次滚轮 ~ 12% 范围变化
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
    if (range >= 100) return; // 全部行已可见
    // 平移幅度：每次 wheel ≈ 15% 当前可视范围；与浏览器滚轮节奏接近
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
  fixYZoomToContainerHeight();
  updateZoomLabels();
}

function applyHover(spanId) {
  if (!chart) return;
  chart.dispatchAction({ type: 'downplay', seriesIndex: 0 });
  if (!spanId) return;
  const idx = props.spans.findIndex((s) => s.spanId === spanId);
  if (idx < 0) return;
  chart.dispatchAction({ type: 'highlight', seriesIndex: 0, dataIndex: idx });
}

function resize() {
  if (chart) {
    chart.resize();
    fixYZoomToContainerHeight();
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
      fixYZoomToContainerHeight();
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

watch(() => props.spans, rebuild, { deep: false });
watch(() => props.parentWindow, rebuild, { deep: true });
watch(() => props.labelMode, rebuild);
watch(() => props.hoveredSpanId, (v) => applyHover(v));
</script>

<style scoped>
.trace-gantt-host {
  position: relative;
  width: 100%;
  height: 100%;
  min-height: 200px;
  display: flex;
  flex-direction: column;
}

/* 顶部时间信息条：常驻显示 [起始 — 持续 — 结束]，与 X dataZoom slider 在同一视觉带上 */
.trace-gantt-zoom-info {
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
.trace-gantt-zoom-left,
.trace-gantt-zoom-right {
  color: var(--tt-text-secondary, #475569);
  font-size: 11px;
  font-family: var(--tt-font-mono, ui-monospace, monospace);
}
.trace-gantt-zoom-mid {
  color: var(--tt-text-muted, #94a3b8);
  font-size: 11px;
}

.trace-gantt-chart {
  width: 100%;
  flex: 1;
  min-height: 0;
}

.trace-gantt-empty {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}

/* Y 轴标签全名浮层 */
.trace-gantt-label-tip {
  position: absolute;
  background: var(--tt-surface-base, #fff);
  color: var(--tt-text-primary, #0f172a);
  border: 1px solid var(--tt-border-soft, #e5e7eb);
  border-radius: 6px;
  padding: 4px 8px;
  font-size: 12px;
  max-width: 480px;
  word-break: break-all;
  pointer-events: none;
  box-shadow: 0 6px 18px rgba(15, 23, 42, 0.08);
  z-index: 10;
}
</style>

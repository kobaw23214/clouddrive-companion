<template>
  <div class="trace-children" :class="{ 'is-fullscreen': fullscreen }">
    <!-- 加载 / 错误 / 空 -->
    <div v-if="loading" class="tt-skeleton trace-children-skeleton-chart"></div>
    <div v-else-if="error" class="tt-empty">
      <span class="tt-badge tt-badge-fail">加载失败</span>
      <span class="tt-meta">{{ error }}</span>
      <button class="tt-button" @click="load">重试</button>
    </div>
    <div v-else-if="!children.length" class="tt-empty">
      <span>该 span 没有子 span（已是最细粒度）</span>
    </div>

    <!-- 数据 -->
    <template v-else>
      <!-- 甘特图卡片（带全屏按钮 + 返回按钮） -->
      <section class="tt-card trace-children-chart-card">
        <header class="trace-children-chart-header">
          <span class="tt-title-sm">子阶段（{{ children.length }}）</span>
          <span class="tt-meta trace-children-chart-tip">滚轮上下平移 · Ctrl+滚轮缩放时间轴</span>
          <button
            class="tt-button trace-children-fullscreen-btn"
            @click="toggleFullscreen"
            :title="fullscreen ? '退出全屏' : '全屏查看'"
          >
            <svg v-if="!fullscreen" width="14" height="14" viewBox="0 0 24 24" fill="none"
                 stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <polyline points="15 3 21 3 21 9"></polyline>
              <polyline points="9 21 3 21 3 15"></polyline>
              <line x1="21" y1="3" x2="14" y2="10"></line>
              <line x1="3" y1="21" x2="10" y2="14"></line>
            </svg>
            <svg v-else width="14" height="14" viewBox="0 0 24 24" fill="none"
                 stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <polyline points="4 14 10 14 10 20"></polyline>
              <polyline points="20 10 14 10 14 4"></polyline>
              <line x1="14" y1="10" x2="21" y2="3"></line>
              <line x1="3" y1="21" x2="10" y2="14"></line>
            </svg>
            {{ fullscreen ? '退出' : '全屏' }}
          </button>
          <button class="tt-button" @click="$emit('back')">‹ 返回</button>
        </header>
        <div class="trace-children-chart-body" :style="{ height: chartHeight + 'px' }">
          <TraceGantt
            ref="ganttRef"
            :spans="children"
            label-mode="auto"
            :clickable="true"
            :parent-window="parentWindow"
            :hovered-span-id="hoveredSpanId"
            @hover="onChartHover"
            @span-click="onChartClick"
          />
        </div>
        <!-- 上下分隔条：拖拽改变图表高度 -->
        <div
          v-if="!fullscreen"
          class="trace-children-resizer"
          :class="{ 'is-dragging': resizing }"
          @pointerdown="onResizeStart"
          title="拖动调整图表与列表的高度比例"
        ><span class="trace-children-resizer-handle"></span></div>
      </section>

      <!-- 完整字段表格 -->
      <section class="tt-card trace-children-list-card">
        <header class="trace-children-list-header trace-children-row">
          <span class="trace-children-cell-status"></span>
          <span class="trace-children-cell-stage tt-meta">阶段</span>
          <span class="trace-children-cell-id tt-meta">Span ID</span>
          <span class="trace-children-cell-info tt-meta">详情 / 错误</span>
          <span class="trace-children-cell-time tt-meta">开始</span>
          <span class="trace-children-cell-end tt-meta">结束</span>
          <span class="trace-children-cell-duration tt-meta">耗时</span>
          <span class="trace-children-cell-action tt-meta">操作</span>
        </header>
        <div
          v-for="s in children"
          :key="s.spanId"
          class="trace-children-row tt-row"
          :class="{ 'is-hovered': hoveredSpanId === s.spanId }"
          @mouseenter="hoveredSpanId = s.spanId"
          @mouseleave="hoveredSpanId = ''"
          @click="onRowClick(s)"
        >
          <span class="tt-dot trace-children-cell-status" :class="s.success ? 'tt-dot-ok' : 'tt-dot-fail'"></span>
          <span class="trace-children-cell-stage">
            <span class="trace-children-stage-swatch" :style="{ background: stageColor(s.stage) }"></span>
            <span>{{ stageLabel(s.stage) }}</span>
          </span>
          <span class="trace-children-cell-id tt-num tt-truncate" :title="s.spanId">{{ shortId(s.spanId) }}</span>
          <span class="trace-children-cell-info tt-meta tt-truncate" :title="rowInfoTooltip(s)">
            <template v-if="s.errorMessage">
              <span class="trace-children-info-error">{{ s.errorMessage }}</span>
            </template>
            <template v-else-if="s.filename">
              {{ s.filename }}<span v-if="s.extra" class="trace-children-info-extra"> · {{ s.extra }}</span>
            </template>
            <template v-else-if="s.extra">
              {{ s.extra }}
            </template>
            <template v-else>—</template>
          </span>
          <span class="trace-children-cell-time tt-meta tt-num">{{ formatTimeFull(s.startMs) }}</span>
          <span class="trace-children-cell-end tt-meta tt-num">{{ s.endMs ? formatTimeFull(s.endMs) : '进行中' }}</span>
          <span class="trace-children-cell-duration tt-num">{{ formatMs(s.durationMs) }}</span>
          <span class="trace-children-cell-action">
            <button
              class="trace-children-drilldown-btn"
              :title="hasChildrenMap[s.spanId] ? '查看子阶段' : '已是最细粒度'"
              :disabled="!hasChildrenMap[s.spanId]"
              @click.stop="onDrilldown(s)"
            >
              {{ hasChildrenMap[s.spanId] ? '深入 ›' : '—' }}
            </button>
          </span>
        </div>
      </section>
    </template>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue';
import TraceGantt from './TraceGantt.vue';
import { getSpanChildren, subscribeTraceStream } from './api.js';
import {
  stageLabel, stageColor, formatMs, formatTimeShort, formatTimeFull,
} from './stage_meta.js';

const props = defineProps({
  spanId: { type: String, required: true },
  filename: { type: String, default: '' },
});
const emit = defineEmits(['loading', 'back', 'enter-span']);

const children = ref([]);
const parent = ref(null);
const loading = ref(false);
const error = ref('');
const hoveredSpanId = ref('');
const fullscreen = ref(false);
const ganttRef = ref(null);

// 哪些子 span 还有自己的子？预查一遍，决定"深入"按钮可用性。
// 性能考虑：N+1 查询，但子 span 通常 < 50 个，可接受。
const hasChildrenMap = ref({});

const parentWindow = computed(() => {
  if (!parent.value || !parent.value.startMs) return null;
  return { startMs: parent.value.startMs, endMs: parent.value.endMs || 0 };
});

// 图表高度：用户可拖动 resizer 调整。
const userChartHeight = ref(0); // 0 表示用默认值
const chartHeight = computed(() => {
  if (fullscreen.value) {
    if (typeof window === 'undefined') return 800;
    return Math.max(400, window.innerHeight - 200);
  }
  if (userChartHeight.value > 0) return userChartHeight.value;
  if (typeof window === 'undefined') return 480;
  return Math.max(320, Math.floor(window.innerHeight * 0.6));
});

// 拖动 resizer 调整图表高度
const resizing = ref(false);
let dragStartY = 0;
let dragStartH = 0;
function onResizeStart(e) {
  if (fullscreen.value) return;
  resizing.value = true;
  dragStartY = e.clientY;
  dragStartH = chartHeight.value;
  if (e.target && e.target.setPointerCapture && e.pointerId !== undefined) {
    try { e.target.setPointerCapture(e.pointerId); } catch {}
  }
  window.addEventListener('pointermove', onResizeMove);
  window.addEventListener('pointerup', onResizeEnd);
}
function onResizeMove(e) {
  if (!resizing.value) return;
  const dy = e.clientY - dragStartY;
  const next = dragStartH + dy;
  const max = (typeof window !== 'undefined' ? window.innerHeight : 800) - 160;
  userChartHeight.value = Math.max(200, Math.min(max, next));
  if (ganttRef.value && ganttRef.value.resize) ganttRef.value.resize();
}
function onResizeEnd() {
  resizing.value = false;
  window.removeEventListener('pointermove', onResizeMove);
  window.removeEventListener('pointerup', onResizeEnd);
}

async function load() {
  if (!props.spanId) return;
  loading.value = true;
  error.value = '';
  emit('loading', true);
  try {
    children.value = await getSpanChildren(props.spanId);
    parent.value = computeParentWindow(children.value);
    // 用 stage 启发式判断哪些子 span 还可能有子，避免 N+1 请求
    hasChildrenMap.value = inferDrilldownMap(children.value);
    // 重建实时订阅（基于已加载的 traceId）
    startStream();
  } catch (e) {
    error.value = (e && e.message) || String(e);
    children.value = [];
    parent.value = null;
    hasChildrenMap.value = {};
  } finally {
    loading.value = false;
    emit('loading', false);
  }
}

// ─── 实时更新（SSE） ───
//
// 仅关心 parentSpanId === props.spanId 的事件。
// SSE 是按 traceId 订阅的，traceId 从已加载的 children[0] 拿。children 为空时
// 也开订阅会失败（trace 还不存在），所以在 load() 后重建。
let unsubscribeStream = null;
function startStream() {
  if (unsubscribeStream) {
    unsubscribeStream();
    unsubscribeStream = null;
  }
  // 没有任何 child 还能订阅吗？理论上父 span 的 traceId 我们知道（来自上层导航），
  // 但当前 props 里没传 traceId。退一步：children 为空就先不订阅，等用户点刷新
  // 再加载到 children 后自动订阅。这是合理折中。
  if (!children.value.length) return;
  const traceId = children.value[0].traceId;
  if (!traceId) return;

  unsubscribeStream = subscribeTraceStream(traceId, {
    onInsert: (sp) => {
      if (!sp || sp.parentSpanId !== props.spanId) return;
      const idx = children.value.findIndex((x) => x.spanId === sp.spanId);
      if (idx >= 0) {
        const next = children.value.slice();
        next[idx] = sp;
        children.value = next;
      } else {
        children.value = [...children.value, sp];
      }
      hasChildrenMap.value = inferDrilldownMap(children.value);
    },
    onUpdate: (sp) => {
      if (!sp || !sp.spanId) return;
      const idx = children.value.findIndex((x) => x.spanId === sp.spanId);
      if (idx >= 0) {
        const next = children.value.slice();
        next[idx] = sp;
        children.value = next;
      }
    },
  });
}

// inferDrilldownMap 根据 stage 推断哪些子还能继续下钻：
//   - upload_task / upload_file 这类容器型 stage 一定有子
//   - md5_calc / check_chunks / chunk_upload / notify_complete / server_merge 是叶子
//   - file_scan 现在是分层的（每个目录一条 span），保守视作可深入；底层
//     不含子目录的"叶子目录"虽然进去会是空，前端显示空态即可，不致误导
//   - 其它（other / 未知）保守假设有子
function inferDrilldownMap(kids) {
  const LEAF_STAGES = new Set([
    'md5_calc',
    'check_chunks',
    'chunk_upload',
    'notify_complete',
    'server_merge',
  ]);
  const out = {};
  for (const k of kids) {
    out[k.spanId] = !LEAF_STAGES.has(k.stage);
  }
  return out;
}

// 用孩子的 min/max 时间作为父窗口的近似（避免新增 GetSpan 后端接口）
function computeParentWindow(kids) {
  if (!kids || !kids.length) return null;
  let min = Infinity, max = 0;
  let stage = '';
  for (const k of kids) {
    if (Number.isFinite(k.startMs)) min = Math.min(min, k.startMs);
    if (Number.isFinite(k.endMs) && k.endMs > 0) max = Math.max(max, k.endMs);
    if (!stage) stage = k.stage;
  }
  if (!Number.isFinite(min)) return null;
  return {
    spanId: props.spanId,
    stage,
    startMs: min,
    endMs: max,
    durationMs: max > 0 ? max - min : 0,
  };
}

function onChartHover(span) {
  hoveredSpanId.value = (span && span.spanId) || '';
}

// 点击甘特条：如果有子就下钻，否则只高亮（视觉反馈）
function onChartClick(s) {
  if (!s) return;
  if (hasChildrenMap.value[s.spanId]) {
    emit('enter-span', { spanId: s.spanId, filename: s.filename || stageLabel(s.stage) });
  } else {
    hoveredSpanId.value = s.spanId;
  }
}

// 点击行：只是高亮，不打开抽屉
function onRowClick(s) {
  hoveredSpanId.value = s.spanId;
}

// 单独的"深入 ›"按钮才是显式下钻
function onDrilldown(s) {
  if (!hasChildrenMap.value[s.spanId]) return;
  emit('enter-span', { spanId: s.spanId, filename: s.filename || stageLabel(s.stage) });
}

function rowInfoTooltip(s) {
  const parts = [];
  if (s.errorMessage) parts.push(s.errorMessage);
  if (s.filename) parts.push(s.filename);
  if (s.extra) parts.push(s.extra);
  return parts.join('\n');
}

function shortId(id) {
  if (!id) return '—';
  return id.length > 12 ? id.slice(0, 8) + '…' + id.slice(-4) : id;
}

// 全屏切换
async function toggleFullscreen() {
  fullscreen.value = !fullscreen.value;
  await nextTick();
  if (ganttRef.value && ganttRef.value.resize) ganttRef.value.resize();
}

// ESC 退出全屏（优先于 modal 的 ESC，因为 stop propagation）
function onKeydown(e) {
  if (fullscreen.value && e.key === 'Escape') {
    e.preventDefault();
    e.stopPropagation();
    fullscreen.value = false;
  }
}

// 窗口尺寸变化重算图高度
function onResize() {
  if (ganttRef.value && ganttRef.value.resize) ganttRef.value.resize();
}

onMounted(() => {
  load();
  window.addEventListener('keydown', onKeydown, true);
  window.addEventListener('resize', onResize);
});
onUnmounted(() => {
  if (unsubscribeStream) { unsubscribeStream(); unsubscribeStream = null; }
  window.removeEventListener('keydown', onKeydown, true);
  window.removeEventListener('resize', onResize);
});
watch(() => props.spanId, load);
</script>

<style scoped>
.trace-children {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 16px 20px 24px;
}

/* 全屏模式：把甘特图卡片提升为顶层覆盖整个 modal-body */
.trace-children.is-fullscreen .trace-children-chart-card {
  position: fixed;
  inset: 56px 0 0 0; /* 56 = modal-header 高度 */
  z-index: 1050;
  border-radius: 0;
  border-left: none;
  border-right: none;
  background: var(--tt-surface-base);
  display: flex;
  flex-direction: column;
}
.trace-children.is-fullscreen .trace-children-chart-body {
  flex: 1;
  min-height: 0;
}
.trace-children.is-fullscreen .trace-children-list-card {
  /* 全屏时其它内容仍可见但被遮挡 */
}

.trace-children-skeleton-chart {
  height: 240px;
  border-radius: var(--tt-radius-card);
}

/* 甘特图卡片 */
.trace-children-chart-card {
  display: flex;
  flex-direction: column;
}
.trace-children-chart-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--tt-border-soft);
}
.trace-children-chart-header .tt-title-sm { flex-shrink: 0; }
.trace-children-chart-tip { flex: 1; }

.trace-children-fullscreen-btn {
  flex-shrink: 0;
}

.trace-children-chart-body {
  padding: 8px 12px;
  /* height 由父组件计算 */
}

/* 图表与列表之间的拖动分隔条 */
.trace-children-resizer {
  height: 8px;
  cursor: row-resize;
  background: var(--tt-surface-base);
  border-top: 1px solid var(--tt-border-soft);
  display: flex;
  align-items: center;
  justify-content: center;
  user-select: none;
  touch-action: none;
}
.trace-children-resizer:hover,
.trace-children-resizer.is-dragging {
  background: var(--tt-accent-soft);
}
.trace-children-resizer-handle {
  width: 36px;
  height: 3px;
  border-radius: 2px;
  background: var(--tt-border-strong);
  transition: background 80ms ease;
}
.trace-children-resizer:hover .trace-children-resizer-handle,
.trace-children-resizer.is-dragging .trace-children-resizer-handle {
  background: var(--tt-accent);
}

/* 完整字段表格 */
.trace-children-list-card {
  display: flex;
  flex-direction: column;
}

.trace-children-row {
  display: grid;
  grid-template-columns:
    14px                /* status dot */
    140px               /* stage */
    110px               /* span id */
    minmax(180px, 1fr)  /* info */
    140px               /* start */
    140px               /* end */
    80px                /* duration */
    72px;               /* drilldown */
  gap: 12px;
  align-items: center;
  padding: 8px 16px;
  height: var(--tt-row-height);
}
.trace-children-list-header {
  cursor: default;
  background: var(--tt-surface-soft);
  border-bottom: 1px solid var(--tt-border-soft);
  height: 36px;
}
.trace-children-list-header:hover { background: var(--tt-surface-soft); }
.trace-children-row.is-hovered { background: var(--tt-accent-soft); }

.trace-children-cell-stage {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--tt-text-primary);
}
.trace-children-stage-swatch {
  width: 10px;
  height: 10px;
  border-radius: 2px;
  flex-shrink: 0;
}

.trace-children-cell-id {
  color: var(--tt-text-secondary);
  font-size: 12px;
}

.trace-children-cell-info {
  min-width: 0;
}
.trace-children-info-error { color: var(--tt-status-fail); }
.trace-children-info-extra { color: var(--tt-text-muted); }

.trace-children-cell-time,
.trace-children-cell-end {
  white-space: nowrap;
  font-size: 12px;
}
.trace-children-cell-duration { text-align: right; }

.trace-children-cell-action {
  text-align: right;
}

.trace-children-drilldown-btn {
  border: none;
  background: transparent;
  color: var(--tt-accent);
  font-size: 12px;
  cursor: pointer;
  padding: 4px 6px;
  border-radius: 4px;
  transition: background 80ms ease, color 80ms ease;
}
.trace-children-drilldown-btn:hover:not(:disabled) {
  background: var(--tt-accent-soft);
}
.trace-children-drilldown-btn:disabled {
  color: var(--tt-text-muted);
  cursor: default;
}
</style>

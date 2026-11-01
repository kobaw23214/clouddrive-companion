<template>
  <div class="trace-tops" :class="{ 'is-fullscreen': fullscreen }">
    <div v-if="loading" class="tt-skeleton trace-tops-skeleton-chart"></div>
    <div v-else-if="error" class="tt-empty">
      <span class="tt-badge tt-badge-fail">加载失败</span>
      <span class="tt-meta">{{ error }}</span>
      <button class="tt-button" @click="load">重试</button>
    </div>
    <div v-else-if="!spans.length" class="tt-empty">
      <span>该 trace 暂无顶层 span</span>
    </div>

    <template v-else>
      <!-- 视图切换 Tab：甘特图（默认）/ 时序图（该任务的 HTTP 交互记录） -->
      <div class="trace-tops-tabs">
        <button
          class="trace-tops-tab"
          :class="{ 'is-active': activeTab === 'gantt' }"
          @click="switchTab('gantt')"
        >甘特图</button>
        <button
          class="trace-tops-tab"
          :class="{ 'is-active': activeTab === 'sequence' }"
          @click="switchTab('sequence')"
        >时序图</button>
      </div>

      <template v-if="activeTab === 'gantt'">
      <section class="tt-card trace-tops-chart-card">
        <header class="trace-tops-chart-header">
          <span class="tt-title-sm">顶层 span（{{ spans.length }}）</span>
          <span class="tt-meta trace-tops-chart-tip">滚轮上下平移 · Ctrl+滚轮缩放时间轴</span>
          <span v-if="summary && summary.hasFailure" class="tt-badge tt-badge-fail">含失败</span>
          <button
            class="tt-button trace-tops-fullscreen-btn"
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
          <button class="tt-button" @click="$emit('back')">‹ 任务列表</button>
          <button
            class="tt-button tt-button-danger"
            v-if="canDelete"
            :disabled="deleting"
            @click="onDelete"
          >{{ deleting ? '删除中…' : '删除该 trace' }}</button>
        </header>
        <div class="trace-tops-chart-body" :style="{ height: chartHeight + 'px' }">
          <TraceGantt
            ref="ganttRef"
            :spans="spans"
            label-mode="auto"
            :clickable="true"
            :hovered-span-id="hoveredSpanId"
            @hover="onChartHover"
            @span-click="enterSpan"
          />
        </div>
        <!-- 上下分隔条：拖拽改变图表高度 -->
        <div
          v-if="!fullscreen"
          class="trace-tops-resizer"
          :class="{ 'is-dragging': resizing }"
          @pointerdown="onResizeStart"
          title="拖动调整图表与列表的高度比例"
        ><span class="trace-tops-resizer-handle"></span></div>
      </section>

      <!-- 完整字段表格 -->
      <section class="tt-card trace-tops-list-card">
        <header class="trace-tops-list-header trace-tops-row">
          <span class="trace-tops-cell-status"></span>
          <span class="trace-tops-cell-stage tt-meta">阶段</span>
          <span class="trace-tops-cell-id tt-meta">Span ID</span>
          <span class="trace-tops-cell-name tt-meta">文件 / 详情</span>
          <span class="trace-tops-cell-time tt-meta">开始</span>
          <span class="trace-tops-cell-end tt-meta">结束</span>
          <span class="trace-tops-cell-duration tt-meta">耗时</span>
          <span class="trace-tops-cell-action tt-meta">操作</span>
        </header>
        <div
          v-for="s in spans"
          :key="s.spanId"
          class="trace-tops-row tt-row"
          :class="{ 'is-hovered': hoveredSpanId === s.spanId }"
          @mouseenter="hoveredSpanId = s.spanId"
          @mouseleave="hoveredSpanId = ''"
          @click="onRowClick(s)"
        >
          <span class="tt-dot trace-tops-cell-status" :class="s.success ? 'tt-dot-ok' : 'tt-dot-fail'"></span>
          <span class="trace-tops-cell-stage">
            <span class="trace-tops-stage-swatch" :style="{ background: stageColor(s.stage) }"></span>
            <span>{{ stageLabel(s.stage) }}</span>
          </span>
          <span class="trace-tops-cell-id tt-num tt-truncate" :title="s.spanId">{{ shortId(s.spanId) }}</span>
          <span class="trace-tops-cell-name tt-truncate" :title="rowInfoTooltip(s)">
            <template v-if="s.errorMessage">
              <span class="trace-tops-info-error">{{ s.errorMessage }}</span>
            </template>
            <template v-else-if="s.filename">
              {{ s.filename }}<span v-if="s.extra" class="trace-tops-info-extra"> · {{ s.extra }}</span>
            </template>
            <template v-else-if="s.extra">
              {{ s.extra }}
            </template>
            <template v-else>—</template>
          </span>
          <span class="trace-tops-cell-time tt-meta tt-num">{{ formatTimeFull(s.startMs) }}</span>
          <span class="trace-tops-cell-end tt-meta tt-num">{{ s.endMs ? formatTimeFull(s.endMs) : '进行中' }}</span>
          <span class="trace-tops-cell-duration tt-num">{{ formatMs(s.durationMs) }}</span>
          <span class="trace-tops-cell-action">
            <button class="trace-tops-drilldown-btn" @click.stop="enterSpan(s)" title="查看子阶段">
              深入 ›
            </button>
          </span>
        </div>
      </section>
      </template>

      <!-- 时序图：该任务（taskId）的 HTTP 交互记录 -->
      <section v-else class="tt-card trace-tops-seq-card">
        <div v-if="seqLoading" class="tt-skeleton trace-tops-seq-skeleton"></div>
        <div v-else-if="seqError" class="tt-empty">
          <span class="tt-badge tt-badge-fail">加载失败</span>
          <span class="tt-meta">{{ seqError }}</span>
          <button class="tt-button" @click="loadSequence">重试</button>
        </div>
        <div v-else-if="!seqEvents.length" class="tt-empty">
          <span>该任务暂无 HTTP 交互记录</span>
        </div>
        <div v-else class="trace-tops-seq-body" :style="{ height: chartHeight + 'px' }">
          <TraceSequence :events="seqEvents" />
        </div>
      </section>
    </template>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue';
import TraceGantt from './TraceGantt.vue';
import TraceSequence from './TraceSequence.vue';
import { getTraceTopSpans, getTraceAllSpans, listTracedTasks, deleteTrace, subscribeTraceStream } from './api.js';
import { GetSequenceEvents } from '../../../wailsjs/go/app/App';
import {
  stageLabel, stageColor, formatMs, formatTimeShort, formatTimeFull,
} from './stage_meta.js';

const props = defineProps({
  traceId: { type: String, required: true },
  taskName: { type: String, default: '' },
});
const emit = defineEmits(['loading', 'back', 'enter-span']);

const spans = ref([]);
const summary = ref(null);
const loading = ref(false);
const error = ref('');
const hoveredSpanId = ref('');
const deleting = ref(false);
const fullscreen = ref(false);
const ganttRef = ref(null);

const canDelete = computed(() => !!props.traceId && !loading.value);

// ─── 甘特图 / 时序图 Tab ───
//
// 时序图展示该任务（taskId）的 HTTP 交互记录，数据来自 HTTP 日志库
// （GetSequenceEvents），与 span 数据相互独立，切到 Tab 时才懒加载一次。
const activeTab = ref('gantt');
const seqEvents = ref([]);
const seqLoading = ref(false);
const seqError = ref('');
const seqLoaded = ref(false); // 是否已拉取过（避免来回切 Tab 重复请求）

function switchTab(tab) {
  if (activeTab.value === tab) return;
  activeTab.value = tab;
  if (tab === 'sequence') {
    // 时序图不支持全屏态，先退出，避免甘特图卡片的 fixed 定位盖住 Tab
    fullscreen.value = false;
    if (!seqLoaded.value) loadSequence();
  } else {
    nextTick(() => {
      if (ganttRef.value && ganttRef.value.resize) ganttRef.value.resize();
    });
  }
}

// 时序图数据：先用 GetTraceAllSpans 取 taskId（任一 span 上都带），
// 再按 taskId 拉 HTTP 交互记录；fromMs/toMs 传 0 表示不限时间（后端语义），
// 由 taskId 精确限定范围。taskId 为空（如独立的 file_scan trace）时显示空态。
async function loadSequence() {
  if (!props.traceId || seqLoading.value) return;
  seqLoading.value = true;
  seqError.value = '';
  try {
    const all = await getTraceAllSpans(props.traceId);
    const allSpans = (all && all.spans) || [];
    const taskId = (allSpans[0] && allSpans[0].taskId) || '';
    if (!taskId) {
      seqEvents.value = [];
    } else {
      const resp = await GetSequenceEvents(0, 0, taskId, '', 1, 500);
      seqEvents.value = (resp && resp.entries) || [];
    }
    seqLoaded.value = true;
  } catch (e) {
    seqError.value = (e && e.message) || String(e);
    seqEvents.value = [];
  } finally {
    seqLoading.value = false;
  }
}

// 图表高度：用户可拖动 resizer 调整。
//   - 普通模式：默认 60vh（最小 320），用户拖拽 → 持久到本组件实例的 ref
//   - 全屏：占满（父容器是 flex/fixed，由 CSS 控制）
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
  // 限制范围：最小 200，最大 视口 - 160（给列表 + footer 留位）
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
  if (!props.traceId) return;
  loading.value = true;
  error.value = '';
  emit('loading', true);
  try {
    const [list, sumResp] = await Promise.all([
      getTraceTopSpans(props.traceId),
      listTracedTasks({ page: 1, size: 1 }).catch(() => ({ items: [] })),
    ]);
    spans.value = list || [];
    if (sumResp && Array.isArray(sumResp.items)) {
      summary.value = sumResp.items.find((it) => it.traceId === props.traceId) || null;
    }
    if (!summary.value) {
      summary.value = computeFallbackSummary(spans.value);
    }
  } catch (e) {
    error.value = (e && e.message) || String(e);
    spans.value = [];
    summary.value = null;
  } finally {
    loading.value = false;
    emit('loading', false);
  }
}

// ─── 实时更新（SSE） ───
//
// 仅关心顶层 span（parentSpanId === ''），后端会推所有 span 变更，本地这里过滤一次。
// 增量逻辑：
//   - insert：parentSpanId === '' 时 push 进去
//   - update：按 spanId 找到行替换；找不到（被前端筛掉了）就忽略
let unsubscribeStream = null;
function startStream() {
  if (unsubscribeStream) {
    unsubscribeStream();
    unsubscribeStream = null;
  }
  if (!props.traceId) return;
  unsubscribeStream = subscribeTraceStream(props.traceId, {
    onInsert: (sp) => {
      if (!sp || sp.parentSpanId !== '') return;
      // 防重：同 spanId 已存在就走 update 逻辑
      const idx = spans.value.findIndex((x) => x.spanId === sp.spanId);
      if (idx >= 0) spans.value[idx] = sp;
      else spans.value = [...spans.value, sp];
      // 顶层失败状态变化时同步 summary 上的 hasFailure 标记
      if (!summary.value) summary.value = computeFallbackSummary(spans.value);
      else if (sp.success === false) summary.value = { ...summary.value, hasFailure: true };
    },
    onUpdate: (sp) => {
      if (!sp || !sp.spanId) return;
      const idx = spans.value.findIndex((x) => x.spanId === sp.spanId);
      if (idx >= 0) {
        // 创建新数组让 Vue 触发重渲染
        const next = spans.value.slice();
        next[idx] = sp;
        spans.value = next;
      }
      if (sp.success === false && summary.value) {
        summary.value = { ...summary.value, hasFailure: true };
      }
    },
  });
}

function computeFallbackSummary(list) {
  if (!list || !list.length) return null;
  return {
    traceId: props.traceId,
    spanCount: list.length,
    hasFailure: list.some((s) => s.success === false),
  };
}

function enterSpan(s) {
  if (!s || !s.spanId) return;
  emit('enter-span', { spanId: s.spanId, filename: s.filename || stageLabel(s.stage) });
}

function onRowClick(s) {
  // 行点击只是高亮，真正下钻走"深入"按钮或甘特条
  hoveredSpanId.value = s.spanId;
}

function onChartHover(span) {
  hoveredSpanId.value = (span && span.spanId) || '';
}

function rowInfoTooltip(s) {
  const parts = [];
  if (s.errorMessage) parts.push(s.errorMessage);
  if (s.filename) parts.push(s.filename);
  if (s.extra) parts.push(s.extra);
  return parts.join('\n');
}

async function onDelete() {
  if (!props.traceId || deleting.value) return;
  if (typeof window !== 'undefined' && !window.confirm('确认删除该 trace 的全部 span 数据？此操作不可恢复。')) {
    return;
  }
  deleting.value = true;
  try {
    await deleteTrace(props.traceId);
    emit('back');
  } catch (e) {
    error.value = '删除失败：' + ((e && e.message) || e);
  } finally {
    deleting.value = false;
  }
}

function shortId(id) {
  if (!id) return '—';
  return id.length > 12 ? id.slice(0, 8) + '…' + id.slice(-4) : id;
}

async function toggleFullscreen() {
  fullscreen.value = !fullscreen.value;
  await nextTick();
  if (ganttRef.value && ganttRef.value.resize) ganttRef.value.resize();
}

function onKeydown(e) {
  if (fullscreen.value && e.key === 'Escape') {
    e.preventDefault();
    e.stopPropagation();
    fullscreen.value = false;
  }
}

function onResize() {
  if (ganttRef.value && ganttRef.value.resize) ganttRef.value.resize();
}

onMounted(() => {
  load();
  startStream();
  window.addEventListener('keydown', onKeydown, true);
  window.addEventListener('resize', onResize);
});
onUnmounted(() => {
  if (unsubscribeStream) { unsubscribeStream(); unsubscribeStream = null; }
  window.removeEventListener('keydown', onKeydown, true);
  window.removeEventListener('resize', onResize);
});
watch(() => props.traceId, () => {
  load();
  startStream();
  // trace 切换后时序图缓存失效：回到甘特图并重置，下次切 Tab 时重新拉取
  activeTab.value = 'gantt';
  seqEvents.value = [];
  seqLoaded.value = false;
  seqError.value = '';
});
</script>

<style scoped>
.trace-tops {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 16px 20px 24px;
}

.trace-tops.is-fullscreen .trace-tops-chart-card {
  position: fixed;
  inset: 56px 0 0 0;
  z-index: 1050;
  border-radius: 0;
  border-left: none;
  border-right: none;
  background: var(--tt-surface-base);
  display: flex;
  flex-direction: column;
}
.trace-tops.is-fullscreen .trace-tops-chart-body {
  flex: 1;
  min-height: 0;
}

.tt-button-danger { color: var(--tt-status-fail); }
.tt-button-danger:hover {
  background: var(--tt-status-fail-bg);
  color: var(--tt-status-fail);
  border-color: rgba(239, 68, 68, 0.4);
}

.trace-tops-skeleton-chart {
  height: 240px;
  border-radius: var(--tt-radius-card);
}

/* 甘特图卡片 */
.trace-tops-chart-card {
  display: flex;
  flex-direction: column;
}
.trace-tops-chart-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--tt-border-soft);
}
.trace-tops-chart-header .tt-title-sm { flex-shrink: 0; }
.trace-tops-chart-tip { flex: 1; }
.trace-tops-fullscreen-btn { flex-shrink: 0; }

.trace-tops-chart-body { padding: 8px 12px; }

/* 图表与列表之间的拖动分隔条 */
.trace-tops-resizer {
  height: 8px;
  cursor: row-resize;
  background: var(--tt-surface-base);
  border-top: 1px solid var(--tt-border-soft);
  display: flex;
  align-items: center;
  justify-content: center;
  position: relative;
  user-select: none;
  touch-action: none;
}
.trace-tops-resizer:hover,
.trace-tops-resizer.is-dragging {
  background: var(--tt-accent-soft);
}
.trace-tops-resizer-handle {
  width: 36px;
  height: 3px;
  border-radius: 2px;
  background: var(--tt-border-strong);
  transition: background 80ms ease;
}
.trace-tops-resizer:hover .trace-tops-resizer-handle,
.trace-tops-resizer.is-dragging .trace-tops-resizer-handle {
  background: var(--tt-accent);
}

/* 列表 */
.trace-tops-list-card {
  display: flex;
  flex-direction: column;
}

.trace-tops-row {
  display: grid;
  grid-template-columns:
    14px
    140px
    110px
    minmax(180px, 1fr)
    140px
    140px
    80px
    72px;
  gap: 12px;
  align-items: center;
  padding: 8px 16px;
  height: var(--tt-row-height);
}
.trace-tops-list-header {
  cursor: default;
  background: var(--tt-surface-soft);
  border-bottom: 1px solid var(--tt-border-soft);
  height: 36px;
}
.trace-tops-list-header:hover { background: var(--tt-surface-soft); }
.trace-tops-row.is-hovered { background: var(--tt-accent-soft); }

.trace-tops-cell-stage {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--tt-text-primary);
}
.trace-tops-stage-swatch {
  width: 10px;
  height: 10px;
  border-radius: 2px;
  flex-shrink: 0;
}

.trace-tops-cell-id {
  color: var(--tt-text-secondary);
  font-size: 12px;
}

.trace-tops-cell-name { min-width: 0; }
.trace-tops-info-error { color: var(--tt-status-fail); }
.trace-tops-info-extra { color: var(--tt-text-muted); }

.trace-tops-cell-time,
.trace-tops-cell-end {
  white-space: nowrap;
  font-size: 12px;
}
.trace-tops-cell-duration { text-align: right; }
.trace-tops-cell-action { text-align: right; }

.trace-tops-drilldown-btn {
  border: none;
  background: transparent;
  color: var(--tt-accent);
  font-size: 12px;
  cursor: pointer;
  padding: 4px 6px;
  border-radius: 4px;
  transition: background 80ms ease;
}
.trace-tops-drilldown-btn:hover {
  background: var(--tt-accent-soft);
}

/* 甘特图 / 时序图 Tab（分段控件风格） */
.trace-tops-tabs {
  display: inline-flex;
  align-self: flex-start;
  background: var(--tt-surface-sunken);
  border-radius: var(--tt-radius-control);
  padding: 3px;
  gap: 2px;
}
.trace-tops-tab {
  border: none;
  background: transparent;
  padding: 5px 18px;
  font-size: 13px;
  color: var(--tt-text-secondary);
  border-radius: 6px;
  cursor: pointer;
  transition: background var(--tt-duration-fast) ease,
              color var(--tt-duration-fast) ease;
}
.trace-tops-tab:hover { color: var(--tt-text-primary); }
.trace-tops-tab.is-active {
  background: var(--tt-surface-base);
  color: var(--tt-text-primary);
  font-weight: 500;
  box-shadow: var(--tt-shadow-card);
}

/* 时序图卡片 */
.trace-tops-seq-card {
  display: flex;
  flex-direction: column;
}
.trace-tops-seq-body { padding: 8px 12px; }
.trace-tops-seq-skeleton {
  height: 320px;
  border-radius: var(--tt-radius-card);
}
</style>

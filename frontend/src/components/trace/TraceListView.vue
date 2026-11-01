<template>
  <div class="trace-list">
    <!-- 1. 采集设置（折叠） -->
    <TraceSettingsPanel @changed="onSettingsChanged" />

    <!-- 2. 筛选栏 + 暂停 badge -->
    <section class="trace-list-filters tt-card">
      <div class="trace-list-filters-row">
        <div class="trace-list-filter">
          <label class="tt-meta">类型</label>
          <select class="tt-input" v-model="filter.traceType" @change="reload">
            <option value="">全部</option>
            <option value="upload">文件上传</option>
            <option value="scan">目录扫描</option>
          </select>
        </div>
        <div class="trace-list-filter">
          <label class="tt-meta">任务 ID</label>
          <input
            class="tt-input"
            v-model.trim="filter.taskId"
            placeholder="精确匹配（留空表示不筛选）"
            @keydown.enter.prevent="reload"
          />
        </div>
        <div class="trace-list-filter">
          <label class="tt-meta">开始</label>
          <input class="tt-input" type="datetime-local" v-model="filter.from" @change="reload" />
        </div>
        <div class="trace-list-filter">
          <label class="tt-meta">结束</label>
          <input class="tt-input" type="datetime-local" v-model="filter.to" @change="reload" />
        </div>
        <div class="trace-list-filter trace-list-filter-actions">
          <button class="tt-button tt-button-primary" @click="reload" :disabled="loading">查询</button>
          <button class="tt-button" @click="resetFilters" :disabled="loading">重置</button>
        </div>
      </div>
    </section>

    <!-- 3. 列表 -->
    <section class="trace-list-result">
      <header class="trace-list-result-header">
        <span class="tt-title-sm">追踪记录</span>
        <span class="tt-meta">共 {{ total }} 条</span>
      </header>

      <!-- 加载骨架 -->
      <div v-if="loading" class="tt-card">
        <div v-for="n in 5" :key="n" class="tt-row trace-list-row">
          <span class="tt-skeleton trace-list-skeleton-dot"></span>
          <div class="tt-row-main">
            <div class="tt-skeleton trace-list-skeleton-line trace-list-skeleton-line-w-l"></div>
            <div class="tt-skeleton trace-list-skeleton-line trace-list-skeleton-line-w-s"></div>
          </div>
          <div class="tt-skeleton trace-list-skeleton-line trace-list-skeleton-line-w-xs"></div>
        </div>
      </div>

      <!-- 错误态 -->
      <div v-else-if="error" class="tt-empty">
        <span class="tt-badge tt-badge-fail">加载失败</span>
        <span class="tt-meta">{{ error }}</span>
        <button class="tt-button" @click="reload">重试</button>
      </div>

      <!-- 空态 -->
      <div v-else-if="!items.length" class="tt-empty">
        <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor"
             stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="11" cy="11" r="7" stroke="currentColor"></circle>
          <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
        </svg>
        <span>暂无符合条件的追踪记录</span>
      </div>

      <!-- 数据列表 -->
      <div v-else class="tt-card">
        <header class="trace-list-row trace-list-head">
          <span class="trace-list-cell-status"></span>
          <span class="trace-list-cell-name tt-meta">任务</span>
          <span class="trace-list-cell-type tt-meta">类型</span>
          <span class="trace-list-cell-time tt-meta">开始时间</span>
          <span class="trace-list-cell-duration tt-meta">耗时</span>
          <span class="trace-list-cell-spans tt-meta">Span 数</span>
          <span class="trace-list-cell-status-text tt-meta">状态</span>
          <span class="trace-list-cell-action"></span>
        </header>

        <div
          v-for="item in items"
          :key="item.traceId"
          class="tt-row trace-list-row"
          @click="enterTrace(item)"
        >
          <span
            class="tt-dot trace-list-cell-status"
            :class="item.hasFailure ? 'tt-dot-fail' : 'tt-dot-ok'"
          ></span>

          <div class="trace-list-cell-name tt-truncate" :title="taskTooltip(item)">
            <div class="tt-title-sm tt-truncate">{{ item.taskName || item.taskId || '—' }}</div>
            <div class="tt-meta">
              <span class="tt-num">{{ shortId(item.traceId) }}</span>
              <span v-if="item.runSeq > 1"> · #{{ item.runSeq }}</span>
            </div>
          </div>

          <span class="trace-list-cell-type">
            <span class="tt-badge" :class="typeBadgeClass(item.traceType)">
              {{ traceTypeLabel(item.traceType) }}
            </span>
          </span>

          <span class="trace-list-cell-time tt-num tt-meta">{{ formatTimeFull(item.startMs) }}</span>
          <span class="trace-list-cell-duration tt-num">{{ formatMs(item.durationMs) }}</span>
          <span class="trace-list-cell-spans tt-num tt-meta">{{ item.spanCount }}</span>

          <span class="trace-list-cell-status-text">
            <span class="tt-badge" :class="item.hasFailure ? 'tt-badge-fail' : 'tt-badge-ok'">
              {{ item.hasFailure ? '失败' : '成功' }}
            </span>
          </span>

          <span class="trace-list-cell-action tt-meta">›</span>
        </div>
      </div>

      <!-- 分页 -->
      <footer v-if="!loading && !error && total > 0" class="trace-list-pagination">
        <button class="tt-button" :disabled="page <= 1 || loading" @click="goPage(page - 1)">‹ 上一页</button>
        <span class="tt-meta">第 {{ page }} / {{ totalPages }} 页 · 共 {{ total }} 条</span>
        <button class="tt-button" :disabled="page >= totalPages || loading" @click="goPage(page + 1)">下一页 ›</button>
        <select class="tt-input trace-list-page-size" v-model.number="pageSize" @change="onPageSizeChange">
          <option :value="20">20 条/页</option>
          <option :value="50">50 条/页</option>
          <option :value="100">100 条/页</option>
          <option :value="200">200 条/页</option>
        </select>
      </footer>
    </section>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue';
import TraceSettingsPanel from './TraceSettingsPanel.vue';
import { listTracedTasks } from './api.js';
import { formatMs, formatTimeFull, traceTypeLabel } from './stage_meta.js';

const emit = defineEmits(['loading', 'enter-trace']);

const items = ref([]);
const total = ref(0);
const loading = ref(false);
const error = ref('');

const page = ref(1);
const pageSize = ref(20);

const filter = reactive({
  traceType: '',
  taskId: '',
  from: defaultFrom(),  // 默认 1 年前
  to: defaultTo(),      // 默认当天 23:59:59
});

function toLocalDatetimeStr(d) {
  const pad = n => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function defaultFrom() {
  const d = new Date();
  d.setFullYear(d.getFullYear() - 1);
  d.setHours(0, 0, 0, 0);
  return toLocalDatetimeStr(d);
}

function defaultTo() {
  const d = new Date();
  d.setHours(23, 59, 0, 0);
  return toLocalDatetimeStr(d);
}

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)));

async function load() {
  loading.value = true;
  error.value = '';
  emit('loading', true);
  try {
    const resp = await listTracedTasks({
      traceType: filter.traceType || '',
      taskId: filter.taskId || '',
      fromMs: parseDatetimeMs(filter.from),
      toMs: parseDatetimeMs(filter.to),
      page: page.value,
      size: pageSize.value,
    });
    items.value = resp.items || [];
    total.value = resp.total || 0;
    // page 越界自动退回最后一页
    if (page.value > totalPages.value) {
      page.value = totalPages.value;
      // 二次请求以拿到正确数据；不会无限递归因为下次满足 page <= totalPages
      loading.value = false;
      await load();
      return;
    }
  } catch (e) {
    error.value = (e && e.message) || String(e);
    items.value = [];
    total.value = 0;
  } finally {
    loading.value = false;
    emit('loading', false);
  }
}

function reload() {
  page.value = 1;
  load();
}

function resetFilters() {
  filter.traceType = '';
  filter.taskId = '';
  filter.from = defaultFrom();
  filter.to = defaultTo();
  page.value = 1;
  pageSize.value = 20;
  load();
}

function goPage(p) {
  if (p < 1 || p > totalPages.value) return;
  page.value = p;
  load();
}

function onPageSizeChange() {
  page.value = 1;
  load();
}

function enterTrace(item) {
  emit('enter-trace', {
    traceId: item.traceId,
    taskName: item.taskName || item.taskId || item.traceId,
  });
}

// 设置面板变化时刷新一次列表（开关切换可能影响后续 trace 显示）
function onSettingsChanged() {
  load();
}

// helpers

function shortId(id) {
  if (!id) return '—';
  return id.length > 12 ? id.slice(0, 8) + '…' : id;
}

function typeBadgeClass(type) {
  // upload 用 accent 软色；scan 用中性色；其他归 muted
  if (type === 'upload') return 'tt-badge-type-upload';
  if (type === 'scan') return 'tt-badge-type-scan';
  return '';
}

function taskTooltip(item) {
  const parts = [];
  if (item.taskName) parts.push(`任务名: ${item.taskName}`);
  if (item.taskId) parts.push(`taskId: ${item.taskId}`);
  parts.push(`traceId: ${item.traceId}`);
  if (item.runSeq > 1) parts.push(`第 ${item.runSeq} 次运行`);
  return parts.join('\n');
}

function parseDatetimeMs(s) {
  if (!s) return 0;
  const ts = Date.parse(s);
  return Number.isFinite(ts) ? ts : 0;
}

onMounted(load);
</script>

<style scoped>
.trace-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 16px 20px 24px;
}

/* 筛选栏 */
.trace-list-filters {
  padding: 12px 16px;
}
.trace-list-filters-row {
  display: flex;
  align-items: flex-end;
  flex-wrap: wrap;
  gap: 12px;
}
.trace-list-filter {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 150px;
}
.trace-list-filter label {
  font-size: 11px;
  color: var(--tt-text-muted);
}
.trace-list-filter-actions {
  flex-direction: row;
  align-items: center;
  gap: 8px;
  margin-left: auto;
  min-width: 0;
}

/* 结果区 */
.trace-list-result-header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  padding: 0 4px 8px;
}

/* 列表行布局 */
.trace-list-row {
  display: grid;
  grid-template-columns: 14px minmax(160px, 2fr) 100px minmax(160px, 1.5fr) 80px 72px 72px 18px;
  gap: 12px;
  align-items: center;
  padding: 8px 16px;
  height: var(--tt-row-height);
}
.trace-list-head {
  cursor: default;
  background: var(--tt-surface-soft);
  border-bottom: 1px solid var(--tt-border-soft);
  height: 36px;
}
.trace-list-head:hover { background: var(--tt-surface-soft); }

.trace-list-cell-status { display: inline-block; width: 14px; height: 14px; }
.trace-list-cell-name { min-width: 0; }
.trace-list-cell-type { white-space: nowrap; }
.trace-list-cell-time { white-space: nowrap; font-size: 12px; }
.trace-list-cell-duration { text-align: right; }
.trace-list-cell-spans { text-align: right; }
.trace-list-cell-status-text { text-align: center; }
.trace-list-cell-action { color: var(--tt-text-muted); text-align: right; }

/* 类型 badge 配色 */
.tt-badge-type-upload {
  color: #4338ca;
  background: #eef2ff;
}
.tt-badge-type-scan {
  color: #0e7490;
  background: #ecfeff;
}

/* 骨架屏行 */
.trace-list-skeleton-dot {
  width: 14px;
  height: 14px;
  border-radius: 50%;
}
.trace-list-skeleton-line {
  height: 10px;
  margin: 4px 0;
}
.trace-list-skeleton-line-w-l { width: 60%; }
.trace-list-skeleton-line-w-s { width: 30%; }
.trace-list-skeleton-line-w-xs { width: 60px; }

/* 分页 */
.trace-list-pagination {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 4px 0;
  flex-wrap: wrap;
}
.trace-list-page-size {
  margin-left: auto;
  width: auto;
  height: 32px;
  padding: 0 28px 0 12px;
}
</style>

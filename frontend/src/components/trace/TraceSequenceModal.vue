<template>
  <!--
    HTTP 交互时序图全屏弹窗（从 Dashboard「HTTP 请求日志」工具栏进入）
      - 工具栏：时间范围 / 方向 / taskId 过滤 + 刷新 + 加载更多（分页 append）
      - 主体：TraceSequence 泳道时序图
      - 点击图中事件 → GetHTTPLogByID 拉全量详情，右侧抽屉展示（简版详情，
        风格对齐 Dashboard HTTP 日志弹窗的详情视图，用 vue-json-pretty 展示头/体）
      - Esc：先关详情抽屉，再关弹窗
  -->
  <div class="modal-overlay" @click.self="onClose">
    <div class="modal-box modal-box-fullscreen trace-modal trace-seq-modal">
      <div class="modal-header">
        <span>
          HTTP 交互时序图
          <span class="tt-meta trace-seq-modal-count">共 {{ total }} 条 · 已加载 {{ events.length }} 条</span>
        </span>
        <div class="modal-header-actions">
          <button class="action-btn" @click="reload" :disabled="loading">
            <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"></polyline><polyline points="1 20 1 14 7 14"></polyline><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"></path></svg>
            {{ loading ? '加载中…' : '刷新' }}
          </button>
          <button class="btn-return" @click="onClose">返回</button>
        </div>
      </div>

      <div class="modal-body trace-modal-body trace-seq-modal-body">
        <!-- 过滤工具栏 -->
        <div class="trace-seq-toolbar">
          <select v-model.number="rangeMs" class="tt-input" @change="reload" title="时间范围">
            <option :value="15 * 60 * 1000">最近 15 分钟</option>
            <option :value="60 * 60 * 1000">最近 1 小时</option>
            <option :value="6 * 60 * 60 * 1000">最近 6 小时</option>
            <option :value="24 * 60 * 60 * 1000">最近 24 小时</option>
          </select>
          <select v-model="direction" class="tt-input" @change="reload" title="方向">
            <option value="">全部方向</option>
            <option value="in">浏览器 → 客户端</option>
            <option value="out">客户端 → 后端</option>
          </select>
          <input
            v-model="taskIdDraft"
            class="tt-input trace-seq-taskid-input"
            placeholder="按 taskId 过滤（可选，回车生效）"
            @keyup.enter="reload" />
          <span class="tt-meta trace-seq-toolbar-tip">滚轮上下平移 · Ctrl+滚轮缩放时间轴</span>
          <button
            v-if="events.length < total"
            class="tt-button"
            @click="loadMore"
            :disabled="loading"
          >{{ loading ? '加载中…' : `加载更多（还有 ${total - events.length} 条）` }}</button>
        </div>

        <div v-if="error" class="tt-empty">
          <span class="tt-badge tt-badge-fail">加载失败</span>
          <span class="tt-meta">{{ error }}</span>
          <button class="tt-button" @click="reload">重试</button>
        </div>

        <!-- 时序图主体 -->
        <div v-show="!error" class="trace-seq-chart-wrap">
          <TraceSequence
            ref="seqRef"
            :events="events"
            :loading="loading"
            :empty-text="emptyText"
            @event-click="openDetail"
          />
        </div>

        <!-- 详情抽屉：点击图中事件后从右侧滑出 -->
        <transition name="trace-seq-detail-slide">
          <aside v-if="detail" class="trace-seq-detail">
            <div class="trace-seq-detail-header">
              <span class="trace-seq-detail-title">
                <span class="http-method" :class="'method-' + (detail.method || '').toLowerCase()">{{ detail.method }}</span>
                <span class="http-status" :class="statusClass(detail.statusCode)">{{ detail.statusCode || '网络错误' }}</span>
              </span>
              <button class="tt-button" @click="detail = null">关闭</button>
            </div>

            <div class="trace-seq-detail-path" :title="detail.path + (detail.query ? '?' + detail.query : '')">
              {{ detail.path }}<span v-if="detail.query" class="trace-seq-detail-query">?{{ detail.query }}</span>
            </div>
            <div class="tt-meta trace-seq-detail-meta">
              {{ detail.timestamp || formatTimeFull(detail.timestampMs) }} · {{ formatMs(detail.durationMs) }} ·
              {{ formatBytes(detail.responseBytes) }} · {{ directionLabel(detail.direction) }}
            </div>
            <div class="tt-meta trace-seq-detail-meta" v-if="detail.peer">对端 {{ detail.peer }}</div>
            <div class="tt-meta trace-seq-detail-meta" v-if="detail.taskId">taskId: {{ detail.taskId }}</div>

            <div class="trace-seq-detail-section">
              <div class="trace-seq-detail-section-title">请求头</div>
              <vue-json-pretty :data="detail.requestHeaders || {}" :deep="2" :show-line="true" :show-double-quotes="false" />
            </div>

            <div class="trace-seq-detail-section" v-if="detail.requestBody">
              <div class="trace-seq-detail-section-title">
                请求体
                <span v-if="detail.requestBodyTruncated" class="trace-seq-truncated-badge">已截断（仅显示前 1 MB）</span>
              </div>
              <vue-json-pretty
                v-if="parsedRequestBody !== undefined"
                :data="parsedRequestBody"
                :deep="3"
                :show-line="true"
                :show-double-quotes="false"
              />
              <pre v-else class="trace-seq-raw-body">{{ detail.requestBody }}</pre>
            </div>

            <div class="trace-seq-detail-section">
              <div class="trace-seq-detail-section-title">响应头</div>
              <vue-json-pretty :data="detail.responseHeaders || {}" :deep="2" :show-line="true" :show-double-quotes="false" />
            </div>

            <div class="trace-seq-detail-section" v-if="detail.responseBody">
              <div class="trace-seq-detail-section-title">
                响应体
                <span v-if="detail.responseBodyTruncated" class="trace-seq-truncated-badge">已截断（仅显示前 1 MB）</span>
              </div>
              <vue-json-pretty
                v-if="parsedResponseBody !== undefined"
                :data="parsedResponseBody"
                :deep="3"
                :show-line="true"
                :show-double-quotes="false"
              />
              <pre v-else class="trace-seq-raw-body">{{ detail.responseBody }}</pre>
            </div>
          </aside>
        </transition>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue';
import VueJsonPretty from 'vue-json-pretty';
import 'vue-json-pretty/lib/styles.css';
import { GetSequenceEvents, GetHTTPLogByID } from '../../../wailsjs/go/app/App';
import TraceSequence from './TraceSequence.vue';
import { formatMs, formatTimeFull, formatBytes } from './stage_meta.js';
import { directionLabel } from './sequence_helpers.js';
import './tokens.css';

const emit = defineEmits(['close']);

// ─── 查询状态 ───
const PAGE_SIZE = 200;
const events = ref([]);
const total = ref(0);
const page = ref(1);
const loading = ref(false);
const error = ref('');

// 过滤条件：时间范围默认最近 1 小时；方向默认全部；taskId 可选精确过滤
const rangeMs = ref(60 * 60 * 1000);
const direction = ref('');
const taskIdDraft = ref('');

const emptyText = computed(() => '当前条件下暂无 HTTP 交互记录');

async function fetchPage(p) {
  // fromMs = now - rangeMs；toMs 传 0 表示不限制上限（后端语义，见 app.go GetSequenceEvents）
  const fromMs = Date.now() - Number(rangeMs.value);
  return await GetSequenceEvents(fromMs, 0, taskIdDraft.value.trim(), direction.value, p, PAGE_SIZE);
}

// 重新加载：重置到第 1 页
async function reload() {
  if (loading.value) return;
  loading.value = true;
  error.value = '';
  try {
    const resp = await fetchPage(1);
    events.value = (resp && resp.entries) || [];
    total.value = (resp && resp.total) || 0;
    page.value = 1;
  } catch (e) {
    error.value = (e && e.message) || String(e);
    events.value = [];
    total.value = 0;
  } finally {
    loading.value = false;
  }
}

// 加载更多：page 递增，结果 append 进 events
async function loadMore() {
  if (loading.value) return;
  loading.value = true;
  error.value = '';
  try {
    const next = page.value + 1;
    const resp = await fetchPage(next);
    events.value = events.value.concat((resp && resp.entries) || []);
    total.value = (resp && resp.total) || 0;
    page.value = next;
  } catch (e) {
    error.value = (e && e.message) || String(e);
  } finally {
    loading.value = false;
  }
}

// ─── 详情抽屉 ───
const detail = ref(null);

async function openDetail(log) {
  if (!log || log.id === undefined || log.id === null) return;
  // 先用列表级字段打开抽屉，再拉全量（headers/body）替换，与 Dashboard 详情逻辑一致
  detail.value = log;
  try {
    const full = await GetHTTPLogByID(log.id);
    if (full && detail.value && detail.value.id === full.id) {
      detail.value = full;
    }
  } catch (e) {
    console.error('加载日志详情失败:', e);
  }
}

// 尝试把 body 字符串解析成 JSON，失败则返回 undefined（让模板回退到 raw 显示）
const parsedRequestBody = computed(() => tryParseJson(detail.value && detail.value.requestBody));
const parsedResponseBody = computed(() => tryParseJson(detail.value && detail.value.responseBody));

function tryParseJson(text) {
  if (!text || typeof text !== 'string') return undefined;
  const trimmed = text.trim();
  if (!trimmed) return undefined;
  if (!(trimmed.startsWith('{') || trimmed.startsWith('['))) return undefined;
  try {
    return JSON.parse(trimmed);
  } catch (_) {
    return undefined;
  }
}

function statusClass(code) {
  if (!code) return 'status-pending';
  if (code >= 500) return 'status-5xx';
  if (code >= 400) return 'status-4xx';
  if (code >= 300) return 'status-3xx';
  if (code >= 200) return 'status-2xx';
  return 'status-1xx';
}

// ─── 关闭 / Esc ───
function onClose() {
  emit('close');
}

function onKeydown(e) {
  if (e.key !== 'Escape') return;
  e.preventDefault();
  if (detail.value) detail.value = null;
  else onClose();
}

onMounted(() => {
  reload();
  window.addEventListener('keydown', onKeydown);
});
onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown);
});
</script>

<style scoped>
.trace-seq-modal-count {
  margin-left: 10px;
  font-weight: 400;
}

.trace-seq-modal-body {
  position: relative;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding: 0;
  background: var(--tt-surface-soft, #f8fafc);
}

/* 过滤工具栏 */
.trace-seq-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 16px;
  background: var(--tt-surface-base);
  border-bottom: 1px solid var(--tt-border-soft);
  flex-shrink: 0;
}
.trace-seq-toolbar .tt-input { width: auto; }
.trace-seq-taskid-input { width: 260px; font-family: var(--tt-font-mono); }
.trace-seq-toolbar-tip { flex: 1; text-align: right; }

/* 图表区 */
.trace-seq-chart-wrap {
  flex: 1;
  min-height: 0;
  padding: 8px 12px;
}

/* 详情抽屉 */
.trace-seq-detail {
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  width: 440px;
  max-width: 90vw;
  background: var(--tt-surface-base);
  border-left: 1px solid var(--tt-border-soft);
  box-shadow: -8px 0 24px rgba(15, 23, 42, 0.08);
  padding: 16px 20px 24px;
  overflow-y: auto;
  z-index: 5;
}
.trace-seq-detail-slide-enter-active,
.trace-seq-detail-slide-leave-active {
  transition: transform var(--tt-duration-normal, 200ms) var(--tt-ease-emphasized, ease),
              opacity var(--tt-duration-normal, 200ms) ease;
}
.trace-seq-detail-slide-enter-from,
.trace-seq-detail-slide-leave-to {
  transform: translateX(24px);
  opacity: 0;
}

.trace-seq-detail-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.trace-seq-detail-title {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.trace-seq-detail-path {
  margin-top: 10px;
  font-size: 13px;
  font-weight: 600;
  color: var(--tt-text-primary);
  word-break: break-all;
}
.trace-seq-detail-query {
  color: var(--tt-text-muted);
  font-weight: 400;
}
.trace-seq-detail-meta {
  margin-top: 6px;
}

.trace-seq-detail-section {
  margin-top: 16px;
}
.trace-seq-detail-section-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--tt-text-secondary);
  margin-bottom: 6px;
}
.trace-seq-truncated-badge {
  margin-left: 6px;
  font-size: 11px;
  font-weight: 400;
  color: var(--tt-status-warn);
}
.trace-seq-raw-body {
  margin: 0;
  padding: 10px 12px;
  background: var(--tt-surface-sunken);
  border-radius: var(--tt-radius-control);
  font-size: 11px;
  font-family: var(--tt-font-mono);
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 320px;
  overflow-y: auto;
}

/* 方法 / 状态徽标（对齐 Dashboard HTTP 日志弹窗的配色习惯） */
.http-method {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 6px;
  font-size: 11px;
  font-weight: 700;
  font-family: var(--tt-font-mono);
  background: var(--tt-surface-sunken);
  color: var(--tt-text-secondary);
}
.method-get { background: #ecfdf5; color: #059669; }
.method-post { background: #eff6ff; color: #2563eb; }
.method-put { background: #fffbeb; color: #d97706; }
.method-delete { background: #fef2f2; color: #dc2626; }

.http-status {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 6px;
  font-size: 11px;
  font-weight: 700;
  font-family: var(--tt-font-mono);
}
.status-2xx { background: var(--tt-status-ok-bg); color: var(--tt-status-ok); }
.status-3xx { background: var(--tt-accent-soft); color: var(--tt-accent); }
.status-4xx { background: var(--tt-status-warn-bg); color: var(--tt-status-warn); }
.status-5xx,
.status-pending { background: var(--tt-status-fail-bg); color: var(--tt-status-fail); }
.status-1xx { background: var(--tt-surface-sunken); color: var(--tt-text-secondary); }
</style>

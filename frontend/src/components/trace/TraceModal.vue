<template>
  <div class="modal-overlay" @click.self="onClose">
    <div class="modal-box modal-box-fullscreen trace-modal">
      <div class="modal-header">
        <span class="trace-modal-title">
          <button
            v-if="!atRoot"
            class="trace-modal-back"
            title="返回上一级"
            @click="back"
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                 stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <line x1="19" y1="12" x2="5" y2="12"></line>
              <polyline points="12 19 5 12 12 5"></polyline>
            </svg>
          </button>
          链路追踪
          <template v-for="(crumb, idx) in breadcrumbs" :key="idx">
            <span class="trace-modal-sep">/</span>
            <span class="trace-modal-bc trace-modal-bc-truncate" :title="crumb.full">
              <span class="trace-modal-bc-meta">{{ crumb.kind }}</span>
              <span :class="crumb.isMono ? 'trace-modal-bc-id' : 'trace-modal-bc-text'">
                {{ crumb.label }}
              </span>
            </span>
          </template>
        </span>
        <div class="modal-header-actions">
          <button class="action-btn" @click="openInBrowser" title="在默认浏览器中打开此查看器">
            <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24"
                 fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"></path>
              <polyline points="15 3 21 3 21 9"></polyline>
              <line x1="10" y1="14" x2="21" y2="3"></line>
            </svg>
            浏览器查看
          </button>
          <button class="action-btn" @click="onRefresh" :disabled="loading">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                 stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <polyline points="23 4 23 10 17 10"></polyline>
              <polyline points="1 20 1 14 7 14"></polyline>
              <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"></path>
            </svg>
            {{ loading ? '加载中…' : '刷新' }}
          </button>
          <button class="btn-return" @click="onClose">返回</button>
        </div>
      </div>

      <div class="modal-body trace-modal-body">
        <component
          :is="currentView"
          :key="viewKey"
          v-bind="currentViewProps"
          @loading="onChildLoading"
          @enter-trace="enterTrace"
          @enter-span="enterSpan"
          @back="back"
        />
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, defineAsyncComponent, onMounted, onUnmounted } from 'vue';
import { BrowserOpenURL } from 'wailsjs/runtime';
import './tokens.css';

const TraceListView       = defineAsyncComponent(() => import('./TraceListView.vue'));
const TraceTopSpansView   = defineAsyncComponent(() => import('./TraceTopSpansView.vue'));
const TraceChildSpansView = defineAsyncComponent(() => import('./TraceChildSpansView.vue'));

const emit = defineEmits(['close']);

const props = defineProps({
  // 可选：一打开就直接定位到某条 trace 的顶层 span 视图，而不是先停在列表上。
  // 用于"历史记录 → 查看链路"按钮等场景。空字符串 / 未传时退回原行为（显示列表）。
  initialTraceId: { type: String, default: '' },
  initialTaskName: { type: String, default: '' },
});

// ─── 导航栈 ───
//
// 每一帧表示一个视图层级：
//   { type: 'list' }
//   { type: 'top-spans', traceId, taskName }
//   { type: 'children', spanId, filename }
//
// children 帧可以无限嵌套（例如 upload_task → upload_file → chunk_upload）。
// 初始栈：默认是 [{ type: 'list' }]；如果调用方传了 initialTraceId，则
// 直接把 top-spans 帧 push 进去，让用户进入就看到目标任务的链路。
const stack = ref(
  props.initialTraceId
    ? [{ type: 'list' }, { type: 'top-spans', traceId: props.initialTraceId, taskName: props.initialTaskName || '' }]
    : [{ type: 'list' }]
);
const loading = ref(false);
const refreshTick = ref(0);

const current = computed(() => stack.value[stack.value.length - 1]);
const atRoot = computed(() => stack.value.length === 1);

// 面包屑（去掉根，只显示后续帧）
const breadcrumbs = computed(() => {
  const out = [];
  for (let i = 1; i < stack.value.length; i++) {
    const f = stack.value[i];
    if (f.type === 'top-spans') {
      out.push({
        kind: 'trace',
        label: shortId(f.traceId),
        full: f.traceId,
        isMono: true,
      });
    } else if (f.type === 'children') {
      out.push({
        kind: 'span',
        label: f.filename || '—',
        full: f.filename || '',
        isMono: false,
      });
    }
  }
  return out;
});

const viewKey = computed(() => {
  const f = current.value;
  return `${f.type}:${f.traceId || ''}:${f.spanId || ''}:${refreshTick.value}`;
});

const currentView = computed(() => {
  switch (current.value.type) {
    case 'children':  return TraceChildSpansView;
    case 'top-spans': return TraceTopSpansView;
    default:          return TraceListView;
  }
});

const currentViewProps = computed(() => {
  const f = current.value;
  switch (f.type) {
    case 'top-spans':
      return { traceId: f.traceId, taskName: f.taskName };
    case 'children':
      return { spanId: f.spanId, filename: f.filename };
    default:
      return {};
  }
});

// ─── 状态机 ───

function enterTrace({ traceId, taskName }) {
  if (!traceId) return;
  stack.value = [
    { type: 'list' },
    { type: 'top-spans', traceId, taskName: taskName || '' },
  ];
}

function enterSpan({ spanId, filename }) {
  if (!spanId) return;
  // 在当前栈顶 push 新的 children 帧
  stack.value = [
    ...stack.value,
    { type: 'children', spanId, filename: filename || '' },
  ];
}

function back() {
  if (stack.value.length <= 1) return;
  stack.value = stack.value.slice(0, -1);
}

function onRefresh() {
  refreshTick.value++;
}

function onClose() {
  emit('close');
}

function onChildLoading(v) {
  loading.value = !!v;
}

function openInBrowser() {
  const url = 'http://localhost:34567/task-traces/viewer';
  try {
    BrowserOpenURL(url);
  } catch (e) {
    if (typeof window !== 'undefined' && window.open) {
      window.open(url, '_blank');
    }
  }
}

function onKeydown(e) {
  if (e.key === 'Escape') {
    e.preventDefault();
    if (atRoot.value) onClose();
    else back();
  }
}

onMounted(() => window.addEventListener('keydown', onKeydown));
onUnmounted(() => window.removeEventListener('keydown', onKeydown));

function shortId(id) {
  if (!id) return '—';
  return id.length > 12 ? id.slice(0, 8) + '…' : id;
}
</script>

<style scoped>
.trace-modal-title {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  flex: 1;
  min-width: 0;
}

.trace-modal-back {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 6px;
  border: 1px solid #e5e7eb;
  background: #fff;
  color: #475569;
  cursor: pointer;
  transition: background 80ms ease, color 80ms ease;
  flex-shrink: 0;
}
.trace-modal-back:hover {
  background: #f1f5f9;
  color: #111827;
}

.trace-modal-sep {
  color: #cbd5e1;
  font-weight: 400;
  user-select: none;
}

.trace-modal-bc {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 400;
  color: #475569;
  min-width: 0;
}

.trace-modal-bc-truncate {
  max-width: 280px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.trace-modal-bc-meta {
  color: #94a3b8;
  font-size: 12px;
}

.trace-modal-bc-id {
  font-family: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;
  color: #111827;
  font-size: 12px;
}

.trace-modal-bc-text {
  color: #111827;
}

.trace-modal-body {
  background: var(--tt-surface-soft, #f8fafc);
  padding: 0;
}
</style>

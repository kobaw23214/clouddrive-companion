<template>
  <!--
    采集设置面板（Apple 系统偏好风格）：
      - 全局开关 + 各模块开关
      - 4 项数字配置（采样阈值 / 保留天数 / 最大行数 / VACUUM 阈值）
      - 立即清理按钮

    UI 原则：
      - 切换开关失败时回滚 UI 状态，不让用户被骗
      - 数字输入失焦/Enter 才提交，避免每按一次就 PATCH 一次
      - 全局关时模块开关灰显（让交互意图明确）
  -->
  <details class="tt-settings" :open="open" @toggle="onToggle">
    <summary class="tt-settings-summary">
      <span class="tt-title-sm">采集设置</span>
      <span class="tt-meta">
        <template v-if="!loaded">加载中…</template>
        <template v-else-if="!modules.global">
          <span class="tt-badge tt-badge-warn">全局已暂停</span>
        </template>
        <template v-else>
          {{ enabledCount }} / {{ modules.modules.length }} 模块开启
        </template>
      </span>
      <svg class="tt-chev" width="14" height="14" viewBox="0 0 24 24" fill="none"
           stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <polyline points="6 9 12 15 18 9"></polyline>
      </svg>
    </summary>

    <div v-if="loaded" class="tt-settings-body">
      <!-- 全局开关 -->
      <div class="tt-settings-row">
        <div class="tt-settings-label">
          <div class="tt-title-sm">全局采集</div>
          <div class="tt-meta">关闭后所有追踪写入暂停，但已飞行的 span 仍会落盘</div>
        </div>
        <label class="tt-toggle">
          <input
            type="checkbox"
            :checked="modules.global"
            :disabled="globalBusy"
            @change="onToggleGlobal($event.target.checked)"
          />
          <span class="tt-toggle-slider"></span>
        </label>
      </div>

      <div class="tt-divider"></div>

      <!-- 各模块开关 -->
      <div
        v-for="m in modules.modules"
        :key="m.code"
        class="tt-settings-row"
        :class="{ 'is-disabled': !modules.global }"
      >
        <div class="tt-settings-label">
          <div class="tt-title-sm">{{ m.name }}</div>
          <div class="tt-meta">{{ m.description || m.code }}</div>
        </div>
        <label class="tt-toggle">
          <input
            type="checkbox"
            :checked="m.enabled"
            :disabled="!modules.global || moduleBusy[m.code]"
            @change="onToggleModule(m, $event.target.checked)"
          />
          <span class="tt-toggle-slider"></span>
        </label>
      </div>

      <div class="tt-divider"></div>

      <!-- 数字配置 -->
      <div class="tt-settings-row tt-settings-row-input">
        <div class="tt-settings-label">
          <div class="tt-title-sm">采样阈值</div>
          <div class="tt-meta">单任务预估 span 数超过该值时启用 chunk 采样；0 表示禁用采样</div>
        </div>
        <div class="tt-input-with-unit">
          <input
            type="number" min="0" class="tt-input tt-input-num"
            :value="cfg.traceSamplingThreshold"
            @blur="commitCfg('traceSamplingThreshold', $event.target.value)"
            @keydown.enter.prevent="commitCfg('traceSamplingThreshold', $event.target.value)"
          />
          <span class="tt-input-unit">spans</span>
        </div>
      </div>

      <div class="tt-settings-row tt-settings-row-input">
        <div class="tt-settings-label">
          <div class="tt-title-sm">保留天数</div>
          <div class="tt-meta">超过该天数的 span 会被定时清理；0 表示不按时间清理</div>
        </div>
        <div class="tt-input-with-unit">
          <input
            type="number" min="0" class="tt-input tt-input-num"
            :value="cfg.traceMaxAgeDays"
            @blur="commitCfg('traceMaxAgeDays', $event.target.value)"
            @keydown.enter.prevent="commitCfg('traceMaxAgeDays', $event.target.value)"
          />
          <span class="tt-input-unit">天</span>
        </div>
      </div>

      <div class="tt-settings-row tt-settings-row-input">
        <div class="tt-settings-label">
          <div class="tt-title-sm">最大行数</div>
          <div class="tt-meta">超过后按 created_at 升序删除最旧 span；0 表示不按行数清理</div>
        </div>
        <div class="tt-input-with-unit">
          <input
            type="number" min="0" class="tt-input tt-input-num"
            :value="cfg.traceMaxRows"
            @blur="commitCfg('traceMaxRows', $event.target.value)"
            @keydown.enter.prevent="commitCfg('traceMaxRows', $event.target.value)"
          />
          <span class="tt-input-unit">行</span>
        </div>
      </div>

      <div class="tt-settings-row tt-settings-row-input">
        <div class="tt-settings-label">
          <div class="tt-title-sm">VACUUM 阈值</div>
          <div class="tt-meta">数据库文件超过该 MB 后清理时执行 VACUUM；0 表示禁用</div>
        </div>
        <div class="tt-input-with-unit">
          <input
            type="number" min="0" class="tt-input tt-input-num"
            :value="cfg.traceVacuumThresholdMB"
            @blur="commitCfg('traceVacuumThresholdMB', $event.target.value)"
            @keydown.enter.prevent="commitCfg('traceVacuumThresholdMB', $event.target.value)"
          />
          <span class="tt-input-unit">MB</span>
        </div>
      </div>

      <div class="tt-divider"></div>

      <!-- 立即清理 -->
      <div class="tt-settings-row">
        <div class="tt-settings-label">
          <div class="tt-title-sm">立即清理</div>
          <div class="tt-meta">使用当前保留策略立即扫描并删除超期 span</div>
        </div>
        <button class="tt-button" :disabled="cleanupBusy" @click="onCleanupNow">
          {{ cleanupBusy ? '清理中…' : '立即清理' }}
        </button>
      </div>
    </div>

    <!-- toast 提示 -->
    <div v-if="toast.message" class="tt-toast" :class="`tt-toast-${toast.kind}`">
      {{ toast.message }}
    </div>
  </details>
</template>

<script setup>
import { reactive, ref, computed, onMounted } from 'vue';
import {
  listTraceModules,
  setTraceModuleEnabled,
  setTraceGlobalEnabled,
  getTraceConfig,
  updateTraceConfig,
  cleanupTraces,
} from './api.js';

const emit = defineEmits(['changed']);

defineProps({
  open: { type: Boolean, default: false },
});

const loaded = ref(false);
const modules = reactive({
  global: true,
  modules: [],
});
const cfg = reactive({
  traceSamplingThreshold: 0,
  traceMaxAgeDays: 0,
  traceMaxRows: 0,
  traceVacuumThresholdMB: 0,
});

const globalBusy = ref(false);
const moduleBusy = reactive({});
const cleanupBusy = ref(false);

const enabledCount = computed(() => modules.modules.filter((m) => m.enabled).length);

// ─── toast ───
const toast = reactive({ message: '', kind: 'ok' });
let toastTimer = 0;
function showToast(message, kind = 'ok') {
  toast.message = message;
  toast.kind = kind;
  if (toastTimer) clearTimeout(toastTimer);
  toastTimer = setTimeout(() => {
    toast.message = '';
  }, 2400);
}

// ─── 数据加载 ───
async function loadAll() {
  try {
    const [m, c] = await Promise.all([listTraceModules(), getTraceConfig()]);
    modules.global = !!m.global;
    modules.modules = m.modules || [];
    Object.assign(cfg, c);
    loaded.value = true;
  } catch (e) {
    showToast('加载追踪设置失败：' + ((e && e.message) || e), 'fail');
  }
}

onMounted(loadAll);

function onToggle(e) {
  // <details> 展开时再加载（首次已经 onMounted 加载过）
  if (e.target.open && !loaded.value) loadAll();
}

// ─── 全局开关 ───
async function onToggleGlobal(next) {
  if (globalBusy.value) return;
  const prev = modules.global;
  modules.global = next; // 乐观更新
  globalBusy.value = true;
  try {
    await setTraceGlobalEnabled(next);
    showToast(next ? '全局采集已开启' : '全局采集已关闭');
    emit('changed', { kind: 'global', enabled: next });
  } catch (e) {
    modules.global = prev; // 回滚
    showToast('切换失败：' + ((e && e.message) || e), 'fail');
  } finally {
    globalBusy.value = false;
  }
}

// ─── 模块开关 ───
async function onToggleModule(m, next) {
  if (moduleBusy[m.code]) return;
  const prev = m.enabled;
  m.enabled = next; // 乐观更新
  moduleBusy[m.code] = true;
  try {
    await setTraceModuleEnabled(m.code, next);
    showToast(`${m.name} ${next ? '已开启' : '已关闭'}`);
    emit('changed', { kind: 'module', code: m.code, enabled: next });
  } catch (e) {
    m.enabled = prev;
    showToast('切换失败：' + ((e && e.message) || e), 'fail');
  } finally {
    moduleBusy[m.code] = false;
  }
}

// ─── 数字配置 ───
async function commitCfg(field, raw) {
  const n = Number(raw);
  if (!Number.isFinite(n) || n < 0) {
    showToast(`${labelOf(field)} 必须是非负数字`, 'fail');
    // 强制回填上次值（input 是非受控刷新，下一次 props 同步会让它跟上）
    cfg[field] = cfg[field];
    return;
  }
  if (Math.floor(n) === cfg[field]) return; // 没变
  const prev = cfg[field];
  cfg[field] = Math.floor(n);
  try {
    const updated = await updateTraceConfig({ [field]: cfg[field] });
    Object.assign(cfg, updated);
    showToast(`${labelOf(field)} 已更新为 ${cfg[field]}`);
    emit('changed', { kind: 'config', field, value: cfg[field] });
  } catch (e) {
    cfg[field] = prev;
    showToast('保存失败：' + ((e && e.message) || e), 'fail');
  }
}

function labelOf(field) {
  switch (field) {
    case 'traceSamplingThreshold': return '采样阈值';
    case 'traceMaxAgeDays':        return '保留天数';
    case 'traceMaxRows':           return '最大行数';
    case 'traceVacuumThresholdMB': return 'VACUUM 阈值';
    default: return field;
  }
}

// ─── 立即清理 ───
async function onCleanupNow() {
  if (cleanupBusy.value) return;
  cleanupBusy.value = true;
  try {
    const deleted = await cleanupTraces(0, 0); // 用 config 当前值
    showToast(`已删除 ${deleted} 条过期 span`);
    emit('changed', { kind: 'cleanup', deleted });
  } catch (e) {
    showToast('清理失败：' + ((e && e.message) || e), 'fail');
  } finally {
    cleanupBusy.value = false;
  }
}

// 暴露给父组件刷新（例如保留策略变更后重新读 config）
defineExpose({ reload: loadAll });
</script>

<style scoped>
/* 折叠面板容器 */
.tt-settings {
  position: relative;
  background: var(--tt-surface-base);
  border: 1px solid var(--tt-border-soft);
  border-radius: var(--tt-radius-card);
  overflow: hidden;
}
.tt-settings[open] .tt-chev {
  transform: rotate(180deg);
}

.tt-settings-summary {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  cursor: pointer;
  list-style: none;
  user-select: none;
}
.tt-settings-summary::-webkit-details-marker { display: none; }
.tt-settings-summary .tt-title-sm { flex-shrink: 0; }
.tt-settings-summary .tt-meta {
  flex: 1;
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.tt-chev {
  color: var(--tt-text-muted);
  transition: transform var(--tt-duration-normal) ease;
}

.tt-settings-body {
  border-top: 1px solid var(--tt-border-soft);
  background: var(--tt-surface-soft);
}

.tt-settings-row {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 14px 16px;
}
.tt-settings-row-input { padding-block: 10px; }
.tt-settings-row.is-disabled .tt-title-sm,
.tt-settings-row.is-disabled .tt-meta {
  opacity: 0.5;
}

.tt-settings-label {
  flex: 1;
  min-width: 0;
}
.tt-settings-label .tt-meta { margin-top: 2px; }

.tt-divider {
  height: 1px;
  background: var(--tt-border-soft);
  margin: 0 16px;
}

/* iOS 风格 toggle */
.tt-toggle {
  position: relative;
  display: inline-block;
  width: 36px;
  height: 22px;
  flex-shrink: 0;
}
.tt-toggle input {
  opacity: 0;
  width: 0;
  height: 0;
}
.tt-toggle-slider {
  position: absolute;
  inset: 0;
  background: var(--tt-border-strong);
  border-radius: 999px;
  transition: background var(--tt-duration-fast) ease;
  cursor: pointer;
}
.tt-toggle-slider::before {
  content: '';
  position: absolute;
  top: 2px;
  left: 2px;
  width: 18px;
  height: 18px;
  background: #fff;
  border-radius: 50%;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.18);
  transition: transform var(--tt-duration-normal) var(--tt-ease-emphasized);
}
.tt-toggle input:checked + .tt-toggle-slider {
  background: var(--tt-accent);
}
.tt-toggle input:checked + .tt-toggle-slider::before {
  transform: translateX(14px);
}
.tt-toggle input:disabled + .tt-toggle-slider {
  opacity: 0.5;
  cursor: not-allowed;
}

/* 数字输入 + 单位 */
.tt-input-with-unit {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.tt-input-num {
  width: 110px;
  text-align: right;
  font-family: var(--tt-font-mono);
}
.tt-input-unit {
  color: var(--tt-text-muted);
  font-size: 12px;
  width: 36px;
}

/* toast */
.tt-toast {
  position: absolute;
  top: 12px;
  right: 16px;
  padding: 8px 14px;
  border-radius: var(--tt-radius-control);
  font-size: 12px;
  z-index: 10;
  box-shadow: var(--tt-shadow-pop);
  animation: tt-toast-in 200ms var(--tt-ease-emphasized) both;
}
.tt-toast-ok   { background: var(--tt-status-ok-bg);   color: var(--tt-status-ok); }
.tt-toast-fail { background: var(--tt-status-fail-bg); color: var(--tt-status-fail); }
@keyframes tt-toast-in {
  from { opacity: 0; transform: translateY(-4px); }
  to   { opacity: 1; transform: translateY(0); }
}
</style>

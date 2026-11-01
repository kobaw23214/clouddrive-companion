<template>
  <div class="config-editor">
    <!-- ── 顶部标题栏 ── -->
    <header class="editor-header">
      <div class="header-left">
        <button class="btn-back" @click="backToDashboard" title="返回上传面板">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18" stroke-linecap="round" stroke-linejoin="round">
            <line x1="19" y1="12" x2="5" y2="12"/>
            <polyline points="12 19 5 12 12 5"/>
          </svg>
        </button>
        <div class="header-titles">
          <h1 class="header-title">设置</h1>
          <p class="header-path" :title="configPath">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="11" height="11" stroke-linecap="round" stroke-linejoin="round">
              <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>
              <polyline points="14 2 14 8 20 8"/>
            </svg>
            {{ configPath }}
          </p>
        </div>
      </div>

      <div class="header-actions">
        <div class="search-box">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="11" cy="11" r="8"/>
            <line x1="21" y1="21" x2="16.65" y2="16.65"/>
          </svg>
          <input v-model="searchQuery" type="text" placeholder="搜索配置项" spellcheck="false"/>
          <button v-if="searchQuery" class="search-clear" @click="searchQuery = ''" title="清空">&times;</button>
        </div>

        <button class="btn btn-outline" @click="resetAll" :disabled="saving" title="把所有配置项恢复为默认值（需再点保存才会写入）">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="1 4 1 10 7 10"/>
            <path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"/>
          </svg>
          恢复默认
        </button>

        <button class="btn btn-outline" @click="saveConfig" :disabled="saving || restarting || !hasChanges">
          <svg v-if="!saving" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14" stroke-linecap="round" stroke-linejoin="round">
            <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/>
            <polyline points="17 21 17 13 7 13 7 21"/>
            <polyline points="7 3 7 8 15 8"/>
          </svg>
          <span v-if="saving" class="spinner dark"></span>
          {{ saving ? '保存中...' : (hasChanges ? `保存 (${changedCount})` : '已保存') }}
        </button>

        <!-- 配置多在启动时读取，保存后重启才生效，因此把重启做成主操作 -->
        <button
            class="btn btn-primary"
            @click="saveAndRestart"
            :disabled="saving || restarting"
            title="保存配置并重新启动客户端，使新配置立即生效">
          <svg v-if="!restarting" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="23 4 23 10 17 10"/>
            <polyline points="1 20 1 14 7 14"/>
            <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/>
          </svg>
          <span v-if="restarting" class="spinner"></span>
          {{ restarting ? '正在重启...' : '保存并重启' }}
        </button>
      </div>
    </header>

    <!-- ── 状态提示 ── -->
    <transition name="fade">
      <div v-if="statusMessage" :class="['status-bar', statusType]">
        <span class="status-text">{{ statusMessage }}</span>
        <button class="status-close" @click="statusMessage = ''">&times;</button>
      </div>
    </transition>

    <!-- ── 加载状态 ── -->
    <div v-if="loading" class="loading-state">
      <div class="spinner large"></div>
      <p>正在加载配置...</p>
    </div>

    <!-- ── 主体：左侧图标导航 + 右侧配置卡片 ── -->
    <div v-else class="editor-body">
      <!-- 左侧图标导航 -->
      <nav class="side-nav">
        <button
            v-for="group in groupedFields"
            :key="group.name"
            :class="['nav-item', { active: activeGroup === group.name }]"
            @click="selectGroup(group.name)">
          <span class="nav-icon" v-html="groupIcon(group.name)"></span>
          <span class="nav-text">
            <span class="nav-name">{{ group.name }}</span>
            <span class="nav-meta">{{ group.fields.length }} 项</span>
          </span>
          <span v-if="groupChangedCount(group) > 0" class="nav-dot" :title="`${groupChangedCount(group)} 项已改动`">
            {{ groupChangedCount(group) }}
          </span>
        </button>
      </nav>

      <!-- 右侧内容 -->
      <main class="content-area">
        <div v-if="visibleGroups.length === 0" class="empty-state">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="40" height="40" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="11" cy="11" r="8"/>
            <line x1="21" y1="21" x2="16.65" y2="16.65"/>
          </svg>
          <p>没有匹配「{{ searchQuery }}」的配置项</p>
        </div>

        <section v-for="group in visibleGroups" :key="group.name" class="group-section">
          <div class="section-head">
            <span class="section-icon" v-html="groupIcon(group.name)"></span>
            <h2 class="section-title">{{ group.name }}</h2>
          </div>

          <div class="field-grid">
            <div
                v-for="field in group.fields"
                :key="field.key"
                :class="['field-card', { 'is-modified': isModified(field), 'is-wide': field.type === 'string' }]">
              <div class="field-top">
                <label :for="'field-' + field.key" class="field-label">{{ field.label }}</label>
                <div class="field-tools">
                  <span v-if="isModified(field)" class="badge-dot" title="不是默认值"></span>
                  <button
                      v-if="isModified(field)"
                      class="btn-reset"
                      @click="resetField(field)"
                      :title="'恢复默认值：' + field.defaultValue">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="12" height="12" stroke-linecap="round" stroke-linejoin="round">
                      <polyline points="1 4 1 10 7 10"/>
                      <path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"/>
                    </svg>
                  </button>
                </div>
              </div>

              <!-- 布尔：开关 -->
              <div v-if="field.type === 'bool'" class="field-control">
                <label class="toggle-switch">
                  <input type="checkbox" v-model="formData[field.key]" :id="'field-' + field.key"/>
                  <span class="toggle-slider"></span>
                  <span :class="['toggle-label', { on: formData[field.key] }]">
                    {{ formData[field.key] ? '已启用' : '已禁用' }}
                  </span>
                </label>
              </div>

              <!-- 字符串：文本框 -->
              <div v-else-if="field.type === 'string'" class="field-control">
                <input
                    type="text"
                    :id="'field-' + field.key"
                    v-model="formData[field.key]"
                    class="input-text"
                    spellcheck="false"
                    :placeholder="String(field.defaultValue)"/>
              </div>

              <!-- 数字：数字框 + 单位 -->
              <div v-else class="field-control">
                <input
                    type="number"
                    :id="'field-' + field.key"
                    v-model.number="formData[field.key]"
                    class="input-number"
                    :placeholder="String(field.defaultValue)"/>
                <span v-if="fieldUnit(field)" class="field-unit">{{ fieldUnit(field) }}</span>
              </div>

              <p class="field-desc">{{ field.description }}</p>
              <p v-if="field.key === 'dialogTimeout'" class="field-desc field-warn">
                ⚠ 保留字段，暂未在运行时代码中使用
              </p>
            </div>
          </div>
        </section>
      </main>
    </div>

    <!-- ── 底部提示 ── -->
    <footer class="editor-footer">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="13" height="13" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="12" cy="12" r="10"/>
        <line x1="12" y1="16" x2="12" y2="12"/>
        <line x1="12" y1="8" x2="12.01" y2="8"/>
      </svg>
      <span>配置项在客户端启动时读取，改完点「保存并重启」即可立即生效；仅点「保存」则需下次启动后生效</span>
    </footer>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue';
import { GetConfigMeta, GetConfig, SaveConfig, SaveConfigAndRestart, HasActiveUpload, GetConfigPath, ShowDashboard } from '../../wailsjs/go/app/App';

const loading = ref(true);
const saving = ref(false);
const restarting = ref(false);
const statusMessage = ref('');
const statusType = ref('success');
const configPath = ref('');
const fieldsMeta = ref([]);
const formData = reactive({});
const originalData = reactive({});

// 当前选中的分组；搜索状态下忽略此项，直接展示所有命中结果
const activeGroup = ref('');
const searchQuery = ref('');

// ── 分组图标（内联 SVG，避免依赖字体/图标库；未知分组用齿轮兜底）──
const GROUP_ICONS = {
  '基础连接': '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg>',
  '后端地址': '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="2" width="20" height="8" rx="2" ry="2"/><rect x="2" y="14" width="20" height="8" rx="2" ry="2"/><line x1="6" y1="6" x2="6.01" y2="6"/><line x1="6" y1="18" x2="6.01" y2="18"/></svg>',
  '上传性能': '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg>',
  '超时配置': '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>',
  '功能开关': '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="4" y1="21" x2="4" y2="14"/><line x1="4" y1="10" x2="4" y2="3"/><line x1="12" y1="21" x2="12" y2="12"/><line x1="12" y1="8" x2="12" y2="3"/><line x1="20" y1="21" x2="20" y2="16"/><line x1="20" y1="12" x2="20" y2="3"/><line x1="1" y1="14" x2="7" y2="14"/><line x1="9" y1="8" x2="15" y2="8"/><line x1="17" y1="16" x2="23" y2="16"/></svg>',
  '链路追踪': '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="20" x2="18" y2="10"/><line x1="12" y1="20" x2="12" y2="4"/><line x1="6" y1="20" x2="6" y2="14"/></svg>',
};
const FALLBACK_ICON = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>';

function groupIcon(name) {
  return GROUP_ICONS[name] || FALLBACK_ICON;
}

// 数字类字段的单位后缀（chunkSize 换算成人类可读体积）
function fieldUnit(field) {
  switch (field.key) {
    case 'chunkSize':
      return '≈ ' + formatBytes(formData[field.key]);
    case 'httpTimeout':
    case 'dialogTimeout':
      return '秒';
    case 'traceMaxAgeDays':
      return '天';
    case 'traceVacuumThresholdMB':
      return 'MB';
    default:
      return '';
  }
}

// 按分组整理字段
const groupedFields = computed(() => {
  const groups = {};
  for (const field of fieldsMeta.value) {
    if (!groups[field.group]) {
      groups[field.group] = { name: field.group, order: field.groupOrder, fields: [] };
    }
    groups[field.group].fields.push(field);
  }
  return Object.values(groups).sort((a, b) => a.order - b.order);
});

// 右侧实际展示的分组：
//   有搜索词 → 跨分组过滤，展示所有命中的分组（空分组剔除）
//   无搜索词 → 只展示当前选中的分组
const visibleGroups = computed(() => {
  const q = searchQuery.value.trim().toLowerCase();
  if (!q) {
    return groupedFields.value.filter(g => g.name === activeGroup.value);
  }
  return groupedFields.value
      .map(g => ({
        ...g,
        fields: g.fields.filter(f =>
            (f.label || '').toLowerCase().includes(q) ||
            (f.key || '').toLowerCase().includes(q) ||
            (f.description || '').toLowerCase().includes(q)
        ),
      }))
      .filter(g => g.fields.length > 0);
});

// 未保存的改动数量（与已保存值比较）
const changedCount = computed(() =>
    Object.keys(formData).filter(k => formData[k] !== originalData[k]).length
);
const hasChanges = computed(() => changedCount.value > 0);

// 分组内"非默认值"的字段数，用于导航上的小圆点
function groupChangedCount(group) {
  return group.fields.filter(f => isModified(f)).length;
}

// 判断字段是否为非默认值
function isModified(field) {
  return formData[field.key] !== field.defaultValue;
}

function resetField(field) {
  formData[field.key] = field.defaultValue;
}

function resetAll() {
  for (const field of fieldsMeta.value) {
    formData[field.key] = field.defaultValue;
  }
  showStatus('已恢复所有配置为默认值（尚未保存）', 'info');
}

function selectGroup(name) {
  activeGroup.value = name;
  searchQuery.value = '';
}

// 返回上传面板
async function backToDashboard() {
  try {
    await ShowDashboard();
  } catch (err) {
    console.error('返回上传面板失败:', err);
  }
}

function formatBytes(bytes) {
  if (!bytes || bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB'];
  let i = 0;
  let val = bytes;
  while (val >= 1024 && i < units.length - 1) {
    val /= 1024;
    i++;
  }
  return val.toFixed(i > 0 ? 1 : 0) + ' ' + units[i];
}

function showStatus(msg, type = 'success') {
  statusMessage.value = msg;
  statusType.value = type;
  if (type === 'success' || type === 'info') {
    setTimeout(() => { statusMessage.value = ''; }, 4000);
  }
}

async function saveConfig() {
  saving.value = true;
  try {
    const configToSave = { ...formData };
    await SaveConfig(configToSave);
    Object.assign(originalData, configToSave);
    showStatus('配置保存成功，重新启动客户端后生效', 'success');
  } catch (err) {
    showStatus('保存失败: ' + (err.message || err), 'error');
  } finally {
    saving.value = false;
  }
}

// 保存并重启客户端：配置项大多在启动时读取，重启后才真正生效。
// 重启会中断正在进行的上传（未完成任务保留 manifest，重启后可续传），
// 因此如果检测到有活跃上传，先额外确认一次。
async function saveAndRestart() {
  let uploading = false;
  try {
    uploading = await HasActiveUpload();
  } catch (err) {
    console.warn('检查上传状态失败，按无活跃上传处理:', err);
  }

  const tip = uploading
      ? '当前有正在进行的上传任务，重启会中断上传（未完成的任务会保留，重启后可在面板中继续）。\n\n确认保存配置并重启客户端？'
      : '将保存配置并重新启动客户端，使新配置立即生效。\n\n确认继续？';
  if (!confirm(tip)) return;

  restarting.value = true;
  try {
    await SaveConfigAndRestart({ ...formData });
    Object.assign(originalData, { ...formData });
    showStatus('配置已保存，客户端正在重启...', 'success');
    // 进程随后会退出并由新实例接管，这里不再复位 restarting，
    // 保持按钮禁用与"正在重启"文案，避免用户重复点击。
  } catch (err) {
    restarting.value = false;
    showStatus('保存并重启失败: ' + (err.message || err), 'error');
  }
}

onMounted(async () => {
  try {
    const [meta, currentConfig, path] = await Promise.all([
      GetConfigMeta(),
      GetConfig(),
      GetConfigPath(),
    ]);

    fieldsMeta.value = meta;
    configPath.value = path;

    for (const field of meta) {
      const val = currentConfig[field.key];
      formData[field.key] = val !== undefined ? val : field.defaultValue;
      originalData[field.key] = formData[field.key];
    }

    // 默认选中第一个分组
    if (groupedFields.value.length > 0) {
      activeGroup.value = groupedFields.value[0].name;
    }
  } catch (err) {
    showStatus('加载配置失败: ' + (err.message || err), 'error');
  } finally {
    loading.value = false;
  }
});
</script>

<style scoped>
/* ── 设计变量 ── */
.config-editor {
  --bg: #f4f6fb;
  --surface: #ffffff;
  --border: #e4e9f2;
  --border-strong: #d3dbe8;
  --text: #16202e;
  --text-2: #64748b;
  --text-3: #94a3b8;
  --primary: #4f6ef7;
  --primary-hover: #3b5de7;
  --primary-light: #eef2ff;
  --success: #059669;
  --success-bg: #ecfdf5;
  --error: #dc2626;
  --error-bg: #fef2f2;
  --info: #0284c7;
  --info-bg: #f0f9ff;
  --warning: #d97706;
  --amber: #f59e0b;
  --radius: 10px;
  --shadow-sm: 0 1px 2px rgba(16, 32, 46, 0.05);
  --shadow-md: 0 4px 14px rgba(16, 32, 46, 0.07);

  display: flex;
  flex-direction: column;
  height: 100vh;
  background: var(--bg);
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  color: var(--text);
  overflow: hidden;
}

/* ── 顶部标题栏 ── */
.editor-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 12px 20px;
  background: var(--surface);
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
  --wails-draggable: drag;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}

.btn-back {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  flex-shrink: 0;
  border: 1px solid var(--border);
  border-radius: 9px;
  background: var(--surface);
  color: var(--text-2);
  cursor: pointer;
  transition: all 0.18s;
  --wails-draggable: no-drag;
}

.btn-back:hover {
  background: var(--primary-light);
  border-color: var(--primary);
  color: var(--primary);
  transform: translateX(-2px);
}

.header-titles {
  min-width: 0;
}

.header-title {
  margin: 0;
  font-size: 17px;
  font-weight: 700;
  letter-spacing: 0.2px;
}

.header-path {
  display: flex;
  align-items: center;
  gap: 4px;
  margin: 2px 0 0;
  font-size: 11px;
  color: var(--text-3);
  max-width: 420px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.header-path svg {
  flex-shrink: 0;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
  --wails-draggable: no-drag;
}

/* ── 搜索框 ── */
.search-box {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 10px;
  height: 34px;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 9px;
  color: var(--text-3);
  transition: all 0.18s;
}

.search-box:focus-within {
  background: var(--surface);
  border-color: var(--primary);
  box-shadow: 0 0 0 3px rgba(79, 110, 247, 0.1);
}

.search-box input {
  width: 132px;
  border: none;
  outline: none;
  background: transparent;
  font-size: 12.5px;
  color: var(--text);
}

.search-clear {
  border: none;
  background: none;
  color: var(--text-3);
  font-size: 16px;
  line-height: 1;
  cursor: pointer;
  padding: 0 2px;
}

.search-clear:hover {
  color: var(--text);
}

/* ── 按钮 ── */
.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 34px;
  padding: 0 14px;
  border-radius: 9px;
  font-size: 12.5px;
  font-weight: 600;
  cursor: pointer;
  border: none;
  transition: all 0.18s;
  white-space: nowrap;
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.btn-primary {
  background: var(--primary);
  color: #fff;
}

.btn-primary:hover:not(:disabled) {
  background: var(--primary-hover);
  box-shadow: 0 3px 10px rgba(79, 110, 247, 0.35);
}

.btn-outline {
  background: var(--surface);
  color: var(--text-2);
  border: 1px solid var(--border);
}

.btn-outline:hover:not(:disabled) {
  border-color: var(--primary);
  color: var(--primary);
  background: var(--primary-light);
}

/* ── 状态栏 ── */
.status-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 9px 20px;
  font-size: 12.5px;
  font-weight: 600;
  flex-shrink: 0;
}

.status-bar.success { background: var(--success-bg); color: var(--success); }
.status-bar.error   { background: var(--error-bg);   color: var(--error); }
.status-bar.info    { background: var(--info-bg);    color: var(--info); }

.status-close {
  background: none;
  border: none;
  font-size: 17px;
  cursor: pointer;
  color: inherit;
  opacity: 0.55;
  padding: 0 4px;
}

.status-close:hover { opacity: 1; }

.fade-enter-active, .fade-leave-active { transition: opacity 0.2s; }
.fade-enter-from, .fade-leave-to { opacity: 0; }

/* ── 加载 / 空状态 ── */
.loading-state, .empty-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 14px;
  color: var(--text-3);
  font-size: 13px;
}

.spinner {
  display: inline-block;
  width: 13px;
  height: 13px;
  border: 2px solid rgba(255, 255, 255, 0.35);
  border-top-color: #fff;
  border-radius: 50%;
  animation: spin 0.6s linear infinite;
}

/* 浅色底按钮里的 spinner 需要深色描边才看得见 */
.spinner.dark {
  border-color: rgba(100, 116, 139, 0.3);
  border-top-color: var(--text-2);
}

.spinner.large {
  width: 30px;
  height: 30px;
  border: 3px solid var(--border);
  border-top-color: var(--primary);
}

@keyframes spin { to { transform: rotate(360deg); } }

/* ── 主体布局 ── */
.editor-body {
  flex: 1;
  display: flex;
  min-height: 0;
}

/* ── 左侧图标导航 ── */
.side-nav {
  width: 208px;
  flex-shrink: 0;
  padding: 12px 10px;
  background: var(--surface);
  border-right: 1px solid var(--border);
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.nav-item {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 9px 10px;
  border: none;
  border-radius: 9px;
  background: transparent;
  color: var(--text-2);
  cursor: pointer;
  text-align: left;
  transition: all 0.16s;
  position: relative;
}

.nav-item:hover {
  background: var(--bg);
  color: var(--text);
}

.nav-item.active {
  background: var(--primary-light);
  color: var(--primary);
}

.nav-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  flex-shrink: 0;
  border-radius: 8px;
  background: var(--bg);
  color: var(--text-2);
  transition: all 0.16s;
}

.nav-item:hover .nav-icon { color: var(--text); }

.nav-item.active .nav-icon {
  background: var(--primary);
  color: #fff;
}

.nav-icon :deep(svg) {
  width: 16px;
  height: 16px;
}

.nav-text {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
  flex: 1;
}

.nav-name {
  font-size: 13px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.nav-meta {
  font-size: 10.5px;
  color: var(--text-3);
}

.nav-item.active .nav-meta { color: var(--primary); opacity: 0.75; }

.nav-dot {
  flex-shrink: 0;
  min-width: 17px;
  height: 17px;
  padding: 0 4px;
  border-radius: 9px;
  background: var(--amber);
  color: #fff;
  font-size: 10px;
  font-weight: 700;
  line-height: 17px;
  text-align: center;
}

/* ── 右侧内容 ── */
.content-area {
  flex: 1;
  overflow-y: auto;
  padding: 18px 22px 24px;
  min-width: 0;
}

.content-area::-webkit-scrollbar,
.side-nav::-webkit-scrollbar { width: 6px; }

.content-area::-webkit-scrollbar-thumb,
.side-nav::-webkit-scrollbar-thumb {
  background: #cbd5e1;
  border-radius: 3px;
}

.content-area::-webkit-scrollbar-thumb:hover,
.side-nav::-webkit-scrollbar-thumb:hover { background: #94a3b8; }

.group-section { margin-bottom: 26px; }

.section-head {
  display: flex;
  align-items: center;
  gap: 9px;
  margin-bottom: 12px;
}

.section-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border-radius: 7px;
  background: var(--primary-light);
  color: var(--primary);
}

.section-icon :deep(svg) { width: 14px; height: 14px; }

.section-title {
  margin: 0;
  font-size: 14.5px;
  font-weight: 700;
}

/* ── 配置卡片网格 ── */
.field-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(292px, 1fr));
  gap: 12px;
}

.field-card {
  position: relative;
  padding: 14px 15px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  box-shadow: var(--shadow-sm);
  transition: border-color 0.18s, box-shadow 0.18s, transform 0.18s;
}

.field-card:hover {
  border-color: var(--border-strong);
  box-shadow: var(--shadow-md);
  transform: translateY(-1px);
}

/* 非默认值：左侧竖条 + 琥珀色描边，一眼看出改过哪些 */
.field-card.is-modified {
  border-color: #fcd9a0;
  background: linear-gradient(90deg, #fffbf2 0%, var(--surface) 60%);
}

.field-card.is-modified::before {
  content: '';
  position: absolute;
  top: 12px;
  bottom: 12px;
  left: 0;
  width: 3px;
  border-radius: 0 3px 3px 0;
  background: var(--amber);
}

/* 字符串项通常较长（URL / 路径），占满整行更好读 */
.field-card.is-wide { grid-column: 1 / -1; }

.field-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 9px;
}

.field-label {
  font-size: 12.5px;
  font-weight: 700;
  color: var(--text);
}

.field-tools {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}

.badge-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--amber);
}

.btn-reset {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 21px;
  height: 21px;
  padding: 0;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--surface);
  color: var(--text-3);
  cursor: pointer;
  transition: all 0.15s;
}

.btn-reset:hover {
  border-color: var(--primary);
  color: var(--primary);
  background: var(--primary-light);
  transform: rotate(-45deg);
}

/* ── 控件 ── */
.field-control {
  display: flex;
  align-items: center;
  gap: 8px;
}

.input-text, .input-number {
  width: 100%;
  padding: 8px 11px;
  border: 1px solid var(--border);
  border-radius: 8px;
  font-size: 12.5px;
  color: var(--text);
  background: var(--bg);
  transition: all 0.18s;
  outline: none;
  box-sizing: border-box;
}

.input-text:focus, .input-number:focus {
  background: var(--surface);
  border-color: var(--primary);
  box-shadow: 0 0 0 3px rgba(79, 110, 247, 0.12);
}

.input-number { max-width: 130px; }

.field-unit {
  font-size: 11.5px;
  color: var(--text-3);
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}

.field-desc {
  margin: 9px 0 0;
  font-size: 11.5px;
  color: var(--text-3);
  line-height: 1.55;
}

.field-warn { color: var(--warning); }

/* ── 开关 ── */
.toggle-switch {
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  user-select: none;
}

.toggle-switch input { display: none; }

.toggle-slider {
  position: relative;
  width: 38px;
  height: 21px;
  background: #cbd5e1;
  border-radius: 11px;
  transition: background 0.24s;
  flex-shrink: 0;
}

.toggle-slider::after {
  content: '';
  position: absolute;
  top: 3px;
  left: 3px;
  width: 15px;
  height: 15px;
  background: #fff;
  border-radius: 50%;
  transition: transform 0.24s;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.22);
}

.toggle-switch input:checked + .toggle-slider { background: var(--primary); }
.toggle-switch input:checked + .toggle-slider::after { transform: translateX(17px); }

.toggle-label {
  font-size: 11.5px;
  font-weight: 600;
  color: var(--text-3);
}

.toggle-label.on { color: var(--primary); }

/* ── 底部提示 ── */
.editor-footer {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 9px 20px;
  background: var(--surface);
  border-top: 1px solid var(--border);
  flex-shrink: 0;
  font-size: 11.5px;
  color: var(--text-3);
}

.editor-footer svg { flex-shrink: 0; }
</style>

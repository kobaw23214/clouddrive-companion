<template>
  <div class="file-selector">
    <header class="header">
      <div class="navigation">
        <button @click="goUp" :disabled="currentPath === '/'" class="nav-button">
          ↑ 上一级
        </button>
        <button @click="goToRoot" class="nav-button">
          🖥️ 此电脑
        </button>
        <button @click="goToDesktop" class="nav-button">
          🏠 桌面
        </button>
        <button @click="toggleViewMode" class="nav-button view-toggle" title="切换视图">
          {{ viewMode === 'list' ? '🔲' : '☰' }}
        </button>
        <button
            @click="refreshCurrentDirectory"
            class="nav-button refresh-button"
            :class="{ spinning: isLoading }"
            :disabled="isLoading"
            title="刷新当前目录"
        >
          ⟳
        </button>
      </div>
      <div class="current-path-container">
        <span class="path-label">当前路径:</span>
        <div class="path-input-wrapper">
          <input
              ref="pathInputRef"
              type="text"
              v-model="pathInput"
              class="path-input"
              :placeholder="currentPath === '/' ? '此电脑 (输入路径并回车跳转)' : '输入路径并回车跳转'"
              spellcheck="false"
              autocomplete="off"
              @focus="onPathFocus"
              @blur="onPathBlur"
              @input="onPathInput"
              @keydown.down.prevent="moveActive(1)"
              @keydown.up.prevent="moveActive(-1)"
              @keydown.enter.prevent="onPathEnter"
              @keydown.esc="showSuggestions = false"
          />
          <ul
              v-if="showSuggestions && suggestions.length > 0"
              class="path-suggestions"
              @mousedown.prevent
          >
            <li
                v-for="(item, index) in suggestions"
                :key="item.path"
                :class="['suggestion-item', { active: index === activeSuggestionIndex }]"
                @mouseenter="activeSuggestionIndex = index"
                @click="applySuggestion(item)"
            >
              <span class="suggestion-icon">📁</span>
              <span class="suggestion-name">{{ item.name }}</span>
              <span class="suggestion-path">{{ item.path }}</span>
            </li>
          </ul>
        </div>
        <div class="filter-wrapper">
          <input
              type="text"
              v-model="nameFilter"
              class="filter-input"
              placeholder="筛选当前目录"
              spellcheck="false"
              autocomplete="off"
              @keydown.esc="nameFilter = ''"
          />
          <button
              v-if="nameFilter"
              class="filter-clear"
              title="清空筛选"
              @click="nameFilter = ''"
          >&times;</button>
        </div>
      </div>
      <div class="list-status-bar">
        <span class="status-count">{{ filterCountLabel }}</span>
        <template v-if="listLoadedAt">
          <span class="status-sep">·</span>
          <span class="status-time" :class="{ stale: isListStale }">
            读取于 {{ listLoadedAtLabel }}（{{ listAgeLabel }}）
          </span>
          <span class="status-sep">·</span>
          <span class="status-cost">耗时 {{ listLoadCost }} ms</span>
          <span v-if="isListStale" class="status-hint">
            列表不会自动更新，内容可能已变化 —— 可点右上角 ⟳ 重新读取
          </span>
        </template>
      </div>
    </header>

    <main
        class="file-list-container"
        :class="[viewMode === 'grid' ? 'grid-view' : 'list-view']"
    >
      <header v-if="viewMode === 'list'" class="file-list-header">
        <div class="header-cell checkbox-cell">
          <input
              type="checkbox"
              title="全选/全不选"
              :checked="selectAllState.checked"
              :indeterminate.prop="selectAllState.indeterminate"
              @click="toggleSelectAll"
          />
        </div>
        <div class="header-cell name-cell" @click="setSort('name')">
          文件名
          <span v-if="sortKey === 'name'" class="sort-arrow">{{ sortDirection === 'asc' ? '▲' : '▼' }}</span>
        </div>
        <div class="header-cell date-cell" @click="setSort('date')">
          修改日期
          <span v-if="sortKey === 'date'" class="sort-arrow">{{ sortDirection === 'asc' ? '▲' : '▼' }}</span>
        </div>
        <div class="header-cell type-cell" @click="setSort('type')">
          类型
          <span v-if="sortKey === 'type'" class="sort-arrow">{{ sortDirection === 'asc' ? '▲' : '▼' }}</span>
        </div>
        <div class="header-cell size-cell" @click="setSort('size')">
          大小
          <span v-if="sortKey === 'size'" class="sort-arrow">{{ sortDirection === 'asc' ? '▲' : '▼' }}</span>
        </div>
      </header>

      <div :class="viewMode === 'list' ? 'list-scroll-container' : 'grid-scroll-container'">
        <div v-if="isLoading" class="loading">
          正在加载...
        </div>
        <div v-if="!isLoading && sortedFiles.length === 0" class="empty-list">
          <template v-if="nameFilter.trim() && files.length > 0">
            没有匹配「{{ nameFilter }}」的项目（当前目录共 {{ files.length }} 项）
          </template>
          <template v-else>此文件夹为空</template>
        </div>

<!--        <div-->
<!--            v-for="item in sortedFiles"-->
<!--            :key="item.path"-->
<!--            @click="toggleSelection(item, $event)"-->
<!--            @dblclick="navigate(item)"-->
<!--            :class="['file-item', { selected: selectedPaths.has(item.path) }]"-->
<!--        >-->
        <div
            v-for="item in sortedFiles"
            :key="item.path"
            @dblclick="navigate(item)"
            :class="['file-item', { selected: selectedPaths.has(item.path) }]"
        >
          <template v-if="viewMode === 'list'">
            <input
                type="checkbox"
                :checked="selectedPaths.has(item.path)"
                class="checkbox"
                @click.stop="toggleSelection(item, $event)"
            />
            <div class="file-info" :title="item.name"> <span class="file-icon">{{ item.isDir ? '📁' : '📄' }}</span>
              <span class="file-name">{{ item.name }}</span>
            </div>
            <span class="file-modified">{{ formatTimestamp(item.modifiedTime) }}</span>
            <span class="file-type">{{ getFileType(item) }}</span>
            <span class="file-size">
              {{ item.isDir ? '--' : formatSize(item.size) }}
            </span>
          </template>

          <template v-if="viewMode === 'grid'">
            <div class="file-info">
              <input
                  type="checkbox"
                  :checked="selectedPaths.has(item.path)"
                  class="checkbox"
                  @click.stop="toggleSelection(item, $event)"
              />
              <span class="file-icon">{{ item.isDir ? '📁' : '📄' }}</span>
              <span class="file-name" :title="item.name">{{ item.name }}</span>
            </div>
          </template>

        </div>
      </div>
    </main>

    <div v-if="preselectNotice" class="preselect-notice">
      <span class="notice-text">
        已从浏览器恢复上次选择：{{ preselectNotice.dirs }} 个文件夹、{{ preselectNotice.files }} 个文件
        <template v-if="preselectNotice.missing.length > 0">
          ，<span class="missing-count">{{ preselectNotice.missing.length }} 个文件已不存在</span>
          <a href="javascript:;" class="toggle-missing" @click="showMissingList = !showMissingList">
            {{ showMissingList ? '收起' : '查看' }}
          </a>
        </template>
      </span>
      <button class="notice-close" @click="preselectNotice = null" title="关闭提示">&times;</button>
      <div v-if="showMissingList" class="missing-list">
        <div v-for="m in preselectNotice.missing" :key="m" class="missing-item">{{ m }}</div>
      </div>
    </div>

    <div class="drop-zone-prompt" :class="{ 'has-selection': selectedItems.length > 0 }">
      <template v-if="selectedItems.length === 0">
        当前未选择任何文件
      </template>
      <div v-else class="selected-files-list">
        <span v-for="item in selectedItems" :key="item.path" class="selected-file-chip">
          <span class="chip-icon">{{ item.isDir ? '📁' : '📓' }}</span>
          <span class="chip-path" :title="item.path">{{ item.path }}</span>
          <span v-if="dirFileCounts.has(item.path)" class="chip-count">
            共 {{ dirFileCounts.get(item.path) }} 个文件
          </span>

          <button
              class="chip-remove-btn"
              @click.stop="removeSelectedItem(item.path)"
              title="移除"
          >
            &times; </button>

        </span>
      </div>
    </div>

    <footer class="footer">
      <div class="selection-info">
        已选择 {{ selectedPaths.size }} 个项目
      </div>
      <div class="actions">
        <button @click="cancel" class="button cancel">取消</button>
        <button @click="submit" class="button submit" :disabled="selectedPaths.size === 0">
          确认选择
        </button>
      </div>
    </footer>

    <ScanProgressOverlay :phase="scanPhase" @close="handleScanError" />
  </div>
</template>

<script setup>
import {computed, onMounted, onUnmounted, ref} from 'vue';
import {CancelSelection, GetDesktopPath, GetPendingPreselect, ListDirectory, SubmitSelection} from 'wailsjs/go/app/App';
import {EventsOn} from 'wailsjs/runtime';
import ScanProgressOverlay from './ScanProgressOverlay.vue';
// 视图模式与排序是「用户偏好」，放在模块作用域以跨组件重挂载存活。
// 本组件每次被唤起都会整体重挂（见 App.vue 的 selectorKey），组件内 ref 会
// 全部打回初始值——这对会话状态是期望行为，对用户偏好则不是。
import {sortDirection, sortKey, viewMode} from './fileSelectorPrefs.js';

const scanPhase = ref(null);

const isLoading = ref(false);
const currentPath = ref('/');
const files = ref([]);

// 路径输入框 / 自动补全相关状态
const pathInputRef = ref(null);
const pathInput = ref('');
const suggestions = ref([]);
const showSuggestions = ref(false);
const activeSuggestionIndex = ref(-1);
let suggestTimer = null;

// ── 列表读取时刻与新鲜度 ──
//
// 为什么不做实时监听：本地 NTFS 上用 ReadDirectoryChangesW / fsnotify 是可行的，
// 但在现场真正出问题的那几类盘上恰好不可靠 —— SMB 映射盘的变更通知会延迟或丢失，
// rclone / WinFsp / 网盘挂载这类虚拟文件系统大多没有实现变更通知，监听了也不会触发。
// 与其在关键场景上做一个"看起来实时、实际不动"的假象，不如明确暴露这份列表的
// 读取时刻和已过去多久，让用户自己决定要不要刷新。
const listLoadedAt = ref(null); // 本次列表读取完成的时刻（毫秒时间戳）
const listLoadCost = ref(0);    // 本次读取耗时（毫秒，含 Wails IPC 往返）
const nowTick = ref(Date.now()); // 每秒推进一次，驱动「N 秒前」这类相对时间重算
let ageTimer = null;

// 超过这个秒数就在界面上提示「内容可能已变化」
const STALE_AFTER_SECONDS = 60;

// 目录加载的「代号」：每次发起自增，响应回来时若已不是最新一代就丢弃结果。
//
// 取代了原先 loadDirectory 开头的 `if (isLoading.value) return` 守卫——那个守卫
// 会把新会话的强制刷新静默吞掉：上一个加载还在飞（大目录 / 慢盘 / 网络盘）时
// 直接 return，界面就继续停留在旧目录。改成代号后允许并发发起、只保留最新一次
// 的结果，既不丢刷新也不会让过期响应覆盖新目录。
let loadGeneration = 0;

// 当前目录的名字筛选关键字。
// 选择器原先没有任何筛选手段，目录里几百上千条时只能靠肉眼滚动，
// 「明明在系统里能看到、这里找不到」有相当一部分其实是没翻到。
// 切换目录时清空，刷新同一目录时保留（见 loadDirectory）。
const nameFilter = ref('');

// 混合选择的 Map
const selectedPaths = ref(new Map());

// ── 预选恢复（浏览器"追加"带回上次已选列表）相关状态 ──
// dirFileCounts: 恢复出的目录 → 目录下文件数，仅用于已选 chip 的数量标注
const dirFileCounts = ref(new Map());
// preselectNotice: 恢复结果提示 {dirs, files, missing: []}；null 表示无提示
const preselectNotice = ref(null);
const showMissingList = ref(false);

// 计算显示的路径
const currentPathDisplay = computed(() => (currentPath.value === '/' ? '此电脑' : currentPath.value));



// 混合选择的 Items
const selectedItems = computed(() => {
  return Array.from(selectedPaths.value.values());
});

// 模拟获取文件类型
function getFileType(item) {
  if (item.isDir) {
    return '文件夹';
  }
  const parts = item.name.split('.');
  if (parts.length > 1) {
    return parts.pop().toUpperCase() + ' 文件';
  }
  return '文件';
}

/**
 * 格式化修改日期
 */
function formatTimestamp(timestamp) {
  if (!timestamp || timestamp === 0) {
    return '--';
  }
  try {
    const date = new Date(timestamp);
    // 格式化为 YYYY/MM/DD HH:mm
    const Y = date.getFullYear();
    const M = (date.getMonth() + 1).toString().padStart(2, '0');
    const D = date.getDate().toString().padStart(2, '0');
    const h = date.getHours().toString().padStart(2, '0');
    const m = date.getMinutes().toString().padStart(2, '0');
    return `${Y}/${M}/${D} ${h}:${m}`;
  } catch (e) {
    console.error("日期格式化失败:", e);
    return '--';
  }
}

// 排序后的文件列表（先按关键字筛选，再排序）
const sortedFiles = computed(() => {
  if (!files.value) return [];

  const key = sortKey.value;
  const dir = sortDirection.value;
  const dirMultiplier = dir === 'asc' ? 1 : -1;

  // 0. 按名字筛选（不区分大小写的子串匹配）
  const keyword = nameFilter.value.trim().toLowerCase();
  const source = keyword
      ? files.value.filter(f => f.name.toLowerCase().includes(keyword))
      : files.value;

  // 1. 分离文件夹和文件
  const dirs = source.filter(f => f.isDir);
  const regularFiles = source.filter(f => !f.isDir);

  // 2. 定义排序函数
  const sorter = (a, b) => {
    let valA, valB;
    switch (key) {
      case 'name':
        valA = a.name.toLowerCase();
        valB = b.name.toLowerCase();
        return valA.localeCompare(valB) * dirMultiplier;
      case 'size':
        valA = a.size;
        valB = b.size;
        return (valA - valB) * dirMultiplier;
      case 'type':
        valA = getFileType(a);
        valB = getFileType(b);
        return valA.localeCompare(valB) * dirMultiplier;
      case 'date':
        valA = a.modifiedTime || 0;
        valB = b.modifiedTime || 0;
        return (valA - valB) * dirMultiplier;
      default:
        return a.name.localeCompare(b.name) * dirMultiplier;
    }
  };

  // 3. 排序 (文件夹从不按大小排序)
  if (key === 'size') {
    dirs.sort((a, b) => a.name.localeCompare(b.name) * (sortDirection.value === 'asc' ? 1 : -1) );
  } else {
    dirs.sort(sorter);
  }
  regularFiles.sort(sorter);

  // 4. 始终保持文件夹在文件前面
  return [...dirs, ...regularFiles];
});

// 条目计数提示：让用户能确认「这个目录客户端确实读到了 N 项」，
// 而不是只能凭「我没看到」下结论。筛选生效时同时显示筛选后/总数。
const filterCountLabel = computed(() => {
  const total = files.value ? files.value.length : 0;
  if (!nameFilter.value.trim()) return `共 ${total} 项`;
  return `筛选 ${sortedFiles.value.length} / 共 ${total} 项`;
});

// 读取时刻的绝对时间（HH:mm:ss）
const listLoadedAtLabel = computed(() => {
  if (!listLoadedAt.value) return '';
  const d = new Date(listLoadedAt.value);
  const p = n => n.toString().padStart(2, '0');
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
});

// 距今多少秒。依赖 nowTick，所以每秒会自动重算
const listAgeSeconds = computed(() => {
  if (!listLoadedAt.value) return 0;
  return Math.max(0, Math.floor((nowTick.value - listLoadedAt.value) / 1000));
});

const listAgeLabel = computed(() => {
  const s = listAgeSeconds.value;
  if (s < 5) return '刚刚';
  if (s < 60) return `${s} 秒前`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} 分钟前`;
  return `${Math.floor(m / 60)} 小时前`;
});

const isListStale = computed(() => listAgeSeconds.value >= STALE_AFTER_SECONDS);

//  计算全选复选框的状态
// 这个计算逻辑是正确的，它只检查当前可见的项
const selectAllState = computed(() => {
  const visibleItems = sortedFiles.value;
  if (visibleItems.length === 0) {
    return { checked: false, indeterminate: false };
  }

  const selectedCount = visibleItems.filter(item => selectedPaths.value.has(item.path)).length;

  if (selectedCount === 0) {
    return { checked: false, indeterminate: false };
  }

  if (selectedCount === visibleItems.length) {
    return { checked: true, indeterminate: false };
  }

  return { checked: false, indeterminate: true }; // 部分选中
});

/**
 *  切换全选/全不选
 * 这个函数依赖 toggleSelection，只要 toggleSelection 被修复，这里就能正常工作
 */
function toggleSelectAll() {
  const { checked } = selectAllState.value; // 获取当前状态

  if (checked) {
    // 当前是全选 -> 取消选择所有可见项
    sortedFiles.value.forEach(item => {
      if (selectedPaths.value.has(item.path)) {
        // [!! 优化 !!] 直接调用 removeSelectedItem 更清晰
        removeSelectedItem(item.path);
      }
    });
  } else {
    // 当前是部分选中或全不选 -> 选择所有可见项
    sortedFiles.value.forEach(item => {
      if (!selectedPaths.value.has(item.path)) {
        toggleSelection(item); // 复用 toggleSelection
      }
    });
  }
}

/**
 * 设置排序
 */
function setSort(key) {
  if (sortKey.value === key) {
    sortDirection.value = sortDirection.value === 'asc' ? 'desc' : 'asc';
  } else {
    sortKey.value = key;
    sortDirection.value = 'asc';
  }
}

/**
 * 切换视图模式
 */
function toggleViewMode() {
  viewMode.value = viewMode.value === 'list' ? 'grid' : 'list';
}

/**
 * 从底部的芯片中移除一个已选项
 */
function removeSelectedItem(path) {
  if (selectedPaths.value.has(path)) {
    selectedPaths.value.delete(path);
    dirFileCounts.value.delete(path);
  }
}

/**
 * 规范化用户输入的路径（统一分隔符为反斜杠）
 */
function normalizePath(input) {
  return input.replace(/\//g, '\\');
}

/**
 * 把驱动器盘符补全为带分隔符的形式 (C: -> C:\)
 */
function ensureDriveSep(p) {
  if (/^[A-Za-z]:$/.test(p)) {
    return p + '\\';
  }
  return p;
}

/**
 * 路径输入框获得焦点
 */
function onPathFocus() {
  updateSuggestions();
}

/**
 * 路径输入框失焦（延迟关闭，留出点击建议项的时间）
 */
function onPathBlur() {
  // mousedown.prevent 已经阻止失焦，这里仍延迟关闭兜底
  setTimeout(() => {
    showSuggestions.value = false;
  }, 150);
}

/**
 * 输入时触发（防抖更新建议列表）
 */
function onPathInput() {
  if (suggestTimer) clearTimeout(suggestTimer);
  suggestTimer = setTimeout(updateSuggestions, 150);
}

/**
 * 根据当前输入计算路径提示列表
 */
async function updateSuggestions() {
  const raw = normalizePath(pathInput.value || '');

  // 空输入 -> 列出此电脑（盘符 + 桌面）
  if (raw.trim() === '') {
    try {
      const result = await ListDirectory('/');
      suggestions.value = result.filter(f => f.isDir);
      activeSuggestionIndex.value = -1;
      showSuggestions.value = suggestions.value.length > 0;
    } catch (e) {
      suggestions.value = [];
      showSuggestions.value = false;
    }
    return;
  }

  // 拆分出"父目录"和"正在输入的片段"
  let dirPart;
  let prefix;
  if (raw.endsWith('\\')) {
    dirPart = raw;
    prefix = '';
  } else {
    const idx = raw.lastIndexOf('\\');
    if (idx === -1) {
      // 形如 "C" 或 "C:" —— 用盘符列表做提示
      dirPart = '/';
      prefix = raw;
    } else {
      dirPart = raw.substring(0, idx + 1);
      prefix = raw.substring(idx + 1);
    }
  }

  const listTarget = dirPart === '/' ? '/' : ensureDriveSep(dirPart);

  try {
    const result = await ListDirectory(listTarget);
    const lowerPrefix = prefix.toLowerCase();
    suggestions.value = result
        .filter(f => f.isDir)
        .filter(f => f.name.toLowerCase().startsWith(lowerPrefix))
        .slice(0, 50);
    activeSuggestionIndex.value = -1;
    showSuggestions.value = suggestions.value.length > 0;
  } catch (e) {
    // 目录不存在或无权限，不显示建议
    suggestions.value = [];
    showSuggestions.value = false;
  }
}

/**
 * 键盘上下移动高亮项
 */
function moveActive(delta) {
  if (!showSuggestions.value || suggestions.value.length === 0) return;
  const len = suggestions.value.length;
  let next = activeSuggestionIndex.value + delta;
  if (next < 0) next = len - 1;
  if (next >= len) next = 0;
  activeSuggestionIndex.value = next;
}

/**
 * 选中一个建议项 -> 直接进入该目录
 */
function applySuggestion(item) {
  pathInput.value = item.path;
  showSuggestions.value = false;
  loadDirectory(item.path);
}

/**
 * 回车：若有高亮建议则补全进入，否则尝试跳转到输入的路径
 */
function onPathEnter() {
  if (showSuggestions.value && activeSuggestionIndex.value >= 0) {
    applySuggestion(suggestions.value[activeSuggestionIndex.value]);
    return;
  }
  navigateToInputPath();
}

/**
 * 跳转到输入框中的路径
 */
async function navigateToInputPath() {
  const raw = normalizePath(pathInput.value || '').trim();
  showSuggestions.value = false;

  if (raw === '') {
    goToRoot();
    return;
  }

  let target = ensureDriveSep(raw);
  // 目录路径补全结尾分隔符，便于 goUp 等逻辑一致
  if (!target.endsWith('\\')) {
    target += '\\';
  }

  // 与 loadDirectory 共用同一套代号，避免两条路径的响应互相覆盖
  const gen = ++loadGeneration;
  const startedAt = Date.now();
  try {
    // 直接尝试加载，ListDirectory 失败说明路径无效
    const result = await ListDirectory(target);
    if (gen !== loadGeneration) return;
    files.value = result;
    markListLoaded(startedAt);
    nameFilter.value = '';
    currentPath.value = target;
    pathInput.value = target;
  } catch (err) {
    if (gen !== loadGeneration) return;
    alert('无法打开路径: ' + target + '\n' + err);
    // 还原为当前目录
    pathInput.value = currentPath.value === '/' ? '' : currentPath.value;
  }
}

/**
 * 记录本次列表读取的完成时刻与耗时。
 * 时间在前端取，因此耗时含 Wails IPC 往返——这正是用户实际等待的时间。
 */
function markListLoaded(startedAt) {
  listLoadedAt.value = Date.now();
  listLoadCost.value = listLoadedAt.value - startedAt;
  nowTick.value = listLoadedAt.value; // 立刻让「刚刚」生效，不等下一次 tick
}

/**
 * 加载目录内容
 */
async function loadDirectory(path) {
  const gen = ++loadGeneration;
  // 换目录才清筛选关键字；刷新当前目录时保留，否则用户一边筛一边刷会被反复清空
  const pathChanged = currentPath.value !== path;
  const startedAt = Date.now();
  isLoading.value = true;

  try {
    const result = await ListDirectory(path);
    // 已被更新的加载请求取代（或组件已卸载），丢弃本次结果，避免旧目录覆盖新目录
    if (gen !== loadGeneration) return;
    files.value = result;
    markListLoaded(startedAt);
    if (pathChanged) nameFilter.value = '';
    currentPath.value = path;
    // 同步路径输入框显示（根目录显示为空，由 placeholder 提示）
    pathInput.value = path === '/' ? '' : path;
    showSuggestions.value = false;
  } catch (err) {
    if (gen !== loadGeneration) return;
    console.error("加载目录失败:", err);
    alert("加载目录失败: " + err);
  } finally {
    // 只有最新一代才有资格收掉 loading 态，否则过期响应会提前解除转圈
    if (gen === loadGeneration) isLoading.value = false;
  }
}

/**
 * 导航到根目录（磁盘驱动器）
 */
function goToRoot() {
  loadDirectory('/');
}

/**
 * 刷新当前目录：重新读一次磁盘，保留当前所在路径和已勾选项。
 *
 * 与「新会话自动刷新」（组件重挂）互补：这是用户在选择器里手动改动了磁盘
 * （新建/删除文件）后想立刻看到变化时用的，不该丢掉已有的选择。
 */
function refreshCurrentDirectory() {
  loadDirectory(currentPath.value);
}

/**
 * 导航到桌面
 */
async function goToDesktop() {
  try {
    const desktopPath = await GetDesktopPath();
    if (desktopPath) {
      loadDirectory(desktopPath);
    } else {
      alert("未能找到桌面路径。");
    }
  } catch (err) {
    console.error("获取桌面路径失败:", err);
    alert("获取桌面路径失败: " + err);
  }
}


/**
 * 导航到上一级
 */
function goUp() {
  if (currentPath.value === '/') return;

  let parentPath = currentPath.value.substring(0, currentPath.value.length - 1);
  let lastSep = parentPath.lastIndexOf('\\');

  if (lastSep === -1) {
    goToRoot();
  } else if (lastSep === parentPath.length - 1) {
    goToRoot();
  } else {
    parentPath = parentPath.substring(0, lastSep + 1);
    loadDirectory(parentPath);
  }
}

/**
 * 导航到子目录（双击时）
 */
function navigate(item) {
  if (item.isDir) {
    loadDirectory(item.path);
  }
}

/**
 * [!! 关键修改 !!] 混合选择的逻辑
 */
function toggleSelection(item, event) {
  const newPath = item.path;
  const currentPathsMap = selectedPaths.value;

  // --- Case 1: 取消选择 ---
  if (currentPathsMap.has(newPath)) {
    currentPathsMap.delete(newPath);
    return;
  }

  // --- Case 2: 添加选择 ---
  const currentPaths = Array.from(currentPathsMap.keys());

  // [!! 已移除 !!] 2a. 检查是否有 *父级* 目录已被选中
  //
  // 这段代码是导致 "全选" 失效的根源。
  // 它阻止了子项在父项被选中的情况下被选中。
  /*
  const isAncestorSelected = currentPaths.some(p =>
      newPath.startsWith(p) && // newPath 是 p 的子孙
      newPath.length > p.length   // 确保不是同一路径
  );

  if (isAncestorSelected) {
    console.log("父目录已被选中，忽略此子项。");
    return;
  }
  */
  // [!! 移除结束 !!]


  // 2b. 检查并移除所有 *子级* 目录/文件
  // 这段逻辑是正确的，必须保留。
  // 当选中一个父目录时，自动移除所有已选中的子项。
  if (item.isDir) {
    const descendants = currentPaths.filter(p =>
        p.startsWith(newPath) && // p 是 newPath 的子孙
        p.length > newPath.length    // 确保不是同一路径
    );

    descendants.forEach(p => {
      currentPathsMap.delete(p);
      console.log("移除子项: ", p);
    });
  }

  // 2c. 添加新选择
  currentPathsMap.set(newPath, {path: newPath, isDir: item.isDir, name: item.name});
}


/**
 * 提交选择
 */
async function submit() {
  try {
    await SubmitSelection(Array.from(selectedPaths.value.keys()));
    scanPhase.value = 'scanning';
  } catch (err) {
    console.error("提交失败:", err);
    alert("提交失败: " + err);
  }
}

/**
 * 预选恢复：浏览器"追加"场景把上次已确认的文件列表 POST 给 Go 端，
 * Go 端折叠成"目录 + 散文件"后存起来，这里在选择器打开时主动拉取（消费式，
 * 每次打开只恢复一次），并入当前选择。
 *
 * 合并规则与手动勾选保持一致：
 *  - 恢复目录时移除已选中的子孙项（避免提交后重复扫描）；
 *  - 恢复文件时若已有祖先目录被选中则跳过（该文件已被目录选择覆盖）。
 */
async function restorePreselect() {
  let pre;
  try {
    pre = await GetPendingPreselect();
  } catch (err) {
    console.error("拉取预选列表失败:", err);
    return;
  }
  if (!pre) return;

  // Go 端返回 slash 路径；选择器内部与 ListDirectory 一致使用反斜杠、目录带尾部分隔符
  const toLocal = p => p.replace(/\//g, '\\');
  const baseName = p => {
    const segs = p.split('/').filter(s => s.length > 0);
    return segs.length ? segs[segs.length - 1] : p;
  };

  for (const d of (pre.dirs || [])) {
    const dirPath = toLocal(d.path) + '\\';
    for (const p of Array.from(selectedPaths.value.keys())) {
      if (p !== dirPath && p.startsWith(dirPath)) {
        selectedPaths.value.delete(p);
        dirFileCounts.value.delete(p);
      }
    }
    selectedPaths.value.set(dirPath, {path: dirPath, isDir: true, name: baseName(d.path)});
    dirFileCounts.value.set(dirPath, d.fileCount);
  }
  for (const f of (pre.files || [])) {
    const filePath = toLocal(f);
    let covered = false;
    for (const p of selectedPaths.value.keys()) {
      // 目录路径带尾部 '\'，前缀比较安全（"E:\a\b\" 不会误伤 "E:\a\b2\..."）
      if (p !== filePath && filePath.startsWith(p)) {
        covered = true;
        break;
      }
    }
    if (covered) continue;
    selectedPaths.value.set(filePath, {path: filePath, isDir: false, name: baseName(f)});
  }

  preselectNotice.value = {
    dirs: (pre.dirs || []).length,
    files: (pre.files || []).length,
    missing: pre.missing || [],
  };
  showMissingList.value = false;
}

/** 清空预选恢复的痕迹（提交/取消/重新打开时调用） */
function clearPreselectState() {
  preselectNotice.value = null;
  showMissingList.value = false;
  dirFileCounts.value.clear();
}

function handleScanError() {
  scanPhase.value = null;
}

/**
 * 取消选择
 */
async function cancel() {
  try {
    await CancelSelection();
    selectedPaths.value.clear();
    clearPreselectState();
    goToRoot();
  } catch (err) {
    console.error("取消失败:", err);
  }
}

/**
 * 格式化文件大小
 */
function formatSize(bytes) {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

// EventsOn 返回「取消该监听器」的函数，集中收集后在 onUnmounted 里逐个调用。
// 本组件现在会随每次唤起重新挂载（App.vue 的 selectorKey），不注销就会
// 每开一次叠加一组监听器，一个事件被处理 N 次。
let eventCleanups = [];

onMounted(() => {
  // 本组件每次被唤起都是全新实例（App.vue 用递增 :key 强制重挂载），
  // 因此这里不需要、也不应该手工复位任何状态：当前路径、已选项、扫描遮罩、
  // 路径补全、预选提示条全都已是初始值。只做「拉数据」这一件事。
  loadDirectory('/');
  // 预选恢复（"追加"）：Go 端在发出 view:show 之前就已存好 pendingPreselect，
  // 这里挂载后主动拉取（消费式，取走即清），天然规避推送式的时序竞态。
  restorePreselect();

  eventCleanups = [
    EventsOn("scan:complete", () => {
      scanPhase.value = 'preparing';
    }),

    EventsOn("scan:error", () => {
      scanPhase.value = null;
    }),

    // doHide（扫描完成）/ doCancel（取消选择）是 Go 发来的**单向通知**，
    // 这里只负责收掉扫描遮罩。窗口的隐藏由 Go 自己直接执行（HideWindow），
    // 不依赖这两个监听器——绝不要把「隐藏窗口」的职责放回事件回调里，
    // 原因见 internal/app/app.go OnStartup 中的说明。
    // 其余状态无需在此复位：下次唤起时组件整体重挂。
    EventsOn("doHide", () => {
      scanPhase.value = null;
    }),

    EventsOn("doCancel", () => {
      scanPhase.value = null;
    }),
  ];

  // 每秒推进 nowTick，让状态栏的「N 秒前」自己走起来。
  // 只驱动一个数字的重算，开销可以忽略。
  ageTimer = setInterval(() => {
    nowTick.value = Date.now();
  }, 1000);
});

onUnmounted(() => {
  eventCleanups.forEach(off => off());
  eventCleanups = [];
  if (ageTimer) {
    clearInterval(ageTimer);
    ageTimer = null;
  }
  // 作废在途的目录加载响应，防止它回来时写入已卸载实例的 ref
  loadGeneration++;
  // 清掉路径补全的防抖定时器，否则卸载后仍会触发一次 ListDirectory
  if (suggestTimer) {
    clearTimeout(suggestTimer);
    suggestTimer = null;
  }
});

</script>

<style scoped>
/* [!! 样式部分没有改动，保持原样 !!] */
.file-selector {
  /* [!! 保持修正 !!] CSS 变量在根元素上 */
  --list-grid-columns: 40px 1fr 180px 120px 100px;

  display: flex;
  flex-direction: column;
  height: 100vh;
  background-color: #f9fafb;
  color: #1f2937;
  overflow: hidden;
}

.header {
  padding: 12px 16px;
  background-color: #ffffff;
  border-bottom: 1px solid #e5e7eb;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.05);
}

.navigation {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}

.nav-button {
  padding: 6px 12px;
  font-size: 14px;
  background-color: #e5e7eb;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  transition: background-color 0.2s;
}

.nav-button:hover:not(:disabled) {
  background-color: #d1d5db;
}

.nav-button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.view-toggle {
  margin-left: auto;
  font-size: 18px;
  padding: 4px 10px;
  line-height: 1;
}

/* 刷新按钮：紧贴视图切换按钮右侧（view-toggle 的 margin-left:auto 已把两者一起推到右端） */
.refresh-button {
  font-size: 18px;
  padding: 4px 10px;
  line-height: 1;
}

/* 加载中让图标转起来，同时按钮处于 disabled（见模板的 :disabled="isLoading"） */
.refresh-button.spinning {
  animation: refresh-spin 0.8s linear infinite;
  opacity: 0.6;
}

@keyframes refresh-spin {
  to { transform: rotate(360deg); }
}

/* ── 当前目录筛选框 ── */
.filter-wrapper {
  position: relative;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.filter-input {
  width: 180px;
  padding: 6px 24px 6px 10px;
  font-size: 13px;
  border: 1px solid #d1d5db;
  border-radius: 6px;
  outline: none;
  background-color: #ffffff;
  color: #1f2937;
}

.filter-input:focus {
  border-color: #3b82f6;
  box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.15);
}

.filter-clear {
  position: absolute;
  right: 6px;
  border: none;
  background: transparent;
  color: #9ca3af;
  font-size: 16px;
  line-height: 1;
  cursor: pointer;
  padding: 0 2px;
}

.filter-clear:hover {
  color: #4b5563;
}

/* ── 列表状态栏：条目数 / 读取时刻 / 新鲜度 ── */
.list-status-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 8px;
  font-size: 12px;
  color: #6b7280;
  min-height: 16px;
  flex-wrap: wrap;
}

.status-count {
  font-weight: 600;
  color: #4b5563;
}

.status-sep {
  color: #d1d5db;
}

.status-time.stale {
  color: #b45309;
  font-weight: 600;
}

.status-hint {
  margin-left: 4px;
  padding: 1px 8px;
  border-radius: 10px;
  background-color: #fffbeb;
  border: 1px solid #fde68a;
  color: #92400e;
}


.current-path-container {
  display: flex;
  align-items: center;
  gap: 8px;
}

.path-label {
  font-size: 14px;
  color: #4b5563;
  flex-shrink: 0;
}

.path-input {
  width: 100%;
  padding: 6px 10px;
  font-size: 14px;
  background-color: #f3f4f6;
  border: 1px solid #d1d5db;
  border-radius: 6px;
  color: #1f2937;
  box-sizing: border-box;
}

.path-input:focus {
  outline: none;
  background-color: #ffffff;
  border-color: #93c5fd;
  box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.15);
}

/* 路径输入框 + 建议列表的定位容器 */
.path-input-wrapper {
  position: relative;
  flex: 1;
  min-width: 0;
}

.path-suggestions {
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
  right: 0;
  z-index: 50;
  margin: 0;
  padding: 4px;
  list-style: none;
  background-color: #ffffff;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.12);
  max-height: 280px;
  overflow-y: auto;
}

.suggestion-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
  white-space: nowrap;
}

.suggestion-item:hover,
.suggestion-item.active {
  background-color: #eff6ff;
}

.suggestion-icon {
  flex-shrink: 0;
}

.suggestion-name {
  flex-shrink: 0;
  color: #1f2937;
  font-weight: 500;
}

.suggestion-path {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  color: #9ca3af;
  font-size: 12px;
  text-align: right;
}

.file-list-container {
  flex: 1;
  overflow: hidden;
  padding: 0;
  display: flex;
  flex-direction: column;
}

/* 列表视图表头 */
.file-list-header {
  display: grid;
  grid-template-columns: var(--list-grid-columns);
  background-color: #f9fafb;
  border-bottom: 1px solid #e5e7eb;
  padding: 0 8px;
  font-weight: 500;
  font-size: 14px;
  color: #374151;
  flex-shrink: 0;
}

.header-cell {
  padding: 10px 12px;
  cursor: pointer;
  user-select: none;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  transition: background-color 0.2s;
  display: flex; /*  设为 flex 以便居中复选框 */
  align-items: center; /*  垂直居中 */
}

/* [!! 新增 !!] 复选框列的特定样式 */
.header-cell.checkbox-cell {
  justify-content: center; /* 水平居中 */
  cursor: default;
  padding: 0;
  width: 40px;
}
.header-cell.checkbox-cell input {
  cursor: pointer;
}


.header-cell:hover {
  background-color: #f3f4f6;
}

.header-cell.size-cell {
  text-align: right;
  padding-right: 24px; /* 为滚动条留出空间 */
  justify-content: flex-end; /* [!! 新增 !!] 确保右对齐 */
}

.sort-arrow {
  display: inline-block;
  margin-left: 4px;
  font-size: 10px;
}

/* 滚动容器 (列表) */
.list-scroll-container {
  overflow-y: auto;
  height: 100%;
  padding: 0 8px;
  box-sizing: border-box;
}

/* [!! 保持修正 !!] 网格滚动容器 */
.grid-scroll-container {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
  gap: 16px;
  align-content: flex-start;
  overflow-y: auto;
  height: 100%;
  padding: 8px;
  box-sizing: border-box;
}


.loading, .empty-list {
  text-align: center;
  margin-top: 40px;
  font-size: 16px;
  color: #6b7280;
}

/* --- 列表视图 (List View) 样式 --- */
.list-view .file-item {
  display: grid;
  grid-template-columns: var(--list-grid-columns);
  align-items: center;
  padding: 0;
  border-radius: 6px;
  cursor: pointer;
  transition: background-color 0.1s;
  user-select: none;
  border: 1px solid transparent;
}

.list-view .file-item:hover {
  background-color: #f3f4f6;
}

.list-view .file-item.selected {
  background-color: #dbeafe;
  border: 1px solid #93c5fd;
}

.list-view .checkbox {
  margin: 0 auto;
  cursor: pointer;
}

.list-view .file-info {
  display: flex;
  align-items: center;
  gap: 10px;
  overflow: hidden;
  white-space: nowrap;
  padding: 8px 12px;
}

.list-view .file-icon {
  font-size: 18px;
  flex-shrink: 0;
}

.list-view .file-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 列表视图的新列样式 */
.list-view .file-modified,
.list-view .file-type,
.list-view .file-size {
  font-size: 14px;
  color: #6b7280;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  padding: 8px 12px;
}

.list-view .file-size {
  text-align: right;
  padding-right: 12px;
}

/* --- 网格视图 (Grid View) 样式 --- */
/* [!! 保持修正 !!] 网格项样式 */
.grid-scroll-container .file-item {
  display: flex;
  flex-direction: column;
  justify-content: flex-start;
  align-items: stretch;
  height: 130px;
  padding: 8px;
  border-radius: 6px;
  cursor: pointer;
  transition: background-color 0.1s;
  user-select: none;
  position: relative;
  border: 1px solid transparent;
}

.grid-scroll-container .file-item:hover {
  background-color: #f3f4f6;
}

.grid-scroll-container .file-item.selected {
  background-color: #dbeafe;
  border: 1px solid #93c5fd;
  padding: 8px;
}

.grid-scroll-container .file-item .file-info {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  flex-grow: 1;
  text-align: center;
  gap: 8px;
  overflow: hidden;
  white-space: normal;
}

.grid-scroll-container .file-item .checkbox {
  position: absolute;
  top: 6px;
  left: 6px;
  z-index: 1;
  cursor: pointer;
}

.grid-scroll-container .file-item .file-icon {
  font-size: 72px; /* 保持上次调整的大小 */
}

.grid-scroll-container .file-item .file-name {
  white-space: normal;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  word-break: break-all;
  line-height: 1.3;
  max-height: 2.6em;
}
/* ------------------------------------------ */


.drop-zone-prompt {
  text-align: center;
  padding: 12px;
  font-size: 14px;
  color: #9ca3af;
  border-top: 2px dashed #e5e7eb;
  background-color: #ffffff;
  min-height: 48px;
  max-height: 120px;
  overflow-y: auto;
  box-sizing: border-box;
  flex-shrink: 0;
  transition: background-color 0.2s, border-color 0.2s;
}

.drop-zone-prompt.has-selection {
  border-top-style: solid;
  text-align: left;
  padding: 8px;
}

.selected-files-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.selected-file-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background-color: #e0e7ff;
  color: #4338ca;
  padding: 4px 8px;
  border-radius: 12px;
  font-size: 12px;
  font-weight: 500;
  max-width: 100%;
  box-sizing: border-box;
}

.chip-icon {
  flex-shrink: 0;
}

.chip-path {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex-grow: 1;
  flex-shrink: 1;
  min-width: 0;
}

/* 预选恢复的目录数量标注 */
.chip-count {
  flex-shrink: 0;
  color: #6d28d9;
  background-color: rgba(109, 40, 217, 0.08);
  border-radius: 8px;
  padding: 1px 6px;
  font-size: 11px;
  white-space: nowrap;
}

/* 预选恢复结果提示条 */
.preselect-notice {
  position: relative;
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  padding: 8px 12px;
  margin: 0 16px 8px;
  background-color: #eff6ff;
  border: 1px solid #bfdbfe;
  border-radius: 8px;
  font-size: 13px;
  color: #1e40af;
}

.preselect-notice .notice-text {
  flex-grow: 1;
}

.preselect-notice .missing-count {
  color: #b45309;
  font-weight: 600;
}

.preselect-notice .toggle-missing {
  color: #2563eb;
  margin-left: 4px;
  text-decoration: underline;
  cursor: pointer;
}

.preselect-notice .notice-close {
  flex-shrink: 0;
  border: none;
  background: transparent;
  color: #1e40af;
  font-size: 16px;
  cursor: pointer;
  padding: 0 4px;
  line-height: 1;
}

.preselect-notice .missing-list {
  width: 100%;
  max-height: 120px;
  overflow-y: auto;
  border-top: 1px dashed #bfdbfe;
  padding-top: 6px;
  font-size: 12px;
  color: #92400e;
  word-break: break-all;
}

.chip-remove-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  padding: 0;
  margin-left: 4px;
  background-color: rgba(67, 56, 202, 0.2);
  color: #4338ca;
  border: none;
  border-radius: 50%;
  cursor: pointer;
  font-size: 14px;
  font-weight: bold;
  line-height: 1;
  flex-shrink: 0;
  transition: background-color 0.2s, color 0.2s;
}

.chip-remove-btn:hover {
  background-color: rgba(67, 56, 202, 0.4);
  color: #312e81;
}


.footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  background-color: #ffffff;
  border-top: 1px solid #e5e7eb;
  box-shadow: 0 -1px 3px rgba(0, 0, 0, 0.05);
  flex-shrink: 0;
}

.selection-info {
  font-size: 14px;
  color: #4b5563;
}

.actions {
  display: flex;
  gap: 12px;
}

.button {
  padding: 8px 18px;
  font-size: 14px;
  font-weight: 500;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.2s;
}

.button.cancel {
  background-color: #e5e7eb;
  color: #374151;
}

.button.cancel:hover {
  background-color: #d1d5db;
}

.button.submit {
  background-color: #3b82f6;
  color: #ffffff;
}

.button.submit:hover:not(:disabled) {
  background-color: #2563eb;
}

.button.submit:disabled {
  background-color: #93c5fd;
  cursor: not-allowed;
}
</style>
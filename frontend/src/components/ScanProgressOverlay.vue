<template>
  <div v-if="phase" class="overlay">
    <!-- Scanning Phase -->
    <div v-if="phase === 'scanning' && !errorMessage" class="overlay-content">
      <div class="scan-icon">📂</div>
      <div class="progress-bar-container">
        <div class="progress-bar-indeterminate"></div>
      </div>
      <p class="file-count">已发现 {{ filesFound }} 个文件 · {{ dirsFound }} 个文件夹</p>
      <p class="current-path">{{ displayPath }}</p>
    </div>

    <!-- Preparing Phase -->
    <div v-if="phase === 'preparing' && !errorMessage" class="overlay-content">
      <div class="spinner"></div>
      <p class="preparing-text">正在准备上传...</p>
      <p class="preparing-subtitle">已扫描 {{ filesFound }} 个文件 · {{ dirsFound }} 个文件夹，正在发送文件列表...</p>
    </div>

    <!-- Error State -->
    <div v-if="errorMessage" class="overlay-content error-content">
      <div class="error-icon">⚠️</div>
      <div class="error-alert">
        <p class="error-message">{{ errorMessage }}</p>
      </div>
      <button class="close-button" @click="emit('close')">关闭</button>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue';
import { truncatePath } from '../utils/path.js';
import { EventsOn } from 'wailsjs/runtime';

const props = defineProps({
  phase: {
    type: String,
    default: null,
    validator: (value) => [null, 'scanning', 'preparing'].includes(value)
  }
});

const emit = defineEmits(['close']);

const filesFound = ref(0);
const dirsFound = ref(0);
const currentPath = ref('');
const errorMessage = ref(null);

const displayPath = computed(() => truncatePath(currentPath.value, 60));

// 每当一次新扫描开始，清掉上一次的错误与进度计数。
//
// 必须有这一步：模板里 scanning/preparing 两个分支都带 `&& !errorMessage` 条件，
// 而 errorMessage 一旦被 scan:error 写入就没有别的复位点。扫描失败时 Go 不会隐藏
// 窗口（doHide 只在成功后发），用户点「关闭」收掉遮罩、留在同一个组件实例里再次
// 「确认选择」，phase 变回 scanning 但旧的 errorMessage 还在 —— 于是永远只看得到
// 上一次的错误，正常进度界面再也出不来，只能重启客户端。
//
// 跨会话（重新唤起选择器）的残留由组件随 FileSelector 一起重挂载解决，
// 这里补的是「同一次会话内重试」这条路径。
watch(() => props.phase, (phase) => {
  if (phase === 'scanning') {
    errorMessage.value = null;
    filesFound.value = 0;
    dirsFound.value = 0;
    currentPath.value = '';
  }
});

// EventsOn 返回取消函数，卸载时必须调用：本组件是 FileSelector 的子组件，
// 会随每次唤起选择器一起重新挂载，不注销就会叠加监听器。
let eventCleanups = [];

onMounted(() => {
  eventCleanups = [
    EventsOn('scan:progress', (data) => {
      filesFound.value = data.filesFound;
      dirsFound.value = data.dirsFound || 0;
      currentPath.value = data.currentPath;
    }),

    EventsOn('scan:error', (data) => {
      errorMessage.value = data.message;
    }),
  ];
});

onUnmounted(() => {
  eventCleanups.forEach(off => off());
  eventCleanups = [];
});
</script>

<style scoped>
.overlay {
  position: fixed;
  top: 0;
  left: 0;
  width: 100%;
  height: 100%;
  z-index: 100;
  background-color: rgba(249, 250, 251, 0.95);
  display: flex;
  align-items: center;
  justify-content: center;
}

.overlay-content {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
  max-width: 480px;
  width: 100%;
  padding: 0 24px;
}

/* Scanning phase */
.scan-icon {
  font-size: 48px;
  animation: pulse 2s ease-in-out infinite;
}

@keyframes pulse {
  0%, 100% { opacity: 1; transform: scale(1); }
  50% { opacity: 0.6; transform: scale(0.95); }
}

.progress-bar-container {
  width: 100%;
  height: 6px;
  background-color: #e5e7eb;
  border-radius: 3px;
  overflow: hidden;
}

.progress-bar-indeterminate {
  width: 40%;
  height: 100%;
  background-color: #3b82f6;
  border-radius: 3px;
  animation: indeterminate 1.5s ease-in-out infinite;
}

@keyframes indeterminate {
  0% { transform: translateX(-100%); }
  100% { transform: translateX(350%); }
}

.file-count {
  font-size: 18px;
  font-weight: 600;
  color: #1f2937;
  margin: 0;
}

.current-path {
  font-size: 13px;
  color: #6b7280;
  margin: 0;
  word-break: break-all;
  text-align: center;
}

/* Preparing phase */
.spinner {
  width: 40px;
  height: 40px;
  border: 4px solid #e5e7eb;
  border-top-color: #3b82f6;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.preparing-text {
  font-size: 18px;
  font-weight: 600;
  color: #1f2937;
  margin: 0;
}

.preparing-subtitle {
  font-size: 14px;
  color: #6b7280;
  margin: 0;
}

/* Error state */
.error-content {
  gap: 12px;
}

.error-icon {
  font-size: 48px;
}

.error-alert {
  background-color: #fef2f2;
  border: 1px solid #fecaca;
  border-radius: 8px;
  padding: 12px 16px;
  width: 100%;
}

.error-message {
  font-size: 14px;
  color: #991b1b;
  margin: 0;
  text-align: center;
}

.close-button {
  padding: 8px 24px;
  font-size: 14px;
  font-weight: 500;
  background-color: #e5e7eb;
  color: #374151;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  transition: background-color 0.2s;
}

.close-button:hover {
  background-color: #d1d5db;
}
</style>
<template>
  <div id="app-container">
    <FileSelector v-if="currentView === 'FileSelector'" :key="selectorKey"/>
    <Dashboard v-else-if="currentView === 'Dashboard'"/>
    <ConfigEditor v-else-if="currentView === 'ConfigEditor'"/>
    <div v-else class="loading-screen">
      正在加载...
    </div>

    <!--
      版本水印。
      刻意放在这里（App.vue 根容器内、所有 v-if 视图之外）而不是塞进某个页面：
        - 它不属于任何视图，在 Dashboard / ConfigEditor 之间切换都不会重建
        - position: fixed + 高 z-index，浮在包括全屏弹窗在内的一切内容之上
        - pointer-events: none，绝不拦截任何点击，纯展示

      FileSelector（文件选择器）不显示：那个界面是给用户挑文件的，右下角紧邻
      已选文件列表和确认/取消按钮，多一个浮层只会干扰。
      条件里额外要求 currentView 非空，避免启动握手完成前在「正在加载...」上闪一下。
    -->
    <div
        v-if="currentView && currentView !== 'FileSelector'"
        class="version-watermark"
    >{{ APP_VERSION }}</div>
  </div>
</template>

<script setup>
import {onMounted, ref} from 'vue';
//  导入 EventsEmit
import {EventsEmit, EventsOn} from 'wailsjs/runtime';
import FileSelector from './components/FileSelector.vue';
import Dashboard from './components/Dashboard.vue';
import ConfigEditor from './components/ConfigEditor.vue';
import {APP_VERSION} from './version.js';

// [!! 新增 !!] 视图路由状态
const currentView = ref(null); // 默认为 null，等待 Go 发送事件

// 文件选择器的「会话序号」：每次 Go 请求显示 FileSelector 就自增，作为 :key
// 强制组件重新挂载。
//
// 为什么必须这样做：客户端窗口是单例复用（app.go 的 ShowWindow/WindowHide，
// 不新建窗口）。用户把选择器留在前台不动，浏览器再次 POST /reselect 时，
// Go 仍然只发一个 view:show="FileSelector"。此时 currentView 已经是
// 'FileSelector'，而 Vue 的 ref setter 用 Object.is 比较、同值直接跳过通知，
// 于是 v-if 不变 → 组件不重挂 → onMounted 不执行 → 目录不刷新，上一次的
// 当前路径、已勾选项、扫描遮罩、路径补全全部残留。
//
// 用递增 key 而不是在 FileSelector 内手写一份「复位状态清单」：全新实例天然
// 零残留，将来往组件里加新状态也不会漏掉复位（清单式复位一定会腐化）。
// 附带好处是子组件 ScanProgressOverlay 跟着重挂，其 errorMessage 也不再
// 跨会话卡在错误态。
const selectorKey = ref(0);

onMounted(() => {
  //  监听来自 Go 的视图切换命令
  EventsOn("view:show", (viewName) => {
    console.log("Wails Event 'view:show' received:", viewName);
    if (viewName === 'FileSelector') {
      selectorKey.value++;
    }
    currentView.value = viewName;
  });

  // [!! 新增 !!] 握手：告诉 Go 后端，Vue 已经准备就绪。
  // 顺带把界面版本标识带过去，让它出现在客户端日志里——现场反馈时可以直接从
  // 日志确认装的是哪个包，不用让用户去截图右下角水印。
  console.log("Frontend is mounted, emitting frontend:ready, version:", APP_VERSION);
  EventsEmit("frontend:ready", APP_VERSION);
});

</script>

<style>
/* 全局样式重置 */
html, body {
  margin: 0;
  padding: 0;
  width: 100%;
  height: 100%;
  overflow: hidden; /* 防止滚动条 */
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  background-color: #f9fafb; /* 浅灰色背景 */
}

#app-container {
  height: 100vh;
  width: 100vw;
  display: flex;
  flex-direction: column;
}

/* [!! 新增 !!] 加载样式 */
.loading-screen {
  display: flex;
  justify-content: center;
  align-items: center;
  height: 100%;
  font-size: 1.2rem;
  color: #666;
}

/* ── 版本水印 ──
   bottom: 62px 是为了落在底栏上方：Dashboard 的 .dashboard-footer 高 54px + 1px 边框，
   FileSelector 的 .footer 约 59px，取 62px 两个页面都不会被压住。
   z-index 取 9999：全仓现有最大值是 1050（trace 视图），确保水印在最上层。 */
.version-watermark {
  position: fixed;
  right: 10px;
  bottom: 62px;
  z-index: 9999;

  /* 关键：不参与命中测试，点击穿透到下面的页面 */
  pointer-events: none;
  user-select: none;

  padding: 3px 7px;
  border-radius: 4px;
  font-family: Consolas, "Courier New", monospace;
  font-size: 11px;
  line-height: 1;
  letter-spacing: 0.3px;
  white-space: nowrap;
  color: #6b7280;
  background-color: rgba(255, 255, 255, 0.75);
  border: 1px solid rgba(0, 0, 0, 0.06);
}
</style>
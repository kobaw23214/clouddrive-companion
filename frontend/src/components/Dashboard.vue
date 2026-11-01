<template>
  <div class="dashboard">
    <header class="dashboard-header">
      <div class="header-top-row">
        <div class="header-stats">
          <div class="stat-item">
            <span class="label">总速度</span>
            <span class="value speed">{{ stats.currentSpeed || '0 B/s' }}</span>
          </div>
          <div class="stat-item">
            <span class="label">活动任务</span>
            <span class="value">{{ stats.activeTasks || 0 }}</span>
          </div>
          <div class="stat-item">
            <span class="label">排队/暂停</span>
            <span class="value">{{ stats.queuedTasks || 0 }}</span>
          </div>
          <div class="stat-item">
            <span class="label">活动文件</span>
            <span class="value">{{ stats.completedFiles || 0 }} / {{ stats.totalFiles || 0 }}</span>
          </div>
        </div>

        <!-- 当前连接的后端地址 -->
        <div
          class="backend-conn-card"
          :class="{ 'is-override': backendConn.overridden }"
          :title="backendConn.baseUrl ? ('当前连接后端：' + backendConn.baseUrl + (backendConn.overridden ? '（由前端拉起时传入，优先于配置文件）' : '（来自配置文件 config.json）')) : '未获取到后端地址'">
          <div class="backend-conn-head">
            <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="2" width="20" height="8" rx="2" ry="2"></rect><rect x="2" y="14" width="20" height="8" rx="2" ry="2"></rect><line x1="6" y1="6" x2="6.01" y2="6"></line><line x1="6" y1="18" x2="6.01" y2="18"></line></svg>
            <span class="backend-conn-label">当前连接后端</span>
            <span class="backend-conn-source" :class="backendConn.overridden ? 'src-uri' : 'src-config'">
              {{ backendConn.overridden ? 'URI 传入' : '配置文件' }}
            </span>
          </div>
          <div class="backend-conn-url">{{ backendConn.baseUrl || '未配置' }}</div>
        </div>

        <!-- 进入配置面板 -->
        <button class="settings-entry-btn" @click="openConfigEditor" title="打开配置面板">
          <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"></circle><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"></path></svg>
        </button>
      </div>

      <!-- 端点状态面板 -->
      <div class="endpoint-panel">
        <div class="endpoint-info">
          <div class="endpoint-status-tag interactive" :class="endpointInfo.running ? 'is-running' : 'is-stopped'" @click="openHttpLogModal" title="点击查看 HTTP 请求日志">
            <div class="tag-status-indicator">
              <svg v-if="endpointInfo.running" xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="currentColor" stroke="currentColor" stroke-width="1" stroke-linecap="round" stroke-linejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"></polygon></svg>
              <svg v-else xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect><line x1="9" y1="9" x2="15" y2="15"></line><line x1="15" y1="9" x2="9" y2="15"></line></svg>
              <span>HTTP: {{ endpointInfo.port || 34567 }}</span>
            </div>
            <span class="tag-action">请求 <span v-if="httpLogTotal" class="history-count">{{ httpLogTotal }}</span></span>
          </div>
          <div class="endpoint-status-tag interactive" @click="openAppLogModal" title="点击查看应用日志">
            <div class="tag-status-indicator app-log-indicator">
              <svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path><polyline points="14 2 14 8 20 8"></polyline><line x1="16" y1="13" x2="8" y2="13"></line><line x1="16" y1="17" x2="8" y2="17"></line><polyline points="10 9 9 9 8 9"></polyline></svg>
              <span>系统日志</span>
            </div>
            <span class="tag-action">文件 <span v-if="appLogFiles.length" class="history-count">{{ appLogFiles.length }}</span></span>
          </div>
          <!-- 任务级链路追踪入口（独立于 HTTP 日志：HTTP 看请求级，这里看任务级） -->
          <div class="endpoint-status-tag interactive" @click="openTraceModal" title="点击查看任务级链路追踪">
            <div class="tag-status-indicator trace-indicator">
              <svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <circle cx="12" cy="6" r="2"></circle>
                <circle cx="6" cy="18" r="2"></circle>
                <circle cx="18" cy="18" r="2"></circle>
                <line x1="12" y1="8" x2="6" y2="16"></line>
                <line x1="12" y1="8" x2="18" y2="16"></line>
              </svg>
              <span>链路追踪</span>
            </div>
            <span class="tag-action">任务 <span v-if="traceTotal" class="history-count">{{ traceTotal }}</span></span>
          </div>
        </div>
      </div>
    </header>

    <!-- HTTP 请求日志弹窗 -->
    <div v-if="showHttpLogModal" class="modal-overlay" @click.self="closeHttpLogModal">
      <div class="modal-box modal-box-fullscreen">
        <div class="modal-header">
          <span>HTTP 请求日志（共 {{ httpLogTotal }} 条 · 数据库 {{ formatSize(httpLogStats.dbSize || 0) }}）</span>
          <div class="modal-header-actions">
            <button class="action-btn" @click="openSequenceModal" title="查看 HTTP 交互时序图">
              <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="4" y1="8" x2="20" y2="8"></line><polyline points="16 4 20 8 16 12"></polyline><line x1="20" y1="16" x2="4" y2="16"></line><polyline points="8 12 4 16 8 20"></polyline></svg>
              时序图
            </button>
            <button class="action-btn" @click="openHttpLogInBrowser" title="在默认浏览器中打开此查看器">
              <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"></path><polyline points="15 3 21 3 21 9"></polyline><line x1="10" y1="14" x2="21" y2="3"></line></svg>
              浏览器查看
            </button>
            <button class="action-btn" @click="refreshHttpLogs" :disabled="httpLogLoading">
              <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"></polyline><polyline points="1 20 1 14 7 14"></polyline><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"></path></svg>
              {{ httpLogLoading ? '刷新中…' : '刷新' }}
            </button>
            <button class="btn-return" @click="closeHttpLogModal">返回</button>
          </div>
        </div>
        <div class="modal-body">
          <!-- 列表视图 -->
          <div v-if="!selectedHttpLog">
            <div class="http-log-filters">
              <input
                v-model="filterDraft.pathLike"
                placeholder="按路径模糊搜索（例: /upload）"
                class="filter-input"
                @keyup.enter="applyFilter" />
              <select v-model="filterDraft.method" class="filter-select" @change="applyFilter">
                <option value="">全部方法</option>
                <option value="GET">GET</option>
                <option value="POST">POST</option>
                <option value="PUT">PUT</option>
                <option value="DELETE">DELETE</option>
                <option value="OPTIONS">OPTIONS</option>
              </select>
              <select v-model="filterDraft.statusGroup" class="filter-select" @change="applyFilter">
                <option value="">全部状态</option>
                <option value="2xx">2xx</option>
                <option value="3xx">3xx</option>
                <option value="4xx">4xx</option>
                <option value="5xx">5xx</option>
              </select>
              <button class="page-btn" @click="applyFilter" :disabled="httpLogLoading">查询</button>
              <button class="page-btn" @click="resetFilter" :disabled="httpLogLoading">重置</button>
              <label class="auto-refresh-label" style="margin-left: 8px;">
                <input type="checkbox" v-model="filterDraft.excludePing" @change="applyFilter" /> 屏蔽 /ping
              </label>
              <label class="auto-refresh-label">
                <input type="checkbox" v-model="httpLogAutoRefresh" @change="setupHttpLogAutoRefresh" />
                自动刷新（3 秒）
              </label>
            </div>

            <div v-if="!httpLogEntries.length" class="meta-empty">
              {{ httpLogLoading ? '加载中…' : '暂无符合条件的请求' }}
            </div>
            <template v-else>
              <table class="meta-table http-log-table">
                <thead>
                  <tr>
                    <th>#</th>
                    <th>路径</th>
                    <th>方法</th>
                    <th>状态</th>
                    <th>响应大小</th>
                    <th>耗时</th>
                    <th>时间</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="rec in httpLogEntries" :key="rec.id" class="history-row" @click="openHttpLogDetail(rec)">
                    <td>{{ rec.id }}</td>
                    <td :title="rec.path + (rec.query ? '?' + rec.query : '')">
                      {{ rec.path }}<span v-if="rec.query" class="query-suffix">?{{ rec.query }}</span>
                    </td>
                    <td>
                      <span class="http-method" :class="'method-' + rec.method.toLowerCase()">{{ rec.method }}</span>
                    </td>
                    <td>
                      <span class="http-status" :class="statusClass(rec.statusCode)">{{ rec.statusCode || '—' }}</span>
                    </td>
                    <td>{{ formatSize(rec.responseBytes) }}</td>
                    <td>{{ rec.durationMs }} ms</td>
                    <td>{{ rec.timestamp }}</td>
                    <td class="td-action">查看 ›</td>
                  </tr>
                </tbody>
              </table>
              <div class="pagination">
                <button class="page-btn" :disabled="httpLogPage <= 1 || httpLogLoading" @click="goToPage(httpLogPage - 1)">‹ 上一页</button>
                <span class="page-info">第 {{ httpLogPage }} / {{ totalPages }} 页 · 共 {{ httpLogTotal }} 条</span>
                <button class="page-btn" :disabled="httpLogPage >= totalPages || httpLogLoading" @click="goToPage(httpLogPage + 1)">下一页 ›</button>
                <select v-model.number="httpLogPageSize" @change="onPageSizeChange" class="page-size-select">
                  <option :value="20">20 条/页</option>
                  <option :value="50">50 条/页</option>
                  <option :value="100">100 条/页</option>
                  <option :value="200">200 条/页</option>
                </select>
              </div>
            </template>
          </div>

          <!-- 详情视图 -->
          <div v-else class="http-log-detail">
            <button class="back-btn" @click="selectedHttpLog = null">‹ 返回列表</button>
            <div class="meta-summary">
              <span class="http-method" :class="'method-' + selectedHttpLog.method.toLowerCase()">{{ selectedHttpLog.method }}</span>
              <span class="detail-path">{{ selectedHttpLog.path }}<span v-if="selectedHttpLog.query">?{{ selectedHttpLog.query }}</span></span>
              <span class="http-status" :class="statusClass(selectedHttpLog.statusCode)">{{ selectedHttpLog.statusCode }}</span>
              <span class="detail-meta">{{ selectedHttpLog.timestamp }} · {{ selectedHttpLog.durationMs }} ms · {{ formatSize(selectedHttpLog.responseBytes) }} · 来自 {{ selectedHttpLog.remoteAddr }}</span>
            </div>

            <div class="detail-section">
              <div class="detail-section-title">请求头</div>
              <vue-json-pretty :data="selectedHttpLog.requestHeaders" :deep="2" :show-line="true" :show-double-quotes="false" />
            </div>

            <div class="detail-section" v-if="selectedHttpLog.requestBody">
              <div class="detail-section-title">
                请求体
                <span v-if="selectedHttpLog.requestBodyTruncated" class="truncated-badge">已截断（仅显示前 1 MB）</span>
              </div>
              <vue-json-pretty
                v-if="parsedRequestBody !== undefined"
                :data="parsedRequestBody"
                :deep="3"
                :show-line="true"
                :show-double-quotes="false"
              />
              <pre v-else class="raw-body">{{ selectedHttpLog.requestBody }}</pre>
            </div>

            <div class="detail-section">
              <div class="detail-section-title">响应头</div>
              <vue-json-pretty :data="selectedHttpLog.responseHeaders" :deep="2" :show-line="true" :show-double-quotes="false" />
            </div>

            <div class="detail-section" v-if="selectedHttpLog.responseBody">
              <div class="detail-section-title">
                响应体
                <span v-if="selectedHttpLog.responseBodyTruncated" class="truncated-badge">已截断（仅显示前 1 MB）</span>
              </div>
              <vue-json-pretty
                v-if="parsedResponseBody !== undefined"
                :data="parsedResponseBody"
                :deep="3"
                :show-line="true"
                :show-double-quotes="false"
              />
              <pre v-else class="raw-body">{{ selectedHttpLog.responseBody }}</pre>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 应用日志弹窗 -->
    <div v-if="showAppLogModal" class="modal-overlay" @click.self="closeAppLogModal">
      <div class="modal-box modal-box-fullscreen app-log-modal">
        <div class="modal-header">
          <span>应用日志（clouddrive-companion-*.log）</span>
          <div class="modal-header-actions">
            <button class="action-btn" @click="openAppLogInBrowser" title="在默认浏览器中打开此查看器">
              <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"></path><polyline points="15 3 21 3 21 9"></polyline><line x1="10" y1="14" x2="21" y2="3"></line></svg>
              浏览器查看
            </button>
            <button class="action-btn" @click="reloadAppLog" :disabled="appLogLoading || !selectedAppLogFile">
              <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"></polyline><polyline points="1 20 1 14 7 14"></polyline><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"></path></svg>
              {{ appLogLoading ? '加载中…' : '刷新' }}
            </button>
            <button class="btn-return" @click="closeAppLogModal">返回</button>
          </div>
        </div>
        <div class="modal-body app-log-body">
          <!-- 左侧：日志文件列表 -->
          <aside class="app-log-sidebar">
            <div class="sidebar-title">日志文件 ({{ appLogFiles.length }})</div>
            <div v-if="!appLogFiles.length" class="meta-empty">暂无日志</div>
            <ul v-else class="app-log-file-list">
              <li
                v-for="f in appLogFiles"
                :key="f.name"
                :class="['app-log-file-item', { active: selectedAppLogFile && selectedAppLogFile.name === f.name }]"
                @click="selectAppLogFile(f)">
                <div class="app-log-file-date">{{ f.date }}</div>
                <div class="app-log-file-meta">{{ formatSize(f.size) }} · {{ formatLogTime(f.modifiedMs) }}</div>
              </li>
            </ul>
          </aside>

          <!-- 右侧：选中文件的内容 -->
          <section class="app-log-content">
            <div v-if="!selectedAppLogFile" class="meta-empty">请选择左侧的日志文件</div>
            <template v-else>
              <div class="app-log-toolbar">
                <input
                  v-model="appLogKeywordDraft"
                  placeholder="按关键字过滤（大小写不敏感，例: ERROR）"
                  class="filter-input"
                  @keyup.enter="applyAppLogKeyword" />
                <button class="page-btn" @click="applyAppLogKeyword" :disabled="appLogLoading">查询</button>
                <button class="page-btn" @click="resetAppLogKeyword" :disabled="appLogLoading">重置</button>
                <span class="app-log-stats">
                  {{ selectedAppLogFile.name }} · {{ formatSize(selectedAppLogFile.size) }}
                  <span v-if="appLogLines.length"> · 已加载 {{ appLogLines.length }} 行</span>
                </span>
              </div>

              <div ref="appLogPaneRef" class="app-log-pane" @scroll="onAppLogPaneScroll">
                <div v-if="!appLogLines.length && !appLogLoading" class="meta-empty">
                  {{ appLogKeyword ? '没有匹配的日志行' : '日志为空' }}
                </div>
                <div v-for="(line, idx) in appLogLines" :key="idx" :class="['app-log-line', appLogLineClass(line)]">
                  <span class="app-log-line-no">{{ idx + 1 }}</span>
                  <span class="app-log-line-text"><template v-for="(seg, segIdx) in splitAppLogLine(line)" :key="segIdx"><mark v-if="seg.match" class="app-log-hl">{{ seg.text }}</mark><template v-else>{{ seg.text }}</template></template></span>
                </div>
                <div v-if="appLogHasMore" class="app-log-loadmore">
                  <span v-if="appLogLoading" class="app-log-loading-hint">加载中…</span>
                </div>
                <div v-else-if="appLogLines.length" class="app-log-end">— 已经到达文件起点 —</div>
              </div>
            </template>
          </section>
        </div>
      </div>
    </div>

    <!-- Goroutines 弹窗 -->
    <div v-if="showGoroutinesModal" class="modal-overlay" @click.self="closeGoroutinesModal">
      <div class="modal-box modal-box-fullscreen">
        <div class="modal-header">
          <span>Goroutines 详情 (总计: {{ goroutinesData.length }})</span>
          <div class="modal-header-actions">
            <button class="action-btn" @click="refreshGoroutines" :disabled="goroutinesLoading">
              <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"></polyline><polyline points="1 20 1 14 7 14"></polyline><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"></path></svg>
              {{ goroutinesLoading ? '加载中…' : '刷新' }}
            </button>
            <button class="btn-return" @click="closeGoroutinesModal">返回</button>
          </div>
        </div>
        <div class="modal-body goroutines-body">
          <aside class="goroutines-sidebar">
            <div class="sidebar-title">线程列表 ({{ goroutinesData.length }})</div>
            <ul class="goroutines-list">
              <li
                v-for="g in goroutinesData"
                :key="g.id"
                :class="['goroutines-item', { active: selectedGoroutine?.id === g.id }]"
                @click="selectedGoroutine = g">
                <div class="goroutine-meta">
                  <span class="goroutine-id">ID: {{ g.id }}</span>
                  <span :class="['goroutine-state', g.state.includes('running') ? 'state-running' : 'state-waiting']">{{ g.state }}</span>
                </div>
              </li>
            </ul>
          </aside>
          <section class="goroutines-content">
            <div v-if="!selectedGoroutine" class="meta-empty">请在左侧选择一个 Goroutine 查看其调用栈</div>
            <div v-else class="goroutine-stack-wrapper">
              <div class="detail-section-title">
                Goroutine {{ selectedGoroutine.id }} - {{ selectedGoroutine.state }}
              </div>
              <pre class="raw-body goroutine-stack">{{ selectedGoroutine.stack }}</pre>
            </div>
          </section>
        </div>
      </div>
    </div>

    <!-- iOS 风格内存展示页面 (全屏) -->
    <div v-if="showMemoryView" class="modal-overlay" @click.self="closeMemoryView">
      <div class="modal-box modal-box-fullscreen memory-fullscreen-box">
        <div class="modal-header memory-modal-header">
          <span>上传客户端内存存储</span>
          <div class="modal-header-actions">
            <button class="action-btn" @click="refreshMemory" :disabled="memoryLoading">
              <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"></polyline><polyline points="1 20 1 14 7 14"></polyline><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"></path></svg>
              {{ memoryLoading ? '刷新中…' : '刷新' }}
            </button>
            <button class="btn-return" @click="closeMemoryView">返回</button>
          </div>
        </div>
        
        <div class="modal-body memory-modal-body">
          <div class="ios-storage-container-light">
            <div class="ios-storage-header-light">
              <h2 class="ios-storage-title-light">上传客户端</h2>
              <div class="ios-storage-usage-light">
                已使用 {{ formatSize(memoryData?.alloc || 0) }} / {{ formatSize(memoryData?.sys || 0) }}
              </div>
            </div>
            
            <div class="ios-storage-bar-light">
              <!-- 堆内存 Alloc -->
              <div class="ios-bar-segment-light segment-alloc-light" 
                   :style="{ width: ((memoryData?.alloc || 0) / (memoryData?.sys || 1)) * 100 + '%' }">
              </div>
              <!-- 空闲/系统保留内存 Sys - Alloc -->
              <div class="ios-bar-segment-light segment-free-light" 
                   :style="{ width: (((memoryData?.sys || 0) - (memoryData?.alloc || 0)) / (memoryData?.sys || 1)) * 100 + '%' }">
              </div>
            </div>
            
            <div class="ios-storage-legend-light">
              <div class="legend-item-light">
                <span class="legend-dot-light dot-alloc-light"></span>
                <span class="legend-name-light">已分配堆内存 (Alloc)</span>
              </div>
              <div class="legend-item-light">
                <span class="legend-dot-light dot-free-light"></span>
                <span class="legend-name-light">系统闲置/保留 (Sys - Alloc)</span>
              </div>
            </div>

            <div class="memory-stats-grid-light">
              <div class="memory-stat-card-light">
                <div class="stat-card-label-light">累计分配总计</div>
                <div class="stat-card-value-light">{{ formatSize(memoryData?.totalAlloc || 0) }}</div>
              </div>
              <div class="memory-stat-card-light">
                <div class="stat-card-label-light">系统占用总计 (Sys)</div>
                <div class="stat-card-value-light">{{ formatSize(memoryData?.sys || 0) }}</div>
              </div>
              <div class="memory-stat-card-light">
                <div class="stat-card-label-light">垃圾回收次数</div>
                <div class="stat-card-value-light">{{ memoryData?.numGC || 0 }} 次</div>
              </div>
              <div class="memory-stat-card-light">
                <div class="stat-card-label-light">下次 GC 阈值</div>
                <div class="stat-card-value-light">{{ formatSize(memoryData?.nextGC || 0) }}</div>
              </div>
              <div class="memory-stat-card-light">
                <div class="stat-card-label-light">GC 暂停总时长</div>
                <div class="stat-card-value-light">{{ ((memoryData?.pauseTotalNs || 0) / 1000000).toFixed(2) }} ms</div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 端口 弹窗 -->
    <div v-if="showPortsModal" class="modal-overlay" @click.self="closePortsModal">
      <div class="modal-box modal-box-fullscreen">
        <div class="modal-header">
          <span>当前进程端口连接 (总计: {{ portsData.length }})</span>
          <div class="modal-header-actions">
            <button class="action-btn" @click="refreshPorts" :disabled="portsLoading">
              <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"></polyline><polyline points="1 20 1 14 7 14"></polyline><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"></path></svg>
              {{ portsLoading ? '加载中…' : '刷新' }}
            </button>
            <button class="btn-return" @click="closePortsModal">返回</button>
          </div>
        </div>
        <div class="modal-body" style="padding: 0;">
          <table class="meta-table" style="width: 100%; border-radius: 0;">
            <thead>
              <tr>
                <th>协议</th>
                <th>本地地址 (Local Address)</th>
                <th>远程地址 (Foreign Address)</th>
                <th>状态 (State)</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="!portsData.length">
                <td colspan="4" class="meta-empty">暂无连接信息</td>
              </tr>
              <tr v-for="(conn, idx) in portsData" :key="idx" class="history-row">
                <td><span class="http-method" :class="'method-' + conn.proto.toLowerCase()">{{ conn.proto }}</span></td>
                <td>{{ conn.local }}</td>
                <td>{{ conn.remote }}</td>
                <td>
                  <span class="goroutine-state" :class="conn.state === 'ESTABLISHED' || conn.state === 'LISTENING' ? 'state-running' : 'state-waiting'">
                    {{ conn.state }}
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <main class="dashboard-main">
      <div v-if="!selectedTask" class="task-list-container">
        <!-- 顶部 Tab 切换：上传任务（实时） / 历史记录（manifest） -->
        <div class="task-tabs">
          <button
            class="task-tab"
            :class="{ active: taskTab === 'active' }"
            @click="switchTaskTab('active')">
            上传任务 ({{ stats.tasks.length }})
          </button>
          <button
            class="task-tab"
            :class="{ active: taskTab === 'history' }"
            @click="switchTaskTab('history')">
            历史记录 ({{ historicalTasks.length }})
          </button>
        </div>

        <!-- 当前活跃任务列表 -->
        <template v-if="taskTab === 'active'">
          <div v-if="stats.tasks.length === 0" class="empty-state">
            当前没有上传任务
          </div>
          <div class="task-list">
            <div v-for="task in sortedTasks" :key="task.taskId" class="task-item" @click="selectedTask = task">
              <div class="task-info">
                <span class="task-name" :title="task.taskName">{{ task.taskName || task.taskId }}</span>
                <span class="task-status" :class="task.status.toLowerCase()">
                  {{ getStatusText(task.status) }}
                </span>
              </div>
              <div class="task-progress-details">
                <span class="task-files">{{ task.completedFiles }} / {{ task.totalFiles }} 文件
                  <span v-if="task.failedFiles > 0" class="task-failed-count">（{{ task.failedFiles }} 个失败）</span>
                </span>
                <span class="task-size">{{ formatSize(task.uploadedSize) }} / {{ formatSize(task.totalSize) }}</span>
              </div>
              <div class="progress-bar-container">
                <div class="progress-bar" :style="{ width: task.progress + '%' }"></div>
              </div>

              <div class="task-actions" v-if="task.status === 'UPLOADING' || task.status === 'PAUSED' || task.status === 'PARTIAL_COMPLETED'">
                <button
                    v-if="task.status === 'UPLOADING'"
                    @click.stop="handlePauseTask(task.taskId)"
                    :disabled="isSendingCmd"
                    class="task-action-btn pause">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <line x1="9" y1="5" x2="9" y2="19"></line>
                    <line x1="15" y1="5" x2="15" y2="19"></line>
                  </svg>
                  暂停
                </button>
                <button
                    v-if="task.status === 'PAUSED'"
                    @click.stop="handleResumeTask(task.taskId)"
                    :disabled="isSendingCmd"
                    class="task-action-btn resume">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <polygon points="6 3 20 12 6 21 6 3"></polygon>
                  </svg>
                  继续
                </button>
                <button
                    v-if="task.status === 'PARTIAL_COMPLETED'"
                    @click.stop="handleResumeTask(task.taskId)"
                    :disabled="isSendingCmd"
                    class="task-action-btn retry">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <polyline points="1 4 1 10 7 10"></polyline>
                    <path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"></path>
                  </svg>
                  重试失败文件
                </button>
                <!-- 已暂停 / 部分完成的任务允许直接删除；上传中的任务需先暂停，避免误删正在写入的任务 -->
                <button
                    v-if="task.status === 'PAUSED' || task.status === 'PARTIAL_COMPLETED'"
                    @click.stop="handleDeletePausedTask(task)"
                    :disabled="isSendingCmd"
                    class="task-action-btn delete"
                    title="删除此任务（仅清理本地任务清单与日志，不影响已上传的文件）">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M3 6h18"></path>
                    <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"></path>
                    <path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                    <line x1="10" y1="11" x2="10" y2="17"></line>
                    <line x1="14" y1="11" x2="14" y2="17"></line>
                  </svg>
                  删除
                </button>
              </div>
            </div>
          </div>
        </template>

        <!-- 历史记录列表：复用相同的 task-item 模板，禁用所有交互按钮 -->
        <template v-else>
          <div v-if="historyLoading" class="empty-state">加载中…</div>
          <div v-else-if="historicalTasks.length === 0" class="empty-state">
            暂无历史任务
          </div>
          <div v-else class="task-list">
            <div v-for="task in historicalTasks" :key="task.taskId" class="task-item" @click="selectedTask = task">
              <div class="task-info">
                <span class="task-name" :title="task.taskName">{{ task.taskName || task.taskId }}</span>
                <span class="task-status" :class="task.status.toLowerCase()">
                  {{ getStatusText(task.status) }}
                </span>
              </div>
              <div class="task-progress-details">
                <span class="task-files">{{ task.completedFiles }} / {{ task.totalFiles }} 文件
                  <span v-if="task.failedFiles > 0" class="task-failed-count">（{{ task.failedFiles }} 个失败）</span>
                </span>
                <span class="task-size">{{ formatSize(task.uploadedSize) }} / {{ formatSize(task.totalSize) }}</span>
              </div>
              <div class="progress-bar-container">
                <div class="progress-bar" :style="{ width: task.progress + '%' }"></div>
              </div>
              <div class="task-actions">
                <button
                    v-if="task.traceId"
                    @click.stop="handleViewTrace(task)"
                    class="task-action-btn resume"
                    title="查看该任务的链路追踪详情">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <circle cx="12" cy="6" r="2"></circle>
                    <circle cx="6" cy="18" r="2"></circle>
                    <circle cx="18" cy="18" r="2"></circle>
                    <line x1="12" y1="8" x2="6" y2="16"></line>
                    <line x1="12" y1="8" x2="18" y2="16"></line>
                  </svg>
                  查看链路
                </button>
                <button
                    @click.stop="handleDeleteHistory(task.taskId)"
                    class="task-action-btn pause"
                    title="从历史记录中删除（不影响已上传的文件）">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M3 6h18"></path>
                    <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"></path>
                    <path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                    <line x1="10" y1="11" x2="10" y2="17"></line>
                    <line x1="14" y1="11" x2="14" y2="17"></line>
                  </svg>
                  删除记录
                </button>
              </div>
            </div>
          </div>
        </template>
      </div>

      <div v-if="selectedTask" class="file-list-container">
        <div class="task-header-section">
          <button @click="goBackToList" class="back-button">
            <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor">
              <path d="M11.5 13.5L6 8L11.5 2.5" stroke="currentColor" stroke-width="2" stroke-linecap="round"
                    stroke-linejoin="round" fill="none"/>
            </svg>
            返回任务列表
          </button>
          <div class="task-title-wrapper">
            <svg class="task-icon" width="24" height="24" viewBox="0 0 24 24" fill="none">
              <path
                  d="M9 13H15M9 17H15M13 3H8.2C7.08 3 6.52 3 6.092 3.218C5.716 3.41 5.41 3.716 5.218 4.092C5 4.52 5 5.08 5 6.2V17.8C5 18.92 5 19.48 5.218 19.908C5.41 20.284 5.716 20.59 6.092 20.782C6.52 21 7.08 21 8.2 21H15.8C16.92 21 17.48 21 17.908 20.782C18.284 20.59 18.59 20.284 18.782 19.908C19 19.48 19 18.92 19 17.8V9M13 3L19 9M13 3V7.4C13 7.96 13 8.24 13.109 8.454C13.205 8.642 13.358 8.795 13.546 8.891C13.76 9 14.04 9 14.6 9H19"
                  stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
            </svg>
            <h2 class="task-title" :title="selectedTask.taskName">
              {{ selectedTask.taskName }}
            </h2>
          </div>
        </div>

        <div v-if="selectedTask.totalFiles > 0" class="file-list-table">
          <div class="file-list-column-headers">
            <div class="header-col header-col-name"
                 :class="{ 'active-sort': sortKey === 'fileName' }"
                 @click="handleSort('fileName')">
              <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor" class="header-icon">
                <path
                    d="M8.5 2.5C8.5 2.22 8.28 2 8 2C7.72 2 7.5 2.22 7.5 2.5V11.29L4.35 8.15C4.16 7.96 3.84 7.96 3.65 8.15C3.46 8.34 3.46 8.66 3.65 8.85L7.65 12.85C7.84 13.04 8.16 13.04 8.35 12.85L12.35 8.85C12.54 8.66 12.54 8.34 12.35 8.15C12.16 7.96 11.84 7.96 11.65 8.15L8.5 11.29V2.5Z"/>
              </svg>
              <span class="header-text">文件名</span>
              <span class="sort-indicator">
                <span v-if="sortKey === 'fileName'">{{ sortDirection === 'asc' ? '↑' : '↓' }}</span>
                <span v-else class="neutral-sort">↕</span>
              </span>
            </div>

            <div class="header-col header-col-path"
                 :class="{ 'active-sort': sortKey === 'relativePath' }"
                 @click="handleSort('relativePath')">
              <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor" class="header-icon">
                <path
                    d="M2 4.5C2 3.67 2.67 3 3.5 3H5.38C5.77 3 6.14 3.18 6.38 3.5L7.62 5H12.5C13.33 5 14 5.67 14 6.5V11.5C14 12.33 13.33 13 12.5 13H3.5C2.67 13 2 12.33 2 11.5V4.5Z"/>
              </svg>
              <span class="header-text">文件路径</span>
              <span class="sort-indicator">
                <span v-if="sortKey === 'relativePath'">{{ sortDirection === 'asc' ? '↑' : '↓' }}</span>
                <span v-else class="neutral-sort">↕</span>
              </span>
            </div>

            <div class="header-col header-col-progress"
                 :class="{ 'active-sort': sortKey === 'progress' }"
                 @click="handleSort('progress')">
              <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor" class="header-icon">
                <path
                    d="M8 2C4.69 2 2 4.69 2 8C2 11.31 4.69 14 8 14C11.31 14 14 11.31 14 8C14 4.69 11.31 2 8 2ZM8 12.5C5.52 12.5 3.5 10.48 3.5 8C3.5 5.52 5.52 3.5 8 3.5C10.48 3.5 12.5 5.52 12.5 8C12.5 10.48 10.48 12.5 8 12.5Z"/>
                <path d="M8 5V8L10.5 9.5"/>
              </svg>
              <span class="header-text">上传进度</span>
              <span class="sort-indicator">
                <span v-if="sortKey === 'progress'">{{ sortDirection === 'asc' ? '↑' : '↓' }}</span>
                <span v-else class="neutral-sort">↕</span>
              </span>
            </div>
          </div>

          <div class="file-list-info">
            <span>共 {{ fileTotalCount }} 个文件</span>
            <span v-if="isLoadingFiles" class="loading-hint">加载中...</span>
          </div>

          <div class="file-list" ref="fileListRef" @scroll="onFileListScroll">
            <div class="virtual-spacer-top" :style="{ height: virtualTopHeight + 'px' }"></div>
            <div v-for="file in visibleFiles" :key="file.relativePath" class="file-item">
              <span class="file-name" :title="file.fileName">{{ file.fileName }}</span>
              <span class="file-path" :class="{ clickable: file.absolutePath }"
                    :title="file.absolutePath ? `点击打开所在文件夹\n${file.absolutePath}` : file.relativePath"
                    @click.stop="handleRevealFile(file)">{{ file.relativePath }}</span>
              <div class="file-progress-details">
                <span class="file-status" :class="file.status.toLowerCase()">
                  {{ getStatusText(file.status) }}
                </span>
                <span class="file-size">{{ formatSize(file.uploadedSize) }} / {{ formatSize(file.totalSize) }}</span>

                <div class="file-controls"
                     v-if="file.status === 'UPLOADING' || file.status === 'PAUSED' || file.status === 'FAILED'">
                  <button
                      v-if="file.status === 'UPLOADING'"
                      @click="handlePauseFile(selectedTask.taskId, file.relativePath)"
                      :disabled="isSendingCmd"
                      class="file-action-btn pause"
                      title="暂停此文件">
                    ⏸
                  </button>
                  <button
                      v-if="file.status === 'PAUSED' || file.status === 'FAILED'"
                      @click="handleResumeFile(selectedTask.taskId, file.relativePath)"
                      :disabled="isSendingCmd"
                      class="file-action-btn resume"
                      title="继续此文件">
                    ▶
                  </button>
                </div>
                <div class="progress-bar-container small">
                  <div class="progress-bar" :style="{ width: file.progress + '%' }"></div>
                </div>
              </div>
            </div>
            <div class="virtual-spacer-bottom" :style="{ height: virtualBottomHeight + 'px' }"></div>
          </div>
        </div>
      </div>
    </main>

    <footer class="dashboard-footer">
      <div class="footer-modules">
        <!-- Goroutines 模块 -->
        <div class="footer-module" @click="openGoroutinesModal" title="点击查看 Goroutine 详情">
          <div class="module-icon">
            <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <rect x="2" y="2" width="20" height="8" rx="2" ry="2"></rect>
              <rect x="2" y="14" width="20" height="8" rx="2" ry="2"></rect>
              <line x1="6" y1="6" x2="6.01" y2="6"></line>
              <line x1="6" y1="18" x2="6.01" y2="18"></line>
            </svg>
          </div>
          <div class="module-info">
            <span class="module-name">Goroutines</span>
            <span class="module-value">{{ stats.sysStats?.numGoroutines || 0 }}</span>
          </div>
        </div>

        <!-- 内存 模块 -->
        <div class="footer-module" @click="openMemoryModal" title="点击查看内存占用详情">
          <div class="module-icon">
            <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <rect x="4" y="4" width="16" height="16" rx="2" ry="2"></rect>
              <rect x="9" y="9" width="6" height="6"></rect>
              <line x1="9" y1="1" x2="9" y2="4"></line>
              <line x1="15" y1="1" x2="15" y2="4"></line>
              <line x1="9" y1="20" x2="9" y2="23"></line>
              <line x1="15" y1="20" x2="15" y2="23"></line>
              <line x1="20" y1="9" x2="23" y2="9"></line>
              <line x1="20" y1="14" x2="23" y2="14"></line>
              <line x1="1" y1="9" x2="4" y2="9"></line>
              <line x1="1" y1="14" x2="4" y2="14"></line>
            </svg>
          </div>
          <div class="module-info">
            <span class="module-name">Memory</span>
            <span class="module-value">{{ formatSize(stats.sysStats?.allocMem || 0) }}</span>
          </div>
        </div>

        <!-- 端口 模块 -->
        <div class="footer-module" :class="{ 'warning': stats.sysStats?.isWarning }" @click="openPortsModal" title="点击查看端口分配详情">
          <div class="module-icon">
            <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <rect x="2" y="2" width="20" height="20" rx="2" ry="2"></rect>
              <circle cx="12" cy="12" r="3"></circle>
              <line x1="12" y1="2" x2="12" y2="9"></line>
              <line x1="12" y1="15" x2="12" y2="22"></line>
              <line x1="2" y1="12" x2="9" y2="12"></line>
              <line x1="15" y1="12" x2="22" y2="12"></line>
            </svg>
          </div>
          <div class="module-info">
            <span class="module-name">Ports</span>
            <span class="module-value">{{ (stats.sysStats?.portUsage || 0).toFixed(1) }}% / {{ stats.sysStats?.maxPorts || 16384 }}</span>
          </div>
        </div>
      </div>
    </footer>
  </div>

  <!-- 任务级链路追踪 modal（独立组件，放在 .dashboard 外，使其真正全屏） -->
  <TraceModal
    v-if="showTraceModal"
    :initial-trace-id="traceModalInitial.traceId"
    :initial-task-name="traceModalInitial.taskName"
    @close="closeTraceModal" />

  <!-- HTTP 交互时序图 modal（独立组件，从 HTTP 请求日志弹窗工具栏进入） -->
  <TraceSequenceModal
    v-if="showSequenceModal"
    @close="closeSequenceModal" />
</template>

<script setup>
import {computed, nextTick, onMounted, onUnmounted, ref, watch, defineAsyncComponent} from 'vue';
//  导入 Wails API
import {EventsOn, BrowserOpenURL} from 'wailsjs/runtime';
import {GetTaskFiles, PauseFile, PauseTask, ResumeFile, ResumeTask, QueryHTTPLogs, GetHTTPLogByID, GetHTTPLogStats, ListAppLogFiles, ReadAppLogFile, ListHistoricalTasks, DeleteHistoricalTask, DeletePausedTask, ListTracedTasks, GetBackendConnectionInfo, ShowConfigEditor, RevealFileInFolder} from 'wailsjs/go/app/App';
import VueJsonPretty from 'vue-json-pretty';
import 'vue-json-pretty/lib/styles.css';

// 任务级链路追踪入口 —— 独立组件，与 Dashboard 其它弹窗解耦。
// 后续追踪 UI 的所有改动只在 ./trace/* 内进行，本文件不再变动。
const TraceModal = defineAsyncComponent(() => import('./trace/TraceModal.vue'));
const showTraceModal = ref(false);
// 链路追踪 trace 总数（顶部 tag 显示用），与列表分页解耦：每次打开 modal
// 后会再刷一次精确值；这里只是给 dashboard 顶部一个粗粒度的概览数字。
const traceTotal = ref(0);
// 打开 TraceModal 时如果设置了 traceId，会跳过列表直接进入对应 trace 顶层 span 视图。
// 由"历史记录 → 查看链路"按钮设置；从顶部 tag 进入时为空（看完整列表）。
const traceModalInitial = ref({ traceId: '', taskName: '' });
function openTraceModal() {
  traceModalInitial.value = { traceId: '', taskName: '' };
  showTraceModal.value = true;
}
function closeTraceModal() {
  showTraceModal.value = false;
  // 关闭追踪弹窗时刷新一下顶部 tag，反映在 modal 内做的删除等操作
  refreshDashboardCounters();
}

// HTTP 交互时序图弹窗 —— 同样异步加载，从 HTTP 请求日志弹窗工具栏的"时序图"按钮进入。
const TraceSequenceModal = defineAsyncComponent(() => import('./trace/TraceSequenceModal.vue'));
const showSequenceModal = ref(false);
function openSequenceModal() {
  showSequenceModal.value = true;
}
function closeSequenceModal() {
  showSequenceModal.value = false;
}

// 仪表盘的完整状态
const stats = ref({
  activeTasks: 0,
  queuedTasks: 0,
  completedTasks: 0,
  totalFiles: 0,
  completedFiles: 0,
  currentSpeed: '0 B/s',
  currentSpeedBps: 0,
  tasks: [],
  sysStats: {
    numGoroutines: 0,
    tcpTimeWait: 0,
    tcpEstablished: 0,
    portUsage: 0,
    isWarning: false
  }
});

// 端点会话状态
const endpointInfo = ref({ port: 34567, running: false });

// 当前实际连接的后端地址（scheme://host:port）。
//   baseUrl    : 形如 http://192.168.1.100:8081，解析失败为空串
//   overridden : true = 用的是前端拉起时通过 URI 传入的内存地址（优先级高于 config.json）
//                false = 用的是 config.json 里配置的地址
const backendConn = ref({ baseUrl: '', overridden: false });

// HTTP 请求日志
const showHttpLogModal = ref(false);
const httpLogEntries = ref([]);
const httpLogTotal = ref(0);
const httpLogPage = ref(1);
const httpLogPageSize = ref(50);
const httpLogLoading = ref(false);
const selectedHttpLog = ref(null);
const httpLogStats = ref({ total: 0, dbPath: '', dbSize: 0, droppedMem: 0 });
const httpLogAutoRefresh = ref(true);
// 查询过滤参数（draft = 表单值，applied = 当前生效值）
const filterDraft = ref({ method: '', pathLike: '', statusGroup: '', excludePing: true });
const filterApplied = ref({ method: '', pathLike: '', statusGroup: '', excludePing: true });
let httpLogTimer = null;

// 应用日志（clouddrive-companion-*.log）
const showAppLogModal = ref(false);
const appLogFiles = ref([]);
const selectedAppLogFile = ref(null);
const appLogLines = ref([]);
const appLogNextOffset = ref(0);
const appLogHasMore = ref(false);
const appLogLoading = ref(false);
const appLogKeywordDraft = ref('');
const appLogKeyword = ref('');
const appLogPaneRef = ref(null);
const APP_LOG_PAGE_SIZE = 200;

// Goroutines 详情
const showGoroutinesModal = ref(false);
const goroutinesData = ref([]);
const selectedGoroutine = ref(null);
const goroutinesLoading = ref(false);

async function refreshGoroutines() {
  if (goroutinesLoading.value) return;
  goroutinesLoading.value = true;
  try {
    // 使用全局对象调用以避免 wailsjs 绑定未重新生成导致的问题
    const data = await window.go.app.App.GetGoroutinesProfile();
    goroutinesData.value = data || [];
    if (goroutinesData.value.length > 0 && !selectedGoroutine.value) {
      selectedGoroutine.value = goroutinesData.value[0];
    } else if (selectedGoroutine.value) {
      // 保持当前选中的更新
      const updated = goroutinesData.value.find(g => g.id === selectedGoroutine.value.id);
      if (updated) {
        selectedGoroutine.value = updated;
      }
    }
  } catch (err) {
    console.error("Failed to load Goroutines profile:", err);
  } finally {
    goroutinesLoading.value = false;
  }
}

function openGoroutinesModal() {
  showGoroutinesModal.value = true;
  refreshGoroutines();
}

function closeGoroutinesModal() {
  showGoroutinesModal.value = false;
}

// 内存 详情
const showMemoryView = ref(false);
const memoryData = ref(null);
const memoryLoading = ref(false);

async function refreshMemory() {
  if (memoryLoading.value) return;
  memoryLoading.value = true;
  try {
    memoryData.value = await window.go.app.App.GetMemoryDetails();
  } catch (err) {
    console.error("Failed to load Memory details:", err);
  } finally {
    memoryLoading.value = false;
  }
}

function openMemoryModal() {
  showMemoryView.value = true;
  refreshMemory();
}

function closeMemoryView() {
  showMemoryView.value = false;
}

// 端口 详情
const showPortsModal = ref(false);
const portsData = ref([]);
const portsLoading = ref(false);

async function refreshPorts() {
  if (portsLoading.value) return;
  portsLoading.value = true;
  try {
    const data = await window.go.app.App.GetPortConnections();
    portsData.value = data || [];
  } catch (err) {
    console.error("Failed to load Ports details:", err);
  } finally {
    portsLoading.value = false;
  }
}

function openPortsModal() {
  showPortsModal.value = true;
  refreshPorts();
}

function closePortsModal() {
  showPortsModal.value = false;
}

const totalPages = computed(() => {
  if (httpLogTotal.value <= 0) return 1;
  return Math.max(1, Math.ceil(httpLogTotal.value / httpLogPageSize.value));
});

// 尝试把 body 字符串解析成 JSON，失败则返回 undefined（让模板回退到 raw 显示）
const parsedRequestBody = computed(() => tryParseJson(selectedHttpLog.value?.requestBody));
const parsedResponseBody = computed(() => tryParseJson(selectedHttpLog.value?.responseBody));

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

async function refreshHttpLogStats() {
  try {
    const stats = await GetHTTPLogStats();
    httpLogStats.value = stats || { total: 0, dbPath: '', dbSize: 0, droppedMem: 0 };
  } catch (err) {
    console.error('加载 HTTP 日志统计失败:', err);
  }
}

async function refreshHttpLogs() {
  if (httpLogLoading.value) return;
  httpLogLoading.value = true;
  try {
    const result = await QueryHTTPLogs(buildQuery(httpLogPage.value, httpLogPageSize.value));
    httpLogTotal.value = result.total || 0;
    httpLogEntries.value = result.entries || [];
    if (httpLogPage.value > totalPages.value) {
      httpLogPage.value = 1;
      const firstPage = await QueryHTTPLogs(buildQuery(1, httpLogPageSize.value));
      httpLogTotal.value = firstPage.total || 0;
      httpLogEntries.value = firstPage.entries || [];
    }
    refreshHttpLogStats();
  } catch (err) {
    console.error('加载 HTTP 日志失败:', err);
    httpLogEntries.value = [];
    httpLogTotal.value = 0;
  } finally {
    httpLogLoading.value = false;
  }
}

function buildQuery(page, size) {
  const f = filterApplied.value;
  return {
    page,
    size,
    method: f.method || '',
    pathLike: f.pathLike || '',
    statusGroup: f.statusGroup || '',
    startMs: 0,
    endMs: 0,
    excludePing: f.excludePing !== undefined ? f.excludePing : true,
  };
}

function applyFilter() {
  filterApplied.value = { ...filterDraft.value };
  httpLogPage.value = 1;
  refreshHttpLogs();
}

function resetFilter() {
  filterDraft.value = { method: '', pathLike: '', statusGroup: '', excludePing: true };
  applyFilter();
}

async function openHttpLogDetail(rec) {
  // body/headers 已经在列表里了，但为了拿到完整未截断信息，单独再查一次
  try {
    const full = await GetHTTPLogByID(rec.id);
    selectedHttpLog.value = full || rec;
  } catch (err) {
    console.error('加载日志详情失败:', err);
    selectedHttpLog.value = rec;
  }
}

function setupHttpLogAutoRefresh() {
  if (httpLogTimer) {
    clearInterval(httpLogTimer);
    httpLogTimer = null;
  }
  if (showHttpLogModal.value && httpLogAutoRefresh.value) {
    httpLogTimer = setInterval(() => {
      if (showHttpLogModal.value && !selectedHttpLog.value && httpLogAutoRefresh.value) {
        refreshHttpLogs();
      }
    }, 3000);
  }
}

async function openHttpLogModal() {
  showHttpLogModal.value = true;
  selectedHttpLog.value = null;
  httpLogPage.value = 1;
  filterDraft.value = { method: '', pathLike: '', statusGroup: '', excludePing: true };
  filterApplied.value = { method: '', pathLike: '', statusGroup: '', excludePing: true };
  await refreshHttpLogs();
  setupHttpLogAutoRefresh();
}

function closeHttpLogModal() {
  showHttpLogModal.value = false;
  selectedHttpLog.value = null;
  if (httpLogTimer) {
    clearInterval(httpLogTimer);
    httpLogTimer = null;
  }
}

function goToPage(page) {
  if (page < 1 || page > totalPages.value) return;
  httpLogPage.value = page;
  refreshHttpLogs();
}

function onPageSizeChange() {
  httpLogPage.value = 1;
  refreshHttpLogs();
}

// ========== 应用日志 ==========

async function openAppLogModal() {
  showAppLogModal.value = true;
  selectedAppLogFile.value = null;
  appLogLines.value = [];
  appLogKeywordDraft.value = '';
  appLogKeyword.value = '';
  await loadAppLogFiles();
  // 默认选中最新（第一项）
  if (appLogFiles.value.length > 0) {
    await selectAppLogFile(appLogFiles.value[0]);
  }
}

function closeAppLogModal() {
  showAppLogModal.value = false;
  selectedAppLogFile.value = null;
  appLogLines.value = [];
}

async function loadAppLogFiles() {
  try {
    const files = await ListAppLogFiles();
    appLogFiles.value = files || [];
  } catch (err) {
    console.error('加载应用日志列表失败:', err);
    appLogFiles.value = [];
  }
}

async function selectAppLogFile(file) {
  if (selectedAppLogFile.value && selectedAppLogFile.value.name === file.name) {
    return; // 已选中，不重复加载
  }
  selectedAppLogFile.value = file;
  appLogLines.value = [];
  appLogNextOffset.value = 0;
  appLogHasMore.value = false;
  await fetchAppLogPage(true);
}

async function fetchAppLogPage(reset) {
  if (!selectedAppLogFile.value || appLogLoading.value) return;
  appLogLoading.value = true;
  try {
    const offset = reset ? 0 : appLogNextOffset.value;
    const result = await ReadAppLogFile(selectedAppLogFile.value.name, offset, APP_LOG_PAGE_SIZE, appLogKeyword.value || '');
    const newLines = result.lines || [];
    if (reset) {
      appLogLines.value = newLines;
    } else {
      appLogLines.value = appLogLines.value.concat(newLines);
    }
    appLogNextOffset.value = result.nextOffset || 0;
    appLogHasMore.value = !!result.hasMore;
    // 同步文件大小（reload 时可能已变化）
    if (selectedAppLogFile.value && result.totalSize) {
      selectedAppLogFile.value = { ...selectedAppLogFile.value, size: result.totalSize };
    }
  } catch (err) {
    console.error('读取应用日志失败:', err);
  } finally {
    appLogLoading.value = false;
  }
}

function loadMoreAppLog() {
  fetchAppLogPage(false);
}

// 日志面板滚动到底部时自动加载更多
let appLogScrollTimer = null;
function onAppLogPaneScroll(e) {
  if (!appLogHasMore.value || appLogLoading.value) return;
  if (appLogScrollTimer) clearTimeout(appLogScrollTimer);
  appLogScrollTimer = setTimeout(() => {
    const el = e.target;
    // 距离底部不到 150px 时触发自动加载
    if (el.scrollTop + el.clientHeight >= el.scrollHeight - 150) {
      loadMoreAppLog();
    }
  }, 80);
}

async function reloadAppLog() {
  if (!selectedAppLogFile.value) return;
  appLogLines.value = [];
  appLogNextOffset.value = 0;
  appLogHasMore.value = false;
  await fetchAppLogPage(true);
}

function applyAppLogKeyword() {
  appLogKeyword.value = (appLogKeywordDraft.value || '').trim();
  reloadAppLog();
}

function resetAppLogKeyword() {
  appLogKeywordDraft.value = '';
  appLogKeyword.value = '';
  reloadAppLog();
}

function appLogLineClass(line) {
  if (/\b(ERROR|FATAL|严重错误|panic|致命)/i.test(line)) return 'log-level-error';
  if (/\b(WARN|警告|Warning)/i.test(line)) return 'log-level-warn';
  if (/\b(INFO)/i.test(line)) return 'log-level-info';
  return '';
}

// 按当前查询关键字把日志行拆成片段，用于高亮匹配部分（大小写不敏感）
function splitAppLogLine(line) {
  const kw = (appLogKeyword.value || '').trim();
  if (!kw) return [{ text: line, match: false }];
  const lowerLine = line.toLowerCase();
  const lowerKw = kw.toLowerCase();
  const segs = [];
  let pos = 0;
  for (;;) {
    const idx = lowerLine.indexOf(lowerKw, pos);
    if (idx === -1) break;
    if (idx > pos) segs.push({ text: line.slice(pos, idx), match: false });
    segs.push({ text: line.slice(idx, idx + lowerKw.length), match: true });
    pos = idx + lowerKw.length;
  }
  if (pos < line.length) segs.push({ text: line.slice(pos), match: false });
  return segs.length ? segs : [{ text: line, match: false }];
}

function formatLogTime(ms) {
  if (!ms) return '';
  const d = new Date(ms);
  const pad = (n) => n.toString().padStart(2, '0');
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function openAppLogInBrowser() {
  // 通过 Wails runtime 用系统默认浏览器打开本地服务的查看器
  try {
    BrowserOpenURL('http://localhost:34567/applog');
  } catch (e) {
    // 兜底：直接 location 跳转（在 wails 内部窗口里也能跳）
    window.open('http://localhost:34567/applog', '_blank');
  }
}

function openHttpLogInBrowser() {
  try {
    BrowserOpenURL('http://localhost:34567/httplog');
  } catch (e) {
    window.open('http://localhost:34567/httplog', '_blank');
  }
}

// 用于显示文件详情
const selectedTask = ref(null);

// 排序状态
const sortKey = ref("fileName");
const sortDirection = ref('asc');

// 命令发送状态
const isSendingCmd = ref(false);

// 任务列表 Tab：active = 实时上传任务（来自 stats.tasks），history = 历史记录（manifest）
const taskTab = ref('active');
const historicalTasks = ref([]);
const historyLoading = ref(false);

async function refreshHistoricalTasks() {
  historyLoading.value = true;
  try {
    const list = await ListHistoricalTasks();
    historicalTasks.value = Array.isArray(list) ? list : [];
  } catch (e) {
    console.error('加载历史任务失败:', e);
    historicalTasks.value = [];
  } finally {
    historyLoading.value = false;
  }
}

function switchTaskTab(tab) {
  taskTab.value = tab;
  if (tab === 'history') refreshHistoricalTasks();
}

// 历史记录卡片的"查看链路"按钮：直接打开 TraceModal 并跳转到该任务的
// top-spans 视图。task.traceId 在 ListHistoricalTasks 里已带回（来自
// task_history.db），如果为空（manifest 兜底分支没记 traceId）按钮不显示。
function handleViewTrace(task) {
  if (!task || !task.traceId) return;
  traceModalInitial.value = {
    traceId: task.traceId,
    taskName: task.taskName || task.taskId || '',
  };
  showTraceModal.value = true;
}

async function handleDeleteHistory(taskId) {
  if (!confirm('确认从历史记录中删除该任务？此操作仅删除本地的任务清单与日志，不影响已上传的文件。')) return;
  try {
    await DeleteHistoricalTask(taskId);
    await refreshHistoricalTasks();
  } catch (e) {
    alert('删除失败：' + (e && e.message ? e.message : e));
  }
}

// 点击文件路径：在系统资源管理器中打开所在文件夹并选中该文件。
// 老历史记录没有 absolutePath 字段，此时路径渲染为纯文本（不可点击）。
async function handleRevealFile(file) {
  if (!file || !file.absolutePath) return;
  try {
    await RevealFileInFolder(file.absolutePath);
  } catch (e) {
    alert('打开所在文件夹失败：' + (e && e.message ? e.message : e));
  }
}

// ===== 虚拟滚动 & 分页加载 =====
const ITEM_HEIGHT = 48; // 每行高度(px)
const PAGE_SIZE = 200;  // 每次从后端加载的条数
const BUFFER_ROWS = 10; // 上下缓冲行数

const fileListRef = ref(null);
const fileCache = ref([]);       // 已加载的文件缓存
const fileTotalCount = ref(0);   // 后端返回的总数
const isLoadingFiles = ref(false);
const loadedRange = ref({ start: 0, end: 0 }); // 已加载的数据范围

// 滚动位置计算出的可见区域
const scrollTop = ref(0);
const containerHeight = ref(600);

// 虚拟滚动计算
const visibleStartIndex = computed(() => {
  return Math.max(0, Math.floor(scrollTop.value / ITEM_HEIGHT) - BUFFER_ROWS);
});

const visibleEndIndex = computed(() => {
  const visibleCount = Math.ceil(containerHeight.value / ITEM_HEIGHT);
  return Math.min(fileTotalCount.value, Math.floor(scrollTop.value / ITEM_HEIGHT) + visibleCount + BUFFER_ROWS);
});

const visibleFiles = computed(() => {
  const start = visibleStartIndex.value;
  const end = visibleEndIndex.value;
  // 从缓存中取出可见范围的数据
  const result = [];
  for (let i = start; i < end; i++) {
    const cacheIndex = i - loadedRange.value.start;
    if (cacheIndex >= 0 && cacheIndex < fileCache.value.length) {
      result.push(fileCache.value[cacheIndex]);
    }
  }
  return result;
});

const virtualTopHeight = computed(() => {
  return visibleStartIndex.value * ITEM_HEIGHT;
});

const virtualBottomHeight = computed(() => {
  return Math.max(0, (fileTotalCount.value - visibleEndIndex.value) * ITEM_HEIGHT);
});

// 从后端加载文件数据
let loadRequestId = 0;
async function loadFiles(offset, limit) {
  if (!selectedTask.value || isLoadingFiles.value) return;

  isLoadingFiles.value = true;
  const requestId = ++loadRequestId;

  try {
    const result = await GetTaskFiles(
      selectedTask.value.taskId,
      offset,
      limit,
      sortKey.value || '',
      sortDirection.value || 'asc',
      '' // statusFilter
    );

    // 防止过期请求覆盖
    if (requestId !== loadRequestId) return;

    fileCache.value = result.files || [];
    fileTotalCount.value = result.total || 0;
    loadedRange.value = { start: offset, end: offset + (result.files?.length || 0) };
  } catch (e) {
    console.error('加载文件列表失败:', e);
  } finally {
    if (requestId === loadRequestId) {
      isLoadingFiles.value = false;
    }
  }
}

// 根据滚动位置决定是否需要加载新数据
function checkAndLoad() {
  if (!selectedTask.value || fileTotalCount.value === 0) return;

  const neededStart = visibleStartIndex.value;
  const neededEnd = visibleEndIndex.value;

  // 如果可见区域超出已加载范围，重新加载
  if (neededStart < loadedRange.value.start || neededEnd > loadedRange.value.end) {
    // 以可见区域为中心，前后各多加载一些
    const loadStart = Math.max(0, neededStart - PAGE_SIZE);
    const loadSize = PAGE_SIZE * 3; // 加载3页，保证滚动流畅
    loadFiles(loadStart, loadSize);
  }
}

let scrollTimer = null;
function onFileListScroll(e) {
  scrollTop.value = e.target.scrollTop;
  containerHeight.value = e.target.clientHeight;

  // 节流检查是否需要加载
  if (scrollTimer) clearTimeout(scrollTimer);
  scrollTimer = setTimeout(checkAndLoad, 100);
}

// 选中任务时加载第一页
watch(selectedTask, async (task) => {
  if (task) {
    fileCache.value = [];
    fileTotalCount.value = task.totalFiles || 0;
    loadedRange.value = { start: 0, end: 0 };
    scrollTop.value = 0;
    await loadFiles(0, PAGE_SIZE * 2);
    await nextTick();
    if (fileListRef.value) {
      containerHeight.value = fileListRef.value.clientHeight;
    }
  }
});

// 定时刷新文件列表（活动任务状态会变化）
let refreshTimer = null;
watch(selectedTask, (task) => {
  if (refreshTimer) {
    clearInterval(refreshTimer);
    refreshTimer = null;
  }
  if (task) {
    refreshTimer = setInterval(() => {
      if (selectedTask.value && !isLoadingFiles.value) {
        loadFiles(loadedRange.value.start, loadedRange.value.end - loadedRange.value.start || PAGE_SIZE);
      }
    }, 5000);
  }
}, { immediate: true });

// EventsOn 返回取消函数，卸载时必须调用。视图在 Dashboard / FileSelector /
// ConfigEditor 之间来回切换会反复挂载本组件，不注销就会每切一次多叠一个
// 监听器，往已销毁实例的 ref 上写数据。
let statsCleanup = null;

// 监听来自 Go 的 'dashboard:stats' 事件
onMounted(() => {
  statsCleanup = EventsOn("dashboard:stats", (data) => {
    stats.value = data;

    if (!stats.value.sysStats) {
      stats.value.sysStats = {
        numGoroutines: 0,
        tcpTimeWait: 0,
        tcpEstablished: 0,
        portUsage: 0,
        isWarning: false
      };
    }

    if (data.endpointInfo) {
      endpointInfo.value = {
        port: data.endpointInfo.port,
        running: data.endpointInfo.running,
      };
    }

    if (selectedTask.value) {
      // 仅在"上传任务"tab 下做"任务消失则跳回列表"的自动清理；
      // 历史 tab 选中的任务永远不在 data.tasks 里，否则一刷就会被清空。
      if (taskTab.value === 'active') {
        const updatedTask = data.tasks.find(t => t.taskId === selectedTask.value.taskId);
        if (updatedTask) {
          // 保留选中状态，只更新统计信息（不覆盖 files）
          selectedTask.value = { ...selectedTask.value, ...updatedTask };
        } else {
          selectedTask.value = null;
        }
      }
    }
  });

  // 顶部 4 个统计 tag（请求数 / 系统日志文件数 / 链路追踪任务数 / 历史记录数）
  // 都是懒加载值——不主动拉一次首次启动 UI 上全是空的。这里在挂载后立刻
  // 各调一次接口给 ref 灌初值，之后再依赖事件流刷新。
  refreshDashboardCounters();
});

onUnmounted(() => {
  if (statsCleanup) {
    statsCleanup();
    statsCleanup = null;
  }
});

// refreshDashboardCounters 拉取 4 个顶部 tag 的初始值。各调用相互独立，
// 任意一个失败不影响其他显示（用 catch 兜底）。
async function refreshDashboardCounters() {
  // HTTP 请求日志总数
  GetHTTPLogStats().then((s) => {
    if (s && typeof s.total === 'number') httpLogTotal.value = s.total;
    if (s) httpLogStats.value = s;
  }).catch(() => {});

  // 应用日志文件数
  ListAppLogFiles().then((files) => {
    appLogFiles.value = Array.isArray(files) ? files : [];
  }).catch(() => {});

  // 链路追踪 trace 总数：size=1 减少 IO，仅取 total 字段
  ListTracedTasks('', '', 0, 0, 1, 1).then((resp) => {
    traceTotal.value = (resp && typeof resp.total === 'number') ? resp.total : 0;
  }).catch(() => {});

  // 历史任务数：复用 historicalTasks 数组，下方 tab 切换时还会再刷一次
  ListHistoricalTasks().then((list) => {
    historicalTasks.value = Array.isArray(list) ? list : [];
  }).catch(() => {});

  // 当前连接的后端地址（内存覆盖优先，否则取 config.json）
  GetBackendConnectionInfo().then((info) => {
    if (info) {
      backendConn.value = {
        baseUrl: info.baseUrl || '',
        overridden: !!info.overridden,
      };
    }
  }).catch(() => {});
}

// 排序任务列表
const sortedTasks = computed(() => {
  if (!stats.value.tasks) return [];
  return [...stats.value.tasks].sort((a, b) => {
    const getOrder = (status) => {
      if (status === 'UPLOADING') return 1;
      if (status === 'QUEUED') return 2;
      if (status === 'PARTIAL_COMPLETED') return 3;
      if (status === 'PAUSED') return 4;
      return 5;
    };
    return getOrder(a.status) - getOrder(b.status);
  });
});

// 任务/文件 操作处理函数
async function handlePauseTask(taskId) {
  const task = stats.value.tasks.find(t => t.taskId === taskId);
  if (task) {
    task.status = 'PAUSED';
  }

  isSendingCmd.value = true;
  try {
    await PauseTask(taskId);
  } catch (error) {
    console.error(`暂停任务 ${taskId} 失败:`, error);
    if (task) {
      task.status = 'UPLOADING';
    }
  } finally {
    isSendingCmd.value = false;
  }
}

async function handleResumeTask(taskId) {
  const task = stats.value.tasks.find(t => t.taskId === taskId);
  if (task) {
    task.status = 'UPLOADING';
  }

  isSendingCmd.value = true;
  try {
    await ResumeTask(taskId);
  } catch (error) {
    console.error(`恢复任务 ${taskId} 失败:`, error);
    if (task) {
      task.status = 'PAUSED';
    }
  } finally {
    isSendingCmd.value = false;
  }
}

// 打开配置面板。由 Go 侧发 view:show 事件切换视图，
// 配置面板里可以再通过"返回"按钮回到本面板。
async function openConfigEditor() {
  try {
    await ShowConfigEditor();
  } catch (error) {
    console.error('打开配置面板失败:', error);
  }
}

// 删除已暂停 / 部分完成的任务：清掉本地 manifest 与日志，任务卡片随之消失。
// 已上传到服务端的文件不受影响，所以这里只做一次确认提示。
async function handleDeletePausedTask(task) {
  const label = task.taskName || task.taskId;
  if (!confirm(`确认删除任务「${label}」？\n\n删除后该任务将无法继续上传（需重新选择文件），仅清理本地的任务清单与日志，不影响已上传成功的文件。`)) {
    return;
  }

  isSendingCmd.value = true;
  try {
    await DeletePausedTask(task.taskId);

    // 乐观移除卡片：后端 TriggerStatsRefresh 也会推新数据，这里先让 UI 立刻响应
    stats.value.tasks = stats.value.tasks.filter(t => t.taskId !== task.taskId);
    if (selectedTask.value && selectedTask.value.taskId === task.taskId) {
      selectedTask.value = null;
    }
  } catch (error) {
    console.error(`删除任务 ${task.taskId} 失败:`, error);
    alert('删除失败：' + (error && error.message ? error.message : error));
  } finally {
    isSendingCmd.value = false;
  }
}

async function handlePauseFile(taskId, relativePath) {
  // 乐观更新缓存中的文件状态
  const file = fileCache.value.find(f => f.relativePath === relativePath);
  if (file) file.status = 'PAUSED';

  isSendingCmd.value = true;
  try {
    await PauseFile(taskId, relativePath);
  } catch (error) {
    console.error(`暂停文件 ${relativePath} 失败:`, error);
    if (file) file.status = 'UPLOADING';
  } finally {
    isSendingCmd.value = false;
  }
}

async function handleResumeFile(taskId, relativePath) {
  const file = fileCache.value.find(f => f.relativePath === relativePath);
  if (file) file.status = 'UPLOADING';

  isSendingCmd.value = true;
  try {
    await ResumeFile(taskId, relativePath);
  } catch (error) {
    console.error(`恢复文件 ${relativePath} 失败:`, error);
    if (file) file.status = 'PAUSED';
  } finally {
    isSendingCmd.value = false;
  }
}

function handleSort(key) {
  if (sortKey.value === key) {
    sortDirection.value = sortDirection.value === 'asc' ? 'desc' : 'asc';
  } else {
    sortKey.value = key;
    sortDirection.value = 'asc';
  }
  // 排序变化后重新从头加载
  scrollTop.value = 0;
  if (fileListRef.value) fileListRef.value.scrollTop = 0;
  loadFiles(0, PAGE_SIZE * 2);
}

function goBackToList() {
  selectedTask.value = null;
  sortKey.value = "fileName";
  sortDirection.value = 'asc';
  if (refreshTimer) {
    clearInterval(refreshTimer);
    refreshTimer = null;
  }
}

function formatSize(bytes) {
  if (!bytes || bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

function getStatusText(status) {
  const statusMap = {
    "UPLOADING": "上传中",
    "PAUSED": "已暂停",
    "QUEUED": "排队中",
    "COMPLETED": "已完成",
    "MERGING": "合并中",
    "FAILED": "失败",
    "PARTIAL_COMPLETED": "部分完成",
    "INTERRUPTED": "已中断",
  };
  return statusMap[status] || status;
}
</script>

<style scoped>
.dashboard {
  display: flex;
  flex-direction: column;
  height: 100vh;
  background-color: #f9fafb;
  color: #1f2937;
  overflow: hidden;
}

.dashboard-header {
  padding: 16px 24px;
  background-color: #ffffff;
  border-bottom: 1px solid #e5e7eb;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.05);
  flex-shrink: 0;
  /* [!! 修改 !!] 移除原来的 padding-bottom，显得更紧凑 */
  padding-bottom: 16px;
}

.header-stats {
  display: flex;
  gap: 32px; /* 增加间距 */
  align-items: center;
}

/* 顶部行：左侧统计 + 右侧后端连接卡片 */
.header-top-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
}

/* ===== 配置面板入口（齿轮图标） ===== */
.settings-entry-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 38px;
  height: 38px;
  flex-shrink: 0;
  padding: 0;
  border: 1px solid #e2e8f0;
  border-radius: 10px;
  background: #ffffff;
  color: #64748b;
  cursor: pointer;
  transition: color 0.18s, border-color 0.18s, background 0.18s, transform 0.18s;
}

.settings-entry-btn:hover {
  color: #4f6ef7;
  border-color: #4f6ef7;
  background: #eef2ff;
  transform: rotate(45deg);
}

/* ===== 当前连接后端 卡片 ===== */
.backend-conn-card {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 240px;
  max-width: 360px;
  margin-left: auto; /* 靠右排列，紧贴右侧的设置按钮 */
  padding: 8px 14px;
  border: 1px solid #e5e7eb;
  border-radius: 10px;
  background: #f8fafc;
  transition: border-color 0.15s, box-shadow 0.15s;
}
/* 用 URI 传入的覆盖地址时，用蓝色强调，提示当前不是配置文件里的地址 */
.backend-conn-card.is-override {
  background: #eff6ff;
  border-color: #bfdbfe;
}

.backend-conn-head {
  display: flex;
  align-items: center;
  gap: 6px;
  color: #6b7280;
}

.backend-conn-label {
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.03em;
}

.backend-conn-source {
  margin-left: auto;
  font-size: 11px;
  font-weight: 600;
  padding: 1px 8px;
  border-radius: 999px;
  line-height: 1.6;
}
.backend-conn-source.src-config {
  background: #e2e8f0;
  color: #475569;
}
.backend-conn-source.src-uri {
  background: #dbeafe;
  color: #1d4ed8;
}

.backend-conn-url {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 14px;
  font-weight: 600;
  color: #1e293b;
  word-break: break-all;
  line-height: 1.4;
}
.backend-conn-card.is-override .backend-conn-url {
  color: #1e40af;
}

.stat-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.stat-item .label {
  font-size: 13px;
  color: #6b7280;
  font-weight: 500;
  text-transform: uppercase;
  letter-spacing: 0.05em; /* 增加字间距 */
}

.stat-item .value {
  font-size: 22px; /* 稍微加大字体 */
  font-weight: 600;
  color: #111827;
}

.stat-item .value.speed {
  color: #3b82f6;
  font-weight: 700;
}

/* [!! 移除 !!] 原来的顶部监控样式 .monitor-panel 等 */

/* [!! 新增 !!] 底部系统监控栏样式 */
/* ===== 端点状态面板 ===== */
.endpoint-panel {
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px solid #e5e7eb;
}

.endpoint-info {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.endpoint-status-tag {
  display: inline-flex;
  align-items: center;
  border-radius: 6px;
  overflow: hidden;
  border: 1px solid #e5e7eb;
}
.endpoint-status-tag.interactive {
  cursor: pointer;
  transition: transform 0.15s, box-shadow 0.15s;
}
.endpoint-status-tag.interactive:hover {
  transform: translateY(-1px);
  box-shadow: 0 2px 4px rgba(0,0,0,0.05);
}

.tag-status-indicator {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 4px 10px;
  font-size: 13px;
  font-weight: 600;
  font-family: monospace;
  border-right: 1px solid #e5e7eb;
}

.endpoint-status-tag.is-running .tag-status-indicator {
  background: #dcfce7;
  color: #166534;
}

.endpoint-status-tag.is-stopped .tag-status-indicator {
  background: #fee2e2;
  color: #991b1b;
}

.app-log-indicator {
  background: #f1f5f9;
  color: #475569;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}

.trace-indicator {
  background: #eef2ff;
  color: #4f46e5;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}

.tag-action {
  padding: 4px 10px;
  font-size: 12px;
  color: #2563eb;
  font-weight: 500;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  background: #fff;
}

/* 移除原来旧的 endpoint-history-btn */
.history-count {
  background: #2563eb;
  color: #fff;
  font-size: 11px;
  font-weight: 700;
  border-radius: 999px;
  padding: 0 5px;
  min-width: 16px;
  text-align: center;
}

/* ===== 弹窗通用 ===== */
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}

.modal-box {
  background: #fff;
  border-radius: 10px;
  width: 700px;
  max-width: 92vw;
  max-height: 72vh;
  display: flex;
  flex-direction: column;
  box-shadow: 0 20px 60px rgba(0,0,0,0.2);
  overflow: hidden;
}

.modal-box-fullscreen {
  width: 100vw;
  height: 100vh;
  max-width: 100vw;
  max-height: 100vh;
  border-radius: 0;
}

.modal-header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 14px 20px;
  border-bottom: 1px solid #e5e7eb;
  font-weight: 600;
  font-size: 14px;
  color: #111827;
  flex-shrink: 0;
}

.modal-close {
  background: none;
  border: none;
  cursor: pointer;
  font-size: 14px;
  color: #6b7280;
  padding: 2px 6px;
  border-radius: 4px;
}
.modal-close:hover { background: #f3f4f6; color: #111827; }

.modal-body {
  flex: 1;
  min-height: 0;
  padding: 16px 20px;
  overflow-y: auto;
}

.meta-empty {
  color: #9ca3af;
  font-size: 13px;
  text-align: center;
  padding: 32px 0;
}

.meta-summary {
  font-size: 12px;
  color: #6b7280;
  margin-bottom: 12px;
}

.meta-table-wrap {
  overflow-x: auto;
}

.meta-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.meta-table th {
  text-align: center;
  padding: 7px 10px;
  background: #f9fafb;
  color: #374151;
  border-bottom: 1px solid #e5e7eb;
  font-weight: 600;
}

.meta-table td {
  padding: 7px 10px;
  border-bottom: 1px solid #f3f4f6;
  color: #1f2937;
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-align: center;
}

.meta-table tr:last-child td { border-bottom: none; }

.history-row { cursor: pointer; }
.history-row:hover td { background: #f0f9ff; }

.td-action {
  color: #2563eb;
  font-weight: 500;
  white-space: nowrap;
}

.back-btn {
  background: none;
  border: none;
  color: #2563eb;
  cursor: pointer;
  font-size: 13px;
  padding: 0 0 12px 0;
  display: block;
}
.back-btn:hover { text-decoration: underline; }
/* ===== 端点样式结束 ===== */

.dashboard-footer {
  flex-shrink: 0;
  background-color: #1e293b; /* Uniform dark theme background */
  padding: 0; /* Full bleed */
  display: flex;
  justify-content: space-between;
  border-top: 1px solid #334155;
  height: 54px;
}

.footer-modules {
  display: flex;
  width: 100%;
  gap: 0; /* No gap */
}

.footer-module {
  flex: 1; /* Expand to fill space equally */
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 0 16px;
  border-radius: 0;
  cursor: pointer;
  transition: background-color 0.2s ease;
  user-select: none;
  color: #f8fafc;
  border-right: 1px solid rgba(255, 255, 255, 0.08); /* Separator line */
}
.footer-module:last-child {
  border-right: none;
}

.footer-module:active {
  background-color: #0f172a;
}

.footer-module:hover {
  background-color: #334155;
}

.module-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  color: #94a3b8;
  transition: color 0.2s ease;
}

.footer-module:hover .module-icon {
  color: #60a5fa; /* Blue icon on hover */
}

.footer-module.warning {
  background-color: #7f1d1d;
}
.footer-module.warning:hover {
  background-color: #991b1b;
}

.module-info {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
}

.module-name {
  font-size: 11px;
  color: #94a3b8;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.module-value {
  font-size: 14px;
  font-weight: 700;
  color: #f8fafc;
  font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
}

/* [!! 新增结束 !!] */

.dashboard-main {
  flex: 1;
  overflow-y: auto;
  padding: 24px;
  /* [!! 修改 !!] 增加底部 padding 防止内容被 footer 遮挡 */
  padding-bottom: 24px;
}

.view-title {
  font-size: 22px;
  font-weight: 600;
  color: #111827;
  margin-top: 0;
  margin-bottom: 20px;
}

/* 任务列表 Tab：上传任务 / 历史记录 */
.task-tabs {
  display: flex;
  gap: 4px;
  margin: 0 0 16px 0;
  border-bottom: 1px solid #e5e7eb;
}
.task-tab {
  background: transparent;
  border: none;
  padding: 10px 18px;
  font-size: 15px;
  font-weight: 500;
  color: #6b7280;
  cursor: pointer;
  border-bottom: 2px solid transparent;
  transition: color 120ms ease, border-color 120ms ease;
}
.task-tab:hover {
  color: #111827;
}
.task-tab.active {
  color: #2563eb;
  border-bottom-color: #2563eb;
}

.empty-state {
  text-align: center;
  padding: 60px 0;
  font-size: 16px;
  color: #6b7280;
  background-color: #fff;
  border-radius: 8px;
  border: 1px solid #e5e7eb;
}

/* 任务列表 */
.task-list {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 16px;
}

.task-item {
  background-color: #fff;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  padding: 16px;
  cursor: pointer;
  transition: all 0.2s ease;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.03);
  display: flex;
  flex-direction: column;
}

.task-item:hover {
  border-color: #d1d5db;
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.05), 0 2px 4px -1px rgba(0, 0, 0, 0.03);
  transform: translateY(-2px);
}

.task-info {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}

.task-name {
  font-size: 16px;
  font-weight: 600;
  color: #1f2937;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.task-status {
  font-size: 12px;
  font-weight: 500;
  padding: 3px 8px;
  border-radius: 12px;
  color: #fff;
}

.task-status.uploading, .task-status.merging {
  background-color: #3b82f6;
}

.task-status.queued {
  background-color: #f59e0b;
}

.task-status.paused {
  background-color: #ef4444;
}

.task-status.partial_completed {
  background-color: #f59e0b;
}

.task-status.completed {
  background-color: #10b981;
}

.task-status.failed {
  background-color: #4b5563;
}

.task-failed-count {
  color: #ef4444;
  font-weight: 600;
}

.task-action-btn.retry {
  background-color: #fef3c7;
  color: #d97706;
}

.task-action-btn.retry:hover:not(:disabled) {
  background-color: #fde68a;
  color: #b45309;
}

.task-progress-details {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  color: #6b7280;
  margin-bottom: 8px;
}

.task-actions {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid #f3f4f6;
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.task-action-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 14px;
  font-size: 13px;
  font-weight: 500;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.2s;
}

.task-action-btn.pause {
  background-color: #fef3c7;
  color: #d97706;
}

.task-action-btn.pause:hover:not(:disabled) {
  background-color: #fde68a;
  color: #b45309;
}

.task-action-btn.resume {
  background-color: #dbeafe;
  color: #3b82f6;
}

.task-action-btn.delete {
  background-color: #fee2e2;
  color: #dc2626;
}

.task-action-btn.delete:hover:not(:disabled) {
  background-color: #fecaca;
  color: #b91c1c;
}

.task-action-btn.resume:hover:not(:disabled) {
  background-color: #bfdbfe;
  color: #2563eb;
}

.progress-bar-container {
  width: 100%;
  height: 8px;
  background-color: #e5e7eb;
  border-radius: 4px;
  overflow: hidden;
  margin-top: auto;
}

.progress-bar-container.small {
  height: 6px;
}

.progress-bar {
  height: 100%;
  background-color: #3b82f6;
  border-radius: 4px;
  transition: width 0.3s ease;
}

.task-header-section {
  margin-bottom: 24px;
  padding-bottom: 20px;
  border-bottom: 2px solid #e5e7eb;
}

.back-button {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  font-size: 14px;
  background-color: #ffffff;
  border: 1px solid #d1d5db;
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.2s;
  color: #4b5563;
  font-weight: 500;
  margin-bottom: 16px;
}

.back-button:hover {
  background-color: #f9fafb;
  border-color: #9ca3af;
  color: #1f2937;
}

.back-button svg {
  transition: transform 0.2s;
}

.back-button:hover svg {
  transform: translateX(-2px);
}

.task-title-wrapper {
  display: flex;
  align-items: center;
  gap: 12px;
}

.task-icon {
  flex-shrink: 0;
  color: #3b82f6;
}

.task-title {
  margin: 0;
  font-size: 24px;
  font-weight: 700;
  color: #111827;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  line-height: 1.3;
}

.file-list-table {
  background-color: #ffffff;
  border-radius: 12px;
  overflow: hidden;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08);
  display: flex;
  flex-direction: column;
  max-height: calc(100vh - 250px);
}

.file-list-column-headers {
  display: flex;
  align-items: center;
  padding: 16px 20px;
  background: linear-gradient(to bottom, #f8fafc, #f1f5f9);
  border-bottom: 2px solid #e2e8f0;
  position: sticky;
  top: 0;
  z-index: 10;
  flex-shrink: 0;
}

.header-col {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  user-select: none;
  padding: 8px 12px;
  border-radius: 6px;
  transition: all 0.2s ease;
  font-size: 13px;
  font-weight: 600;
  color: #64748b;
  letter-spacing: 0.02em;
}

.header-col:hover {
  background-color: rgba(59, 130, 246, 0.08);
  color: #3b82f6;
}

.header-col.active-sort {
  color: #3b82f6;
  background-color: rgba(59, 130, 246, 0.1);
}

.header-icon {
  flex-shrink: 0;
  opacity: 0.6;
  transition: opacity 0.2s;
}

.header-col:hover .header-icon,
.header-col.active-sort .header-icon {
  opacity: 1;
}

.header-text {
  flex: 1;
}

.sort-indicator {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  font-size: 14px;
  font-weight: 600;
  color: #3b82f6;
}

.neutral-sort {
  color: #cbd5e1;
  font-weight: 400;
}

.header-col:hover .neutral-sort {
  color: #94a3b8;
}

.header-col-name {
  flex: 2;
  min-width: 0;
  margin-right: 8px;
}

.header-col-path {
  flex: 3;
  min-width: 0;
  margin-right: 8px;
}

.header-col-progress {
  flex-basis: 340px;
  flex-shrink: 0;
}

.file-list {
  background-color: #fff;
  overflow-y: auto;
  flex: 1;
}

.virtual-spacer-top,
.virtual-spacer-bottom {
  width: 100%;
  flex-shrink: 0;
}

.file-list-info {
  padding: 8px 20px;
  font-size: 12px;
  color: #6b7280;
  background-color: #f8fafc;
  border-bottom: 1px solid #e5e7eb;
  display: flex;
  justify-content: space-between;
  flex-shrink: 0;
}

.loading-hint {
  color: #3b82f6;
  font-weight: 500;
}

.file-item {
  display: flex;
  align-items: center;
  padding: 14px 20px;
  border-bottom: 1px solid #f1f5f9;
  transition: background-color 0.15s;
}

.file-item:hover {
  background-color: #f8fafc;
}

.file-item:last-child {
  border-bottom: none;
}

.file-name {
  font-size: 14px;
  font-weight: 500;
  color: #1f2937;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 2;
  min-width: 0;
  margin-right: 16px;
}

.file-path {
  font-size: 13px;
  color: #6b7280;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 3;
  min-width: 0;
  margin-right: 16px;
}

.file-path.clickable {
  cursor: pointer;
}

.file-path.clickable:hover {
  color: #3b82f6;
  text-decoration: underline;
}

.file-progress-details {
  flex-basis: 340px;
  flex-shrink: 0;
  display: grid;
  grid-template-columns: 70px 1fr 40px;
  grid-template-rows: auto auto;
  align-items: center;
  gap: 4px 8px;
  margin-bottom: 0;
}

.file-status {
  font-size: 13px;
  font-weight: 600;
}

.file-status.uploading, .file-status.merging {
  color: #2563eb;
}

.file-status.queued {
  color: #d97706;
}

.file-status.paused {
  color: #dc2626;
}

.file-status.completed {
  color: #059669;
}

.file-status.failed {
  color: #4b5563;
}

.file-size {
  font-size: 13px;
  color: #6b7280;
  text-align: right;
}

.file-controls {
  grid-row: 1 / span 1;
  grid-column: 3 / span 1;
  display: flex;
  justify-content: center;
  align-items: center;
}

.file-action-btn {
  width: 28px;
  height: 28px;
  padding: 0;
  font-size: 14px;
  border: none;
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s;
}

.file-action-btn.pause {
  background-color: #fef3c7;
  color: #d97706;
}

.file-action-btn.pause:hover:not(:disabled) {
  background-color: #fde68a;
}

.file-action-btn.resume {
  background-color: #dbeafe;
  color: #3b82f6;
}

.file-action-btn.resume:hover:not(:disabled) {
  background-color: #bfdbfe;
}

.file-progress-details .progress-bar-container {
  grid-column: 1 / span 3;
}

/* ===== HTTP 请求日志 ===== */
.http-log-filters {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 12px;
  padding: 10px 12px;
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  border-radius: 6px;
}

.filter-input {
  flex: 1 1 220px;
  min-width: 180px;
  padding: 6px 10px;
  font-size: 13px;
  border: 1px solid #cbd5e1;
  border-radius: 4px;
  outline: none;
}

.filter-input:focus {
  border-color: #3b82f6;
}

.filter-select {
  padding: 6px 10px;
  font-size: 13px;
  border: 1px solid #cbd5e1;
  border-radius: 4px;
  background: #fff;
  cursor: pointer;
}

.auto-refresh-label {
  margin-left: auto;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: #64748b;
  user-select: none;
}

.http-log-table {
  font-size: 13px;
}

.http-log-table th,
.http-log-table td {
  padding: 8px 10px;
}

.http-log-table .query-suffix {
  color: #94a3b8;
  font-size: 12px;
}

.http-method {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.02em;
}

.method-get { background: #dbeafe; color: #1d4ed8; }
.method-post { background: #dcfce7; color: #15803d; }
.method-put { background: #fef3c7; color: #b45309; }
.method-delete { background: #fee2e2; color: #b91c1c; }
.method-options { background: #ede9fe; color: #6d28d9; }
.method-head { background: #f1f5f9; color: #475569; }

.http-status {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.status-2xx { background: #dcfce7; color: #166534; }
.status-3xx { background: #e0e7ff; color: #3730a3; }
.status-4xx { background: #fef3c7; color: #92400e; }
.status-5xx { background: #fee2e2; color: #991b1b; }
.status-pending { background: #f1f5f9; color: #475569; }

.pagination {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 12px 0 4px;
  font-size: 13px;
  color: #475569;
}

.page-btn {
  padding: 6px 14px;
  font-size: 13px;
  background: #f1f5f9;
  color: #1e293b;
  border: 1px solid #e2e8f0;
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.15s;
}

.page-btn:hover:not(:disabled) {
  background: #e2e8f0;
}

.page-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.page-info {
  font-variant-numeric: tabular-nums;
}

.page-size-select {
  margin-left: 12px;
  padding: 5px 8px;
  font-size: 12px;
  border: 1px solid #e2e8f0;
  border-radius: 6px;
  background: #fff;
  cursor: pointer;
}

.http-log-detail {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.http-log-detail .meta-summary {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  padding: 10px 12px;
  background: #f8fafc;
  border-radius: 6px;
  border: 1px solid #e2e8f0;
}

.http-log-detail .detail-path {
  font-family: ui-monospace, "SFMono-Regular", Menlo, monospace;
  font-size: 13px;
  color: #1e293b;
  word-break: break-all;
  flex: 1 1 auto;
}

.http-log-detail .detail-meta {
  font-size: 12px;
  color: #64748b;
  width: 100%;
}

.detail-section {
  background: #fafafa;
  border: 1px solid #e5e7eb;
  border-radius: 6px;
  padding: 10px 12px;
}

.detail-section-title {
  font-size: 13px;
  font-weight: 600;
  color: #334155;
  margin-bottom: 8px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.truncated-badge {
  display: inline-block;
  padding: 2px 8px;
  font-size: 11px;
  font-weight: 500;
  background: #fef3c7;
  color: #92400e;
  border-radius: 4px;
}

.raw-body {
  margin: 0;
  padding: 8px;
  font-family: ui-monospace, "SFMono-Regular", Menlo, monospace;
  font-size: 12px;
  color: #1e293b;
  background: #fff;
  border: 1px solid #e5e7eb;
  border-radius: 4px;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 400px;
  overflow-y: auto;
}

/* vue-json-pretty 微调 */
.detail-section :deep(.vjs-tree) {
  font-size: 12px;
}

/* ===== 应用日志弹窗 ===== */
.app-log-modal {
  /* Inherits from .modal-box-fullscreen */
}

.app-log-body {
  display: flex;
  flex: 1;
  gap: 0;
  padding: 0 !important;
  min-height: 0;
  overflow: hidden;
}

.app-log-sidebar {
  width: 220px;
  flex-shrink: 0;
  background: #f8fafc;
  border-right: 1px solid #e2e8f0;
  overflow-y: auto;
  padding: 12px 0;
}

.sidebar-title {
  padding: 0 16px 10px;
  font-size: 12px;
  font-weight: 600;
  color: #64748b;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.app-log-file-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.app-log-file-item {
  padding: 10px 16px;
  cursor: pointer;
  border-left: 3px solid transparent;
  transition: background 0.15s, border-color 0.15s;
}

.app-log-file-item:hover {
  background: #eff6ff;
}

.app-log-file-item.active {
  background: #e0e7ff;
  border-left-color: #3b82f6;
}

.app-log-file-date {
  font-size: 14px;
  font-weight: 600;
  color: #1e293b;
  font-variant-numeric: tabular-nums;
}

.app-log-file-meta {
  margin-top: 2px;
  font-size: 11px;
  color: #64748b;
  font-variant-numeric: tabular-nums;
}

.app-log-content {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  min-width: 0;
  background: #fff;
}

.app-log-toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  padding: 12px 16px;
  background: #f8fafc;
  border-bottom: 1px solid #e2e8f0;
}

.app-log-stats {
  margin-left: auto;
  font-size: 12px;
  color: #64748b;
  font-variant-numeric: tabular-nums;
}

.app-log-pane {
  flex: 1 1 auto;
  overflow-y: auto;
  padding: 8px 0;
  background: #ffffff;
  color: #1e293b;
  font-family: ui-monospace, "SFMono-Regular", Menlo, Consolas, monospace;
  font-size: 12.5px;
  line-height: 1.55;
}

.app-log-line {
  display: flex;
  align-items: flex-start;
  padding: 1px 16px;
}

.app-log-line:hover {
  background: #f8fafc;
}

.app-log-line-no {
  flex-shrink: 0;
  width: 56px;
  text-align: right;
  margin-right: 14px;
  color: #64748b;
  user-select: none;
  font-variant-numeric: tabular-nums;
}

.app-log-line-text {
  flex: 1 1 auto;
  text-align: left;
  white-space: pre-wrap;
  word-break: break-all;
}

/* 查询关键字高亮 */
.app-log-hl {
  background: #fde68a;
  color: #92400e;
  border-radius: 2px;
  padding: 0 1px;
}

.app-log-line.log-level-error .app-log-line-text {
  color: #ef4444;
}

.app-log-line.log-level-warn .app-log-line-text {
  color: #f59e0b;
}

.app-log-line.log-level-info .app-log-line-text {
  color: #2563eb;
}

.app-log-loadmore {
  text-align: center;
  padding: 12px 16px;
}

.app-log-loading-hint {
  color: #94a3b8;
  font-size: 13px;
}

.app-log-end {
  text-align: center;
  padding: 12px 0;
  font-size: 12px;
  color: #64748b;
}
/* ===== Goroutines 弹窗 ===== */
.goroutines-body {
  display: flex;
  flex: 1;
  gap: 0;
  padding: 0 !important;
  min-height: 0;
  overflow: hidden;
}

.goroutines-sidebar {
  width: 260px;
  flex-shrink: 0;
  background: #f8fafc;
  border-right: 1px solid #e2e8f0;
  overflow-y: auto;
  padding: 12px 0;
}

.goroutines-list {
  list-style: none;
  padding: 0;
  margin: 0;
}

.goroutines-item {
  padding: 8px 16px;
  cursor: pointer;
  border-left: 3px solid transparent;
  transition: all 0.2s;
  border-bottom: 1px solid #f1f5f9;
}

.goroutines-item:hover {
  background: #f1f5f9;
}

.goroutines-item.active {
  background: #eff6ff;
  border-left-color: #3b82f6;
}

.goroutine-meta {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.goroutine-id {
  font-size: 13px;
  font-weight: 600;
  color: #1e293b;
}

.goroutine-state {
  font-size: 11px;
  padding: 2px 6px;
  border-radius: 4px;
  font-weight: 500;
}

.state-running { background: #dcfce7; color: #166534; }
.state-waiting { background: #fef3c7; color: #92400e; }

.goroutines-content {
  flex: 1;
  display: flex;
  flex-direction: column;
  background: #fff;
  min-width: 0;
  padding: 16px 20px;
  overflow-y: auto;
}

.goroutine-stack-wrapper {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.goroutine-stack {
  max-height: none;
  overflow-y: visible;
  font-size: 13px;
  line-height: 1.5;
  background: #f8fafc;
  border-color: #cbd5e1;
}
/* ===== Memory View iOS Style (Light) ===== */
.memory-fullscreen-box {
  background-color: #ffffff !important;
  color: #1e293b;
  display: flex;
  flex-direction: column;
}

.memory-modal-header {
  border-bottom: 1px solid #e2e8f0;
  background: #f8fafc;
}

.memory-modal-body {
  flex: 1;
  padding: 32px 40px !important;
  overflow: hidden;
  background-color: #ffffff;
}

.ios-storage-container-light {
  display: flex;
  flex-direction: column;
  gap: 24px;
  width: 100%;
}

.ios-storage-header-light {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
}

.ios-storage-title-light {
  font-size: 28px;
  font-weight: 700;
  color: #0f172a;
  margin: 0;
}

.ios-storage-usage-light {
  font-size: 16px;
  color: #64748b;
  font-weight: 500;
}

.ios-storage-bar-light {
  height: 32px;
  border-radius: 16px;
  background-color: #f1f5f9;
  display: flex;
  overflow: hidden;
  gap: 3px; /* segment separator */
}

.segment-alloc-light {
  background-color: #3b82f6; /* Blue for Alloc */
  transition: width 0.5s cubic-bezier(0.4, 0, 0.2, 1);
}

.segment-free-light {
  background-color: #cbd5e1; /* Light Grey for Free */
  transition: width 0.5s cubic-bezier(0.4, 0, 0.2, 1);
}

.ios-storage-legend-light {
  display: flex;
  flex-wrap: wrap;
  gap: 32px;
  margin-top: 8px;
}

.legend-item-light {
  display: flex;
  align-items: center;
  gap: 8px;
}

.legend-dot-light {
  width: 12px;
  height: 12px;
  border-radius: 50%;
}

.dot-alloc-light { background-color: #3b82f6; }
.dot-free-light { background-color: #cbd5e1; }

.legend-name-light {
  color: #475569;
  font-size: 14px;
  font-weight: 500;
}

.memory-stats-grid-light {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 20px;
  margin-top: 40px;
  padding-top: 32px;
  border-top: 1px solid #f1f5f9;
}

.memory-stat-card-light {
  background: #f8fafc;
  padding: 20px;
  border-radius: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  border: 1px solid #e2e8f0;
  transition: transform 0.2s, box-shadow 0.2s;
}

.memory-stat-card-light:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(0,0,0,0.05);
}

.stat-card-label-light {
  color: #64748b;
  font-size: 14px;
  font-weight: 500;
}

.stat-card-value-light {
  font-size: 22px;
  font-weight: 700;
  color: #0f172a;
}

/* ===== Modal Action Buttons Design ===== */
.modal-header-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

.action-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  background: transparent;
  color: #3b82f6;
  border: 1px solid transparent;
  padding: 6px 12px;
  border-radius: 6px;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
}

.action-btn svg {
  stroke: #3b82f6;
  transition: all 0.2s;
}

.action-btn:hover:not(:disabled) {
  background: #eff6ff;
  border-color: #bfdbfe;
}

.action-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
  filter: grayscale(100%);
}

.btn-return {
  display: flex;
  align-items: center;
  justify-content: center;
  background: #3b82f6;
  color: #ffffff;
  border: none;
  padding: 6px 20px;
  border-radius: 6px;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  box-shadow: 0 2px 4px rgba(59, 130, 246, 0.25);
  transition: all 0.2s ease;
  margin-left: 8px;
}

.btn-return:hover {
  background: #2563eb;
  box-shadow: 0 4px 8px rgba(59, 130, 246, 0.35);
  transform: translateY(-1px);
}

.btn-return:active {
  transform: translateY(1px);
}
</style>
/**
 * 任务级链路追踪 API 封装。
 *
 * 设计：
 *   - 在 Wails 桌面端运行时通过 wailsjs/go/app/App 暴露的方法调用（同步类型，无 HTTP）
 *   - 字段命名与 internal/tracer 的 JSON tag 保持完全一致（traceId / spanId / startMs ...）
 *   - 所有错误由调用方 try/catch 处理；本层不吞错误，只做参数规整与默认值兜底
 *
 * 注意：本文件不依赖任何 UI 库，可被任意 Vue 组件复用。
 */

import {
  ListTracedTasks,
  GetTraceTopSpans,
  GetSpanChildren,
  GetTraceAllSpans,
  DeleteTrace,
  ListTraceModules,
  SetTraceModuleEnabled,
  SetTraceGlobalEnabled,
  GetTraceConfig,
  UpdateTraceConfig,
  CleanupTraces,
} from '../../../wailsjs/go/app/App';

/**
 * 分页查询追踪任务列表。
 *
 * @param {Object} params
 * @param {string} [params.traceType='']  "upload" / "scan" / "" (all)
 * @param {string} [params.taskId='']     精确匹配；空字符串忽略
 * @param {number} [params.fromMs=0]      起始时间（毫秒，0 表示不限）
 * @param {number} [params.toMs=0]        结束时间（毫秒，0 表示不限）
 * @param {number} [params.page=1]        1-based
 * @param {number} [params.size=50]       每页条数（最大 200）
 * @returns {Promise<{total:number, page:number, size:number, items:Array}>}
 */
export async function listTracedTasks(params = {}) {
  const {
    traceType = '',
    taskId = '',
    fromMs = 0,
    toMs = 0,
    page = 1,
    size = 50,
  } = params;
  return await ListTracedTasks(traceType, taskId, fromMs, toMs, page, size);
}

/** 获取某 trace 的所有顶层 span（按 startMs 升序） */
export async function getTraceTopSpans(traceId) {
  if (!traceId) return [];
  return (await GetTraceTopSpans(traceId)) || [];
}

/** 获取某 span 的直接子 span */
export async function getSpanChildren(spanId) {
  if (!spanId) return [];
  return (await GetSpanChildren(spanId)) || [];
}

/**
 * 获取某 trace 的所有 span（平铺）。
 *
 * @returns {Promise<{spans:Array, truncated:boolean}>}
 */
export async function getTraceAllSpans(traceId) {
  if (!traceId) return { spans: [], truncated: false };
  return await GetTraceAllSpans(traceId);
}

/** 删除某 trace 的所有 span，返回删除条数 */
export async function deleteTrace(traceId) {
  if (!traceId) return 0;
  return await DeleteTrace(traceId);
}

// ─── 模块 / 全局开关 / 配置 ───

/**
 * @returns {Promise<{global:boolean, modules:Array<{code,name,description,enabled}>}>}
 */
export async function listTraceModules() {
  return await ListTraceModules();
}

/** 切换某模块开关。失败时抛错。 */
export async function setTraceModuleEnabled(code, enabled) {
  await SetTraceModuleEnabled(code, !!enabled);
}

/** 切换全局总开关。失败时抛错。 */
export async function setTraceGlobalEnabled(enabled) {
  await SetTraceGlobalEnabled(!!enabled);
}

/**
 * @returns {Promise<{traceSamplingThreshold,traceMaxAgeDays,traceMaxRows,traceVacuumThresholdMB}>}
 */
export async function getTraceConfig() {
  return await GetTraceConfig();
}

/**
 * 部分更新追踪配置。任意字段传 null/undefined 表示不修改；传 0 或正数视为期望值。
 *
 * 后端 Wails Bind 不支持指针，约定 -1 表示"不修改"。
 *
 * @param {Object} patch
 * @param {number} [patch.traceSamplingThreshold]
 * @param {number} [patch.traceMaxAgeDays]
 * @param {number} [patch.traceMaxRows]
 * @param {number} [patch.traceVacuumThresholdMB]
 * @returns {Promise<{traceSamplingThreshold,traceMaxAgeDays,traceMaxRows,traceVacuumThresholdMB}>}
 */
export async function updateTraceConfig(patch = {}) {
  const norm = (v) => {
    if (v === null || v === undefined) return -1;
    const n = Number(v);
    if (!Number.isFinite(n) || n < 0) return -1;
    return Math.floor(n);
  };
  return await UpdateTraceConfig(
    norm(patch.traceSamplingThreshold),
    norm(patch.traceMaxAgeDays),
    norm(patch.traceMaxRows),
    norm(patch.traceVacuumThresholdMB),
  );
}

/** 立即触发清理。参数 <=0 时使用 config 当前值。返回删除条数。 */
export async function cleanupTraces(maxAgeDays = 0, maxRows = 0) {
  return await CleanupTraces(maxAgeDays, maxRows);
}


/**
 * 订阅某 trace 的实时 SSE 事件流。
 *
 * 设计：
 *   - 直接走 HTTP 端口（默认 :34567），与上传进度 SSE 通道完全独立
 *   - Wails 客户端和"浏览器查看器"两种场景都用同一份代码
 *   - 自动重连：连接断开后 3s 重试；连续失败 10 次后停止并通过 onError 抛出
 *
 * @param {string} traceId
 * @param {Object} handlers
 * @param {(span: object) => void} handlers.onInsert  收到 op=insert 时回调
 * @param {(span: object) => void} handlers.onUpdate  收到 op=update 时回调
 * @param {(err: Error) => void}   [handlers.onError] 重连失败超过上限时回调
 * @returns {() => void}  调用返回值即可关闭订阅
 */
export function subscribeTraceStream(traceId, handlers = {}) {
  if (!traceId) return () => {};
  const { onInsert, onUpdate, onError } = handlers;

  const port = (typeof window !== 'undefined' && window.__TRACE_PORT__) || 34567;
  const url = `http://localhost:${port}/task-traces/${encodeURIComponent(traceId)}/stream`;

  let es = null;
  let closed = false;
  let retryCount = 0;
  const maxRetry = 10;
  const retryDelayMs = 3000;
  let retryTimer = null;

  const connect = () => {
    if (closed) return;
    try {
      es = new EventSource(url);
    } catch (e) {
      // 浏览器不支持？直接抛出。
      if (onError) onError(e);
      return;
    }

    es.addEventListener('open', () => {
      retryCount = 0;
    });

    es.addEventListener('span', (ev) => {
      try {
        const payload = JSON.parse(ev.data);
        if (!payload || !payload.span) return;
        if (payload.op === 'insert' && onInsert) onInsert(payload.span);
        else if (payload.op === 'update' && onUpdate) onUpdate(payload.span);
      } catch {
        // 静默忽略坏 payload；前端的"刷新"按钮可补救
      }
    });

    // ping 保活无需处理
    // 浏览器 EventSource 默认会自动重连；这里仍然手写一份，给"重试上限 + 错误回调"做兜底
    es.addEventListener('error', () => {
      if (closed) return;
      try { es.close(); } catch {}
      es = null;
      retryCount += 1;
      if (retryCount > maxRetry) {
        if (onError) onError(new Error('SSE 连接失败次数过多，已停止重连'));
        return;
      }
      retryTimer = setTimeout(connect, retryDelayMs);
    });
  };

  connect();

  return () => {
    closed = true;
    if (retryTimer) clearTimeout(retryTimer);
    if (es) {
      try { es.close(); } catch {}
      es = null;
    }
  };
}

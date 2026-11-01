import {ref} from 'vue';

/**
 * FileSelector 的「UI 偏好」单例。
 *
 * 为什么要放在组件外的模块作用域：
 * 客户端窗口是单例复用（Show/Hide，不新建窗口），而每次浏览器唤起 select-files
 * 时 App.vue 会用递增的 :key 强制 FileSelector 整体重新挂载 —— 这是「新会话零残留」
 * 的实现手段（详见 App.vue 中 selectorKey 的注释）。
 *
 * 重挂载会把组件内所有 ref 打回初始值，这对目录/勾选/遮罩正是我们要的，
 * 但视图模式与排序方式属于**用户偏好**，被重置会很突兀（用户切到网格视图，
 * 下次打开又变回列表）。模块级 ref 是 ES Module 单例，只随进程存活，
 * 因此能跨组件重挂载保留。
 *
 * 注意：只有「用户偏好」才适合放在这里。任何与某一次选择会话相关的状态
 * （当前路径、已选项、扫描阶段、路径补全等）都必须留在组件内，
 * 否则会重新引入本次修复要解决的残留问题。
 */

/** 列表 / 网格视图 */
export const viewMode = ref('list');

/** 排序字段：name | date | type | size */
export const sortKey = ref('name');

/** 排序方向：asc | desc */
export const sortDirection = ref('asc');

/**
 * 客户端界面版本标识 —— 单一真相源。
 *
 * 格式：v{年}-{月}-{日}_{当日序号}
 *   - 年/月/日 各取两位；序号从 1 开始，同一天内每出一个新构建就 +1
 *   - 例：v26-08-18_1、v26-08-18_2、v26-08-19_1
 *
 * 发版流程：改这一行 → `npm run build` → `wails build`。
 *
 * 这个值有两个出口，都是为了现场反馈时能一句话对账「你装的是哪个包」：
 *   1. 右下角常驻水印（见 App.vue 的 .version-watermark，覆盖全部视图）
 *   2. 客户端日志（App.vue 在 frontend:ready 握手时带过去，Go 侧记一行）
 *
 * 注意：这与 setup_inno/go.iss 里的 MyAppVersion 是两回事——那个是安装包版本，
 * Windows 要求 x.y 数字格式，装不下这里的日期序号格式，两者互不替代。
 */
export const APP_VERSION = 'v26-08-18_1';

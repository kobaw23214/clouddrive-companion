import {defineConfig} from 'vite'
import vue from '@vitejs/plugin-vue'
import path from 'path'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [vue()],
  build: {
    // 锁定到 Chromium 109：Win7/8/8.1 上 WebView2 被微软永久冻结在 109，
    // 这里显式设定编译目标，让构建期就暴露出 109 之后才有的语法/API，
    // 而不是等到旧系统上白屏才发现。
    //
    // 典型的 109 之后特性（请避免使用）：
    //   Array.prototype.toSorted / toReversed / with  → Chrome 110
    //   Object.groupBy                                → Chrome 117
    //   Array.fromAsync                               → Chrome 121
    //   原生 CSS 嵌套                                  → Chrome 112
    // 注意：build.target 只转译语法，不会为缺失的运行时 API 注入 polyfill。
    target: 'chrome109',
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src'),
      'wailsjs': path.resolve(__dirname, 'wailsjs')
    }
  }
})

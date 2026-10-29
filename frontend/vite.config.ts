import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

// 浏览器开发模式：`npm run dev` + `go run ./cmd/devserver`，
// /api 由 vite 代理到 devserver（与 Wails 运行时走同一套 App 方法）。
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: { '@': '/src' },
  },
  server: {
    port: 5275,
    proxy: {
      '/api': 'http://127.0.0.1:8175',
      // 文档图片（Markdown 本地图片）：devserver 的只读路由
      '/doc-assets': 'http://127.0.0.1:8175',
    },
  },
})

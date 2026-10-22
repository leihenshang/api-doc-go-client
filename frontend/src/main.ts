// 客户端前端入口：装配 Pinia、i18n，挂载根组件。
import { createPinia } from 'pinia'
import { createApp } from 'vue'
import App from './App.vue'
import { i18n } from './i18n'
import { initTheme } from './lib/theme'
// 随包内置中文字体（HarmonyOS Sans SC，Regular/Medium/Bold 常用字子集）：不依赖系统字体
// 字体已按 GB2312 + 界面用字子集化（约 2.9MB），生僻字回退系统字体
import '@/assets/fonts/harmonyos-sans-sc.css'
import './styles/base.css'

// 挂载前先上色：避免默认浅色样式闪一下（磁盘设置为准的校准发生在 settings.load()）
initTheme()

createApp(App).use(createPinia()).use(i18n).mount('#app')

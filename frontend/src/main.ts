// 客户端前端入口：装配 Pinia、i18n，挂载根组件。
import { createPinia } from 'pinia'
import { createApp } from 'vue'
import App from './App.vue'
import { i18n } from './i18n'
import { initTheme } from './lib/theme'
// 随包内置中文字体（Noto Sans SC 可变字体，SIL OFL）：不依赖系统字体，且提供真实字重（避免合成粗体）
import '@fontsource-variable/noto-sans-sc'
import './styles/base.css'

// 挂载前先上色：避免默认浅色样式闪一下（磁盘设置为准的校准发生在 settings.load()）
initTheme()

createApp(App).use(createPinia()).use(i18n).mount('#app')

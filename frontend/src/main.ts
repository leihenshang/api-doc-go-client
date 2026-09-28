// 客户端前端入口：装配 Pinia、i18n，挂载根组件。
import { createPinia } from 'pinia'
import { createApp } from 'vue'
import App from './App.vue'
import { i18n } from './i18n'
// 随包内置中文字体（Noto Sans SC 可变字体，SIL OFL）：不依赖系统字体，且提供真实字重（避免合成粗体）
import '@fontsource-variable/noto-sans-sc'
import './styles/base.css'

createApp(App).use(createPinia()).use(i18n).mount('#app')

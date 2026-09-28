// 客户端前端入口：装配 Pinia、i18n，挂载根组件。
import { createPinia } from 'pinia'
import { createApp } from 'vue'
import App from './App.vue'
import { i18n } from './i18n'
import './styles/base.css'

createApp(App).use(createPinia()).use(i18n).mount('#app')

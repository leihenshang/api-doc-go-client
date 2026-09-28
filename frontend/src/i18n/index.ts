// 客户端 i18n：v0.0.1 仅包含当前界面所需 key；命名规范与服务端一致（模块.语义），
// 后续把服务端语言包中两端口共用的命名空间（common/param/tree/varForm/varText/env…）搬进来。
import { createI18n } from 'vue-i18n'
import en from './locales/en-US'
import zh from './locales/zh-CN'

const saved = localStorage.getItem('client.lang')
const locale =
  saved === 'zh-CN' || saved === 'en-US'
    ? saved
    : navigator.language.toLowerCase().startsWith('zh')
      ? 'zh-CN'
      : 'en-US'

export const i18n = createI18n({
  legacy: false,
  locale,
  fallbackLocale: 'zh-CN',
  messages: { 'zh-CN': zh, 'en-US': en },
})

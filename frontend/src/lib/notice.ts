// 全局消息：naive-ui 的 useMessage 必须在 NMessageProvider 后代中调用，
// 而根组件 App.vue 自身的 setup 不是自己模板里 provider 的后代 ——
// 因此统一使用离散 API（独立于组件树），任何组件/模块都可直接导入。
import { createDiscreteApi, enUS, zhCN } from 'naive-ui'
import { computed } from 'vue'
import { i18n } from '@/i18n'

// 提示文案跟随当前界面语言
export const { message } = createDiscreteApi(['message'], {
  configProviderProps: computed(() => ({ locale: i18n.global.locale.value === 'en-US' ? enUS : zhCN })),
})

// 全局消息：naive-ui 的 useMessage 必须在 NMessageProvider 后代中调用，
// 而根组件 App.vue 自身的 setup 不是自己模板里 provider 的后代 ——
// 因此统一使用离散 API（独立于组件树），任何组件/模块都可直接导入。
import { i18n } from '@/i18n'
import { isDark } from '@/lib/theme'
import { createDiscreteApi, darkTheme, enUS, zhCN } from 'naive-ui'
import { computed } from 'vue'

// 提示文案跟随界面语言，配色跟随主题（离散 API 在组件树之外，需自行注入主题）
// message 供提示使用；dialog 供需要三选（保存/放弃/取消）的确认框使用（同源注入，样式一致）
export const { message, dialog } = createDiscreteApi(['message', 'dialog'], {
  configProviderProps: computed(() => ({
    locale: i18n.global.locale.value === 'en-US' ? enUS : zhCN,
    theme: isDark.value ? darkTheme : null,
  })),
})

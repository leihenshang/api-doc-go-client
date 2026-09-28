// 全局消息：naive-ui 的 useMessage 必须在 NMessageProvider 后代中调用，
// 而根组件 App.vue 自身的 setup 不是自己模板里 provider 的后代 ——
// 因此统一使用离散 API（独立于组件树），任何组件/模块都可直接导入。
// 注意：locale 使用 naive-ui 默认值（zh-CN）；跟随界面语言切换可在后续版本
// 通过 createDiscreteApi 的 configProviderProps 计算 ref 实现。
import { createDiscreteApi } from 'naive-ui'

export const { message } = createDiscreteApi(['message'])

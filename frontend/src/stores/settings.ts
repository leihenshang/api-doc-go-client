// 全局设置：网络策略 + 本地数据容量 + 界面（主题 / 缩放倍率 / 响应区位置）。
// 界面相关项需全局响应式，因此抽成 store，App.vue 直接消费。
// 主题另有 lib/theme.ts 负责写 DOM（含 localStorage 缓存），本 store 只负责与磁盘设置同步。
import { defineStore } from 'pinia'
import { api } from '@/lib/ipc'
import { applyTheme, isDark, ThemeLight, type ThemeMode } from '@/lib/theme'
import type { Settings } from '@/types'

const DEFAULTS: Settings = {
  insecureSsl: false,
  timeoutSec: 30,
  followRedirects: true,
  maxRedirects: 5,
  persistCookies: true,
  historyLimit: 200,
  uiScale: 1,
  responseLayout: 'right',
  responseSize: 44,
  theme: ThemeLight,
  proxyUrl: '',
  autoSave: false,
}

export const useSettingsStore = defineStore('settings', {
  state: () => ({
    form: { ...DEFAULTS } as Settings,
    loaded: false,
    // 本次会话已手动切过主题：磁盘值回来时不回滚用户的选择
    themeTouched: false,
  }),
  getters: {
    uiScale: (s) => s.form.uiScale,
    responseLayout: (s) => s.form.responseLayout,
    responseSize: (s) => s.form.responseSize,
    /** 编辑后自动写盘（默认关 = 手动保存模式） */
    autoSave: (s) => s.form.autoSave,
    isDark: () => isDark.value,
  },
  actions: {
    async load(): Promise<void> {
      try {
        this.form = { ...DEFAULTS, ...(await api.getSettings()) }
      } catch {
        this.form = { ...DEFAULTS }
      }
      // 磁盘为主题真相源；用户在读取完成前点过切换就不覆盖
      if (!this.themeTouched) applyTheme(this.form.theme)
      this.loaded = true
    },

    async save(next: Settings): Promise<void> {
      await api.saveSettings(next)
      this.form = { ...next }
      this.themeTouched = true
      applyTheme(next.theme)
    },

    /** 切换主题：立即生效 + 落盘（同时刷新原生窗口底色）。 */
    async setTheme(mode: ThemeMode): Promise<void> {
      this.themeTouched = true
      applyTheme(mode)
      // 磁盘设置还没读回来就切换：先补齐其余字段，避免把默认值写回配置文件
      if (!this.loaded) await this.load()
      const next: Settings = { ...this.form, theme: mode }
      this.form = next
      await api.saveSettings(next)
    },
  },
})

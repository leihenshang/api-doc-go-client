// 全局设置：网络策略 + 本地数据容量 + 界面（缩放倍率 / 响应区位置）。
// 界面相关项需全局响应式，因此抽成 store，App.vue 直接消费。
import { defineStore } from 'pinia'
import { api } from '@/lib/ipc'
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
}

export const useSettingsStore = defineStore('settings', {
  state: () => ({
    form: { ...DEFAULTS } as Settings,
    loaded: false,
  }),
  getters: {
    uiScale: (s) => s.form.uiScale,
    responseLayout: (s) => s.form.responseLayout,
    responseSize: (s) => s.form.responseSize,
  },
  actions: {
    async load(): Promise<void> {
      try {
        this.form = { ...DEFAULTS, ...(await api.getSettings()) }
      } catch {
        this.form = { ...DEFAULTS }
      }
      this.loaded = true
    },
    async save(next: Settings): Promise<void> {
      await api.saveSettings(next)
      this.form = { ...next }
    },
  },
})

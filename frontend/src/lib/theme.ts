// 主题（浅色 / 暗色）：`<html data-theme>` 与本模块内响应式状态的唯一写入口。
// 持久化分两处：真相源是全局设置（settings.json 的 theme 字段，由 settings store 读写），
// 同时把值缓存在 localStorage —— 启动时先用缓存上色（配合 index.html 的内联脚本避免闪白），
// settings.load() 拿到磁盘值后若不冲突就不再改动。
import { computed, ref } from 'vue'

export type ThemeMode = 'light' | 'dark'

export const ThemeLight: ThemeMode = 'light'
export const ThemeDark: ThemeMode = 'dark'

const STORAGE_KEY = 'client.theme'

/** 当前主题（响应式）：naive-ui 主题、弹层、自定义样式都据此切换。 */
export const themeMode = ref<ThemeMode>(ThemeLight)

/** 是否暗色（供组件/离散 API 直接使用）。 */
export const isDark = computed(() => themeMode.value === ThemeDark)

/** 读取本地缓存的主题；非法值或存储不可用时回落浅色。 */
export function readStoredTheme(): ThemeMode {
  try {
    return localStorage.getItem(STORAGE_KEY) === ThemeDark ? ThemeDark : ThemeLight
  } catch {
    return ThemeLight
  }
}

/** 应用主题：写状态 + `<html data-theme>` + 本地缓存。 */
export function applyTheme(mode: ThemeMode): void {
  themeMode.value = mode === ThemeDark ? ThemeDark : ThemeLight
  document.documentElement.dataset.theme = themeMode.value
  try {
    localStorage.setItem(STORAGE_KEY, themeMode.value)
  } catch {
    // 缓存失败不影响本次会话，下次启动回落磁盘设置
  }
}

/** 启动时用本地缓存上色（磁盘设置为准的校准在 settings.load() 里）。 */
export function initTheme(): void {
  applyTheme(readStoredTheme())
}

/** 取反：浅色 ↔ 暗色。 */
export function nextTheme(): ThemeMode {
  return themeMode.value === ThemeDark ? ThemeLight : ThemeDark
}

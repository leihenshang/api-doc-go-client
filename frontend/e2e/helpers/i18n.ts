// 用例文案一律取自语言包（本客户端默认 zh-CN，见 src/i18n/index.ts），
// 不在用例里硬编码中文/英文——否则 i18n 改名就会连带炸用例。
import en from '../../src/i18n/locales/en-US'
import zh from '../../src/i18n/locales/zh-CN'

type Dict = Record<string, unknown>

/** 按 `模块.语义` 取中文文案，支持 `{name}` 形式的插值。 */
export function t(key: string, vars: Record<string, string | number> = {}): string {
  const raw = key.split('.').reduce<unknown>((acc, k) => (acc && typeof acc === 'object' ? (acc as Dict)[k] : undefined), zh)
  if (typeof raw !== 'string') throw new Error(`语言包缺少 key: ${key}`)
  return raw.replace(/\{(\w+)\}/g, (_, name: string) => String(vars[name] ?? `{${name}}`))
}

/** 取英文文案（同一个 key，用于断言语言切换真的生效） */
export function tEn(key: string, vars: Record<string, string | number> = {}): string {
  const raw = key.split('.').reduce<unknown>((acc, k) => (acc && typeof acc === 'object' ? (acc as Dict)[k] : undefined), en)
  if (typeof raw !== 'string') throw new Error(`英文语言包缺少 key: ${key}`)
  return raw.replace(/\{(\w+)\}/g, (_, name: string) => String(vars[name] ?? `{${name}}`))
}

// 直连 App 门面造数据 / 校验：比点界面快且稳，适合做「前置条件」与「后置断言」。
import type { AppFixture } from './app'

export interface RequestDoc {
  uid: string
  name: string
  method: string
  url: string
  params?: unknown[]
  headers?: unknown[]
  body?: Record<string, unknown>
  auth?: Record<string, unknown> | null
  docs?: string
  path?: string
  [key: string]: unknown
}

/**
 * 强制「自动保存」开关（走 App 门面，不等界面）。
 *
 * 为什么需要：自动保存会让编辑在 600ms 防抖后落盘，而「编辑不写盘 / 丢弃不落盘」类用例
 * 都建立在**手动保存**前提上。只要有一条用例忘了把开关恢复，后续用例就会莫名失败。
 */
export async function setAutoSave(app: AppFixture, on: boolean): Promise<void> {
  const cur = await app.ipc<Record<string, unknown>>('GetSettings')
  await app.ipc('SaveSettings', { ...cur, autoSave: on })
}

export function readRequest(app: AppFixture, uid: string): Promise<RequestDoc> {
  return app.ipc<RequestDoc>('ReadRequest', uid)
}

/** 读改写：SaveRequest 是整对象覆盖，改字段前必须先读回最新内容 */
export async function patchRequest(app: AppFixture, uid: string, patch: Partial<RequestDoc>): Promise<RequestDoc> {
  const cur = await readRequest(app, uid)
  const next = { ...cur, ...patch }
  await app.ipc('SaveRequest', next)
  return next
}

/** 改全局设置（先读回再覆盖）。用于用例结束时恢复被自己改掉的共享设置。 */
export async function patchSettings(app: AppFixture, patch: Record<string, unknown>): Promise<void> {
  const cur = await app.ipc<Record<string, unknown>>('GetSettings')
  await app.ipc('SaveSettings', { ...cur, ...patch })
}

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

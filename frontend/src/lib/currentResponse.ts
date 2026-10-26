// 右侧响应面板「当前展示的响应」：ResponsePanel 在实时响应与已保存示例之间切换时写入本表，
// 请求编辑器的「从当前响应生成」直接读它 —— 右侧展示什么，就按什么派生字段表，不必重新发送。
// 用 reactive Map 而非普通 Map：ResponsePane 里以 computed 读取，需要能跟随切换示例响应式更新。
import type { SendResult } from '@/types'
import { reactive } from 'vue'

const byTab = reactive(new Map<string, SendResult | null>())

/** 记录某页签右侧面板当前展示的响应（null = 空态）。 */
export function setCurrentResponse(tabKey: string, resp: SendResult | null): void {
  if (!tabKey) return
  byTab.set(tabKey, resp)
}

/** 读取某页签右侧面板当前展示的响应；从未写入时返回 null。 */
export function getCurrentResponse(tabKey: string): SendResult | null {
  return byTab.get(tabKey) ?? null
}

/** 页签切换/关闭时清掉记录，避免旧页签残留的数据被误当成「当前响应」。 */
export function clearCurrentResponse(tabKey: string): void {
  if (!tabKey) return
  byTab.delete(tabKey)
}

// 「保存为变量」的作用域落点（response-panel design-spec §4 的 chips 集合/环境/全局）。
// 客户端现有变量模型只有两处真实存储（见 doc 的「有意差异」说明）：
// - env（环境）    → 当前环境 environments/<name>.yml，保存后立即可 {{var}} 引用
// - collection（集合）→ 当前请求 vars.pre-request（随请求 YAML 落盘，发送时同名覆盖环境变量）
// - global（全局）  → 写入全部环境，跨环境生效
import { api } from '@/lib/ipc'
import { useCollectionStore } from '@/stores/collection'
import { useTabsStore } from '@/stores/tabs'
import type { Tab } from '@/stores/tabs'
import type { EnvVar } from '@/types'

export type VarScope = 'collection' | 'env' | 'global'

export interface VarEntry {
  name: string
  value: string
}

/** 变量名写入校验与 Go 侧占位符规则一致：`[A-Za-z_][A-Za-z0-9_]*`。 */
export const VAR_NAME_RE = /^[A-Za-z_][A-Za-z0-9_]*$/

/** 字段路径 → 合法变量名（`data.items.0.id` → `data_items_0_id`）。 */
export function deriveVarName(path: string): string {
  const name = path.replace(/[^A-Za-z0-9_]+/g, '_').replace(/^_+|_+$/g, '')
  if (!name) return 'field'
  return VAR_NAME_RE.test(name) ? name : `v_${name}`
}

function upsertEnvVar(vars: EnvVar[], e: VarEntry): EnvVar[] {
  const next = vars.filter((v) => v.name !== e.name)
  const old = vars.find((v) => v.name === e.name)
  next.push({ name: e.name, value: e.value, enabled: true, secret: old?.secret ?? false })
  return next
}

function targetEnvNames(scope: VarScope): string[] {
  const coll = useCollectionStore()
  if (scope === 'global') return coll.envNames
  return coll.currentEnv ? [coll.currentEnv] : []
}

/** 按作用域写入一组变量（同名覆盖）。抛错表示前置条件不满足（无环境等），由调用方提示。 */
export async function saveVars(scope: VarScope, tab: Tab, entries: VarEntry[]): Promise<void> {
  if (!entries.length) return
  if (scope === 'collection') {
    const rows = [...(tab.request.varsPreRequest ?? [])]
    for (const e of entries) {
      const i = rows.findIndex((v) => v.name === e.name)
      if (i >= 0) rows[i] = { ...rows[i], value: e.value, enabled: true }
      else rows.push({ name: e.name, value: e.value, enabled: true })
    }
    tab.request.varsPreRequest = rows
    useTabsStore().touch(tab.key)
    return
  }
  const names = targetEnvNames(scope)
  if (!names.length) throw new Error('no-env')
  const coll = useCollectionStore()
  const envs = (coll.info?.envs ?? []).filter((e) => names.includes(e.name))
  for (const env of envs) {
    let vars = env.vars
    for (const e of entries) vars = upsertEnvVar(vars, e)
    await api.saveEnv({ ...env, vars })
  }
  await coll.reload()
}

/** 按作用域删除一个变量（书签取消）。 */
export async function removeVar(scope: VarScope, tab: Tab, name: string): Promise<void> {
  if (scope === 'collection') {
    tab.request.varsPreRequest = (tab.request.varsPreRequest ?? []).filter((v) => v.name !== name)
    useTabsStore().touch(tab.key)
    return
  }
  const names = targetEnvNames(scope)
  if (!names.length) throw new Error('no-env')
  const coll = useCollectionStore()
  const envs = (coll.info?.envs ?? []).filter((e) => names.includes(e.name))
  for (const env of envs) {
    await api.saveEnv({ ...env, vars: env.vars.filter((v) => v.name !== name) })
  }
  await coll.reload()
}

/** 该作用域里已存在的变量名集合（用于字段行书签状态判定）。 */
export function scopeVarNames(scope: VarScope, tab: Tab): Set<string> {
  const coll = useCollectionStore()
  if (scope === 'collection') {
    return new Set((tab.request.varsPreRequest ?? []).map((v) => v.name))
  }
  const envs = scope === 'global' ? (coll.info?.envs ?? []) : (coll.info?.envs ?? []).filter((e) => e.name === coll.currentEnv)
  if (!envs.length) return new Set()
  // 全局要求每个环境都有才算已书签（避免半写入状态反复横跳）
  let names = new Set(envs[0].vars.map((v) => v.name))
  for (const env of envs.slice(1)) {
    const own = new Set(env.vars.map((v) => v.name))
    names = new Set([...names].filter((n) => own.has(n)))
  }
  return names
}

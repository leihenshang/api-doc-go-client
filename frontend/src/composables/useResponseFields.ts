// 响应字段表（Resp 分段）的本地状态与写回：真相源是请求模型 tab.request.respDoc，
// 未落盘草稿 / gRPC 回退 localStorage 一次性存档。从 RequestEditor 抽出。
// 交互与请求体表格同构：可在「响应数据」子页签编辑，是合法 JSON 时**实时**解析出字段表格
// （表格只读、不加/删字段），只有每行「含义」可编辑并防抖写回 respDoc。
import {
    buildRespDoc,
    extractFields,
    loadFields,
    persistRespDoc,
    registerPendingCancel,
    rootJsonType,
    rowsFromDoc,
    unregisterPendingCancel,
    type FieldRow,
    type PersistTimer,
} from '@/lib/responseFields'
import type { Tab } from '@/stores/tabs'
import type { ResponseDoc } from '@/types'
import { computed, onBeforeUnmount, ref, watch } from 'vue'

/** useResponseFields 的上下文：全部以 getter 传入，读取实时 props，避免捕获到旧引用。 */
export interface ResponseFieldsCtx {
    /** 当前页签（读 uid/draft/request.respDoc/key） */
    getTab: () => Tab
    /** 是否 gRPC 请求（gRPC 不落 respDoc，走本地存档） */
    isGrpc: () => boolean
    /** 写盘失败的提示回调 */
    onError: (e: unknown) => void
}

/**
 * 从响应数据文本推导字段行：合法 JSON → 按结构解析出 路径/类型/值（含义按「路径」从 existing 继承）；
 * 文本为空 → 表格清空；**非法 JSON** → 原样返回 existing，避免输入中途把既有表格/含义清掉。
 * 注意：合法但无字段的数据（如 {} / 42 / "x"）会正常得到空表（不是「非法」）。
 */
function deriveFields(text: string, existing: FieldRow[]): FieldRow[] {
    if (text.trim() === '') return []
    try {
        JSON.parse(text)
    } catch {
        return existing // 非法 JSON：保留既有表
    }
    const prev = new Map(existing.map((f) => [f.path, f.meaning]))
    return extractFields(text).map((f) => ({ ...f, meaning: prev.get(f.path) ?? '' }))
}

/** respDoc 的「内容签名」：用来区分「我们的自写回显」与「真正的外部变更（同步/MCP/撤销）」。 */
function docSig(doc: ResponseDoc | null | undefined): string {
    return `${doc?.sample ?? ''}|${doc?.contentType ?? ''}|${JSON.stringify(rowsFromDoc(doc))}`
}

/** 建立响应字段表状态：sample（可编辑的响应数据）↔ fields（派生的只读表格）↔ respDoc。 */
export function useResponseFields(ctx: ResponseFieldsCtx) {
    const sample = ref('')
    const fields = ref<FieldRow[]>([])
    const hasResp = computed(() => fields.value.length > 0)
    const fieldTimer: PersistTimer = { current: null }
    /** 最近一次「自己写回/初始化」的签名：watch 用它排除自写回声，其余变更一律重算。 */
    let lastSig = ''
    /** 本实例当前登记的请求 uid（用于切请求/卸载时注销待写取消函数）。 */
    let lastUid = ''

    /** 切 tab / 外部刷新：以 respDoc.sample 为准重算（无 sample 的旧数据回退既有 params / 本地存档）。 */
    function initFields(): void {
        const tab = ctx.getTab()
        if (lastUid && lastUid !== tab.uid) unregisterPendingCancel(lastUid)
        if (tab.uid) {
            lastUid = tab.uid
            // 登记「取消待写」：响应面板做立即写前会调 cancelPendingFor(uid)，避免旧防抖回滚
            registerPendingCancel(tab.uid, cancelPending)
        }
        const doc = tab.request.respDoc
        lastSig = docSig(doc)
        sample.value = doc?.sample ?? ''
        const fromDoc = rowsFromDoc(doc)
        const base = fromDoc.length ? fromDoc : loadFields(tab.uid)
        fields.value = sample.value.trim() !== '' ? deriveFields(sample.value, base) : base
    }
    initFields()

    /** 取消尚未触发的防抖写回（响应面板做「立即写」前调用，避免旧 doc 盖掉新快照）。 */
    function cancelPending(): void {
        if (fieldTimer.current) {
            clearTimeout(fieldTimer.current)
            fieldTimer.current = null
        }
    }

    // 写回：已落盘 HTTP 请求更新模型 respDoc（含义/数据输入防抖）；草稿 / gRPC 只存本地存档。
    async function persistFields(immediate = false): Promise<void> {
        const tab = ctx.getTab()
        await persistRespDoc({
            uid: tab.uid,
            draft: tab.draft,
            grpc: ctx.isGrpc(),
            rows: fields.value,
            doc: buildRespDoc(fields.value, tab.request.respDoc, {
                sample: sample.value,
                type: rootJsonType(sample.value),
            }),
            immediate,
            timer: fieldTimer,
            attach: (doc) => {
                // 切了 tab：磁盘已按原 uid 写对，但不能把 A 的 respDoc 写进 B 的内存模型
                const now = ctx.getTab()
                if (now.key !== tab.key) return
                lastSig = docSig(doc)
                now.request.respDoc = doc
            },
            onError: ctx.onError,
        })
    }

    // 外部（同步拉取 / MCP / 撤销 / 响应区更新）改写了 respDoc：与「自己写回」签名不同才重算，
    // 既避免自写回声打断输入，也能接收「只改 params 不改 sample」的外部变更（此前会被静默忽略）。
    watch(
        () => ctx.getTab().request.respDoc,
        (doc) => {
            const sig = docSig(doc)
            if (sig === lastSig) return
            lastSig = sig
            sample.value = doc?.sample ?? ''
            const fromDoc = rowsFromDoc(doc)
            fields.value = sample.value.trim() !== '' ? deriveFields(sample.value, fromDoc) : fromDoc
        },
    )

    /** 只更新内存中的 sample / fields / respDoc，不写盘（格式化等需要用户确认保存的场景）。 */
    function applyLocal(value: string): void {
        sample.value = value
        fields.value = deriveFields(value, fields.value)
        const tab = ctx.getTab()
        const doc = buildRespDoc(fields.value, tab.request.respDoc, {
            sample: value,
            type: rootJsonType(value),
        })
        tab.request.respDoc = doc
        lastSig = docSig(doc)
    }

    /**
     * 编辑「响应数据」：实时更新只读表格（合法 JSON 才换，非法时保留既有表）。
     * persist=false 时只改内存并标脏（由调用方走手动保存），不触发防抖写盘。
     */
    function onSampleInput(value: string, opts?: { persist?: boolean }): void {
        applyLocal(value)
        if (opts?.persist === false) return
        void persistFields()
    }

    /** 编辑某字段含义（按路径）：写回 respDoc.params[].description，防抖落盘。 */
    function setMeaning(path: string, meaning: string): void {
        const f = fields.value.find((x) => x.path === path)
        if (f) f.meaning = meaning
        void persistFields()
    }

    onBeforeUnmount(() => {
        cancelPending()
        if (lastUid) unregisterPendingCancel(lastUid)
    })

    return { fields, sample, hasResp, initFields, onSampleInput, setMeaning, cancelPending }
}

// 响应字段表（Resp 分段）的本地状态与写回：真相源是请求模型 tab.request.respDoc，
// 未落盘草稿 / gRPC 回退 localStorage 一次性存档。
// 与请求体表格同构：表格**只在用户点「生成数据表格」/「从本次响应生成」时派生**，
// 派生后 5 列全可编辑并可增删行；编辑「响应数据」（sample）不再联动重算表格。
import {
    buildRespDoc,
    loadFields,
    persistRespDoc,
    registerPendingCancel,
    rootJsonType,
    rowsFromDoc,
    unregisterPendingCancel,
    type FieldRow,
    type PersistTimer,
    type RespDocLive,
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

/** respDoc 的「内容签名」：用来区分「我们的自写回显」与「真正的外部变更（同步/MCP/撤销）」。 */
function docSig(doc: ResponseDoc | null | undefined): string {
    return `${doc?.sample ?? ''}|${doc?.contentType ?? ''}|${JSON.stringify(rowsFromDoc(doc))}`
}

/** 建立响应字段表状态：sample（可编辑的响应数据）+ fields（手工派生/编辑的表格）↔ respDoc。 */
export function useResponseFields(ctx: ResponseFieldsCtx) {
    const sample = ref('')
    const fields = ref<FieldRow[]>([])
    const hasResp = computed(() => fields.value.length > 0)
    const fieldTimer: PersistTimer = { current: null }
    /** 最近一次「自己写回/初始化」的签名：watch 用它排除自写回声，其余变更一律重算。 */
    let lastSig = ''
    /** 本实例当前登记的请求 uid（用于切请求/卸载时注销待写取消函数）。 */
    let lastUid = ''

    /** 切 tab / 外部刷新：sample 与表格都直接取 respDoc（无 params 的旧数据回退本地存档）。 */
    function initFields(): void {
        const tab = ctx.getTab()
        if (lastUid && lastUid !== tab.uid) unregisterPendingCancel(lastUid)
        if (tab.uid) {
            lastUid = tab.uid
            // 登记「取消待写」：生成表格等立即写之前会调 cancelPendingFor(uid)，避免旧防抖回滚
            registerPendingCancel(tab.uid, cancelPending)
        }
        const doc = tab.request.respDoc
        lastSig = docSig(doc)
        sample.value = doc?.sample ?? ''
        const fromDoc = rowsFromDoc(doc)
        fields.value = fromDoc.length ? fromDoc : loadFields(tab.uid)
    }
    initFields()

    /** 取消尚未触发的防抖写回（立即写之前调用，避免旧 doc 盖掉新快照）。 */
    function cancelPending(): void {
        if (fieldTimer.current) {
            clearTimeout(fieldTimer.current)
            fieldTimer.current = null
        }
    }

    // 写回：已落盘 HTTP 请求更新模型 respDoc（单元格输入防抖）；草稿 / gRPC 只存本地存档。
    // liveMeta 只在「从本次响应生成」时由调用方传入（status/headers/contentType 取本次响应快照）；
    // 手动来源与日常编辑都不传，于是这些字段按 buildRespDoc 的规则回退既有 doc（不会被清掉）。
    async function persistFields(immediate = false, liveMeta?: RespDocLive): Promise<void> {
        const tab = ctx.getTab()
        await persistRespDoc({
            uid: tab.uid,
            draft: tab.draft,
            grpc: ctx.isGrpc(),
            rows: fields.value,
            // sample/type 恒按当前「响应数据」：生成/编辑表格**不修改**「响应数据」
            doc: buildRespDoc(fields.value, tab.request.respDoc, {
                sample: sample.value,
                type: rootJsonType(sample.value),
                ...liveMeta,
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

    // 外部（同步拉取 / MCP / 撤销）改写了 respDoc：与「自己写回」签名不同才重算。
    watch(
        () => ctx.getTab().request.respDoc,
        (doc) => {
            const sig = docSig(doc)
            if (sig === lastSig) return
            lastSig = sig
            sample.value = doc?.sample ?? ''
            fields.value = rowsFromDoc(doc)
        },
    )

    /** 只更新内存中的 sample / respDoc，不写盘（格式化等需要用户确认保存的场景）。 */
    function applySample(value: string): void {
        sample.value = value
        const tab = ctx.getTab()
        const doc = buildRespDoc(fields.value, tab.request.respDoc, {
            sample: value,
            type: rootJsonType(value),
        })
        tab.request.respDoc = doc
        lastSig = docSig(doc)
    }

    /**
     * 编辑「响应数据」：只改数据本身，**不重算字段表格**（表格只在手工点生成时变化）。
     * persist=false 时只改内存并标脏（由调用方走手动保存），不触发防抖写盘。
     */
    function onSampleInput(value: string, opts?: { persist?: boolean }): void {
        applySample(value)
        if (opts?.persist === false) return
        void persistFields()
    }

    /** 写入整张表格（生成 / 覆盖 / 新增 / 删行）：immediate=true 时立即落盘（生成场景）。
     * liveMeta 仅在「从本次响应生成」时传入（带当次 status/headers/contentType）。 */
    function applyRows(rows: FieldRow[], immediate = false, liveMeta?: RespDocLive): void {
        fields.value = rows
        void persistFields(immediate, liveMeta)
    }

    /** 就地更新第 i 行的部分列（路径 / 类型 / 必填 / 示例值 / 含义），防抖写回 respDoc。 */
    function patchRow(index: number, patch: Partial<FieldRow>): void {
        if (index < 0 || index >= fields.value.length) return
        const next = fields.value.slice()
        next[index] = { ...next[index], ...patch }
        fields.value = next
        void persistFields()
    }

    /** 追加一行空行（必填默认「是」）。 */
    function addRow(row: FieldRow): void {
        fields.value = [...fields.value, row]
        void persistFields()
    }

    /** 删除第 i 行。 */
    function removeRow(index: number): void {
        if (index < 0 || index >= fields.value.length) return
        fields.value = fields.value.filter((_, i) => i !== index)
        void persistFields(true)
    }

    /**
     * 立刻把尚未触发的防抖写盘刷掉（生成文档等「读取 respDoc」的动作之前调用）：
     * 先丢掉待写的计时器，再以 immediate 写一次当前表格，避免读到 600ms 前的旧快照。
     * 写盘失败由 ctx.onError 提示，本函数不抛错（调用方不需要中断后续流程）。
     */
    async function flushFields(): Promise<void> {
        cancelPending()
        await persistFields(true)
    }

    onBeforeUnmount(() => {
        cancelPending()
        if (lastUid) unregisterPendingCancel(lastUid)
    })

    return {
        fields,
        sample,
        hasResp,
        initFields,
        onSampleInput,
        applyRows,
        patchRow,
        addRow,
        removeRow,
        flushFields,
        cancelPending,
    }
}

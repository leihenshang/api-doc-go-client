// 响应字段表（Resp 分段）的本地状态：真相源是请求模型 tab.request.respDoc。
// 与请求体表格同构：表格**只在用户点「生成数据表格」/「从本次响应生成」时派生**，
// 派生后 5 列全可编辑并可增删行；编辑「响应数据」（sample）不再联动重算表格。
//
// 保存模型（统一手动保存）：本 composable 的所有编辑**只改内存里的 respDoc** 并标脏
// （ctx.onDirty → tabs.touch），不写盘；落盘由 Ctrl+S / 关闭页签时的 tabs.flush 整条请求写入。
import {
    buildRespDoc,
    loadFields,
    rootJsonType,
    rowsFromDoc,
    type FieldRow,
    type RespDocLive,
} from '@/lib/responseFields'
import type { Tab } from '@/stores/tabs'
import type { ResponseDoc } from '@/types'
import { computed, ref, watch } from 'vue'

/** useResponseFields 的上下文：全部以 getter 传入，读取实时 props，避免捕获到旧引用。 */
export interface ResponseFieldsCtx {
    /** 当前页签（读 uid/request.respDoc/key） */
    getTab: () => Tab
    /** 是否 gRPC 请求（gRPC 无 respDoc，字段表不参与落盘） */
    isGrpc: () => boolean
    /** 编辑回调：把当前页签标脏（RequestEditor 传 () => tabs.touch(tab.key)） */
    onDirty: () => void
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
    /** 最近一次「自己写回/初始化」的签名：watch 用它排除自写回声，其余变更一律重算。 */
    let lastSig = ''

    /** 切 tab / 外部刷新：sample 与表格都直接取 respDoc（无 params 的旧数据回退本地存档）。 */
    function initFields(): void {
        const tab = ctx.getTab()
        const doc = tab.request.respDoc
        lastSig = docSig(doc)
        sample.value = doc?.sample ?? ''
        const fromDoc = rowsFromDoc(doc)
        fields.value = fromDoc.length ? fromDoc : loadFields(tab.uid)
    }
    initFields()

    /**
     * 把当前 sample + 表格组装成 respDoc 写回**内存模型**，并标脏（不写盘）。
     * liveMeta 只在「从本次响应生成」时由调用方传入（status/headers/contentType 取本次响应快照）；
     * 手动来源与日常编辑都不传，于是这些字段按 buildRespDoc 的规则回退既有 doc（不会被清掉）。
     */
    function commit(liveMeta?: RespDocLive): void {
        const tab = ctx.getTab()
        // sample/type 按当前「响应数据」；仅「从本次响应生成」选「覆盖」时由 applyRows
        // 先写入 nextSample，于是这次提交会连同响应体一起更新。
        const doc = buildRespDoc(fields.value, tab.request.respDoc, {
            sample: sample.value,
            type: rootJsonType(sample.value),
            ...liveMeta,
        })
        tab.request.respDoc = doc
        // 先记签名再标脏：watch 是异步的，签名一致时不会把这次自写当成外部变更重算
        lastSig = docSig(doc)
        ctx.onDirty()
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

    /** 编辑「响应数据」：只改数据本身，**不重算字段表格**；只改内存并标脏。 */
    function onSampleInput(value: string): void {
        sample.value = value
        commit()
    }

    /** 写入整张表格（生成 / 覆盖 / 新增 / 删行）；liveMeta 仅在「从本次响应生成」时传入。
     *  nextSample 传入时一并替换「响应数据」（live 来源选「覆盖」：响应体随表格一起更新）。 */
    function applyRows(rows: FieldRow[], liveMeta?: RespDocLive, nextSample?: string): void {
        if (nextSample !== undefined) sample.value = nextSample
        fields.value = rows
        commit(liveMeta)
    }

    /** 就地更新第 i 行的部分列（路径 / 类型 / 必填 / 示例值 / 含义）。 */
    function patchRow(index: number, patch: Partial<FieldRow>): void {
        if (index < 0 || index >= fields.value.length) return
        const next = fields.value.slice()
        next[index] = { ...next[index], ...patch }
        fields.value = next
        commit()
    }

    /** 追加一行空行（必填默认「是」）。 */
    function addRow(row: FieldRow): void {
        fields.value = [...fields.value, row]
        commit()
    }

    /** 删除第 i 行。 */
    function removeRow(index: number): void {
        if (index < 0 || index >= fields.value.length) return
        fields.value = fields.value.filter((_, i) => i !== index)
        commit()
    }

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
    }
}

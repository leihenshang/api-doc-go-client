// 响应字段表（Resp 分段）的本地状态与写回：真相源是请求模型 tab.request.respDoc，
// 未落盘草稿 / gRPC 回退 localStorage 一次性存档。从 RequestEditor 抽出，
// 让「字段表 ↔ respDoc ↔ 防抖落盘」这一整套逻辑独立成一处，组件只负责渲染。
import {
    buildRespDoc,
    loadFields,
    persistRespDoc,
    rowsFromDoc,
    type FieldRow,
    type PersistTimer,
} from '@/lib/responseFields'
import type { Tab } from '@/stores/tabs'
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

/** 建立响应字段表状态，返回字段表与其增删改入口 + 供 tab 切换时重算的 initFields。 */
export function useResponseFields(ctx: ResponseFieldsCtx) {
    const fields = ref<FieldRow[]>([])
    const hasResp = computed(() => fields.value.length > 0)
    const fieldTimer: PersistTimer = { current: null }

    function initFields(): void {
        const tab = ctx.getTab()
        const fromDoc = rowsFromDoc(tab.request.respDoc)
        fields.value = fromDoc.length ? fromDoc : loadFields(tab.uid)
    }
    initFields()

    // 外部（响应区更新 / 同步拉取）改写了 respDoc：仅当与本地字段表不同才整体替换，
    // 避免含义输入触发的防抖写回（每次都是新 doc 对象）刚写完就把输入打断。
    watch(
        () => ctx.getTab().request.respDoc,
        (doc) => {
            const incoming = rowsFromDoc(doc)
            const cur = fields.value
            const same =
                incoming.length === cur.length &&
                incoming.every(
                    (f, i) =>
                        f.path === cur[i]?.path &&
                        f.value === cur[i]?.value &&
                        f.type === cur[i]?.type &&
                        f.meaning === cur[i]?.meaning,
                )
            if (!same) fields.value = incoming
        },
    )

    /** 写回：已落盘 HTTP 请求更新模型 respDoc（含义输入防抖；删除/清空立即写）；草稿 / gRPC 只存本地。 */
    async function persistFields(immediate = false): Promise<void> {
        const tab = ctx.getTab()
        // 字段表编辑不动快照：type/sample/status/headers 一律回退既有 respDoc（live 传空）。
        await persistRespDoc({
            uid: tab.uid,
            draft: tab.draft,
            grpc: ctx.isGrpc(),
            rows: fields.value,
            doc: buildRespDoc(fields.value, tab.request.respDoc, {}),
            immediate,
            timer: fieldTimer,
            attach: (doc) => {
                ctx.getTab().request.respDoc = doc
            },
            onError: ctx.onError,
        })
    }

    function setMeaning(path: string, meaning: string): void {
        const f = fields.value.find((x) => x.path === path)
        if (f) f.meaning = meaning
        void persistFields()
    }
    async function removeField(path: string): Promise<void> {
        fields.value = fields.value.filter((f) => f.path !== path)
        await persistFields(true)
    }
    async function removeFieldsByPath(paths: string[]): Promise<void> {
        const drop = new Set(paths)
        fields.value = fields.value.filter((f) => !drop.has(f.path))
        await persistFields(true)
    }
    async function clearFields(): Promise<void> {
        fields.value = []
        await persistFields(true)
    }

    onBeforeUnmount(() => {
        if (fieldTimer.current) clearTimeout(fieldTimer.current)
    })

    return { fields, hasResp, initFields, setMeaning, removeField, removeFieldsByPath, clearFields }
}

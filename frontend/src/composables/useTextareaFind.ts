// 编辑区页内查找（textarea 版）：在 JSON / 文本请求体、响应数据、脚本、Docs 源码等 textarea 上
// 用原生选区做「上一个 / 下一个」跳转。从 RequestEditor 抽出，组件只提供分段状态查询与容器 ref。
import FindBar from '@/components/FindBar.vue'
import { findInTextareas, selectTextareaMatch, type TextareaMatch } from '@/lib/find'
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

/** useTextareaFind 的上下文：分段相关的响应式来源以 getter 注入（读取实时值，避免捕获旧引用）。 */
export interface TextareaFindCtx {
    /** 当前分段 key（body/script/docs/message 才可能有可查找 textarea） */
    getSeg: () => string
    /** Docs 是否处于编辑态（预览是渲染 HTML，不接入查找） */
    getDocsEditing: () => boolean
    /** 请求体类型（body 分段用：仅 json/text 可查找） */
    getBodyType: () => string
    /** 响应数据子页签（resp 分段用：仅 body 视图有可编辑 textarea） */
    getRespView?: () => string
    /** 是否 gRPC 请求（message 分段的 JSON textarea 支持查找） */
    isGrpc: () => boolean
}

/**
 * 建立编辑区查找：返回查找条状态、容器 ref（供模板 ref 绑定）与操作入口。
 * 内部维护「关键字/分段变化时重算命中」的 watch 与 window 级 Ctrl+F 监听。
 */
export function useTextareaFind(ctx: TextareaFindCtx) {
    const segBodyEl = ref<HTMLElement | null>(null)
    const findOn = ref(false)
    const findKw = ref('')
    const findTotal = ref(0)
    const findIndex = ref(-1)
    const findBarRef = ref<InstanceType<typeof FindBar> | null>(null)
    let findMatches: TextareaMatch[] = []

    /** 当前分段是否有可查找的 textarea（Docs 仅编辑态有；预览是渲染 HTML，不接入）。 */
    function findable(): boolean {
        const seg = ctx.getSeg()
        if (seg === 'body') return ctx.getBodyType() === 'json' || ctx.getBodyType() === 'text'
        if (seg === 'resp') return ctx.getRespView?.() === 'body'
        if (seg === 'script') return true
        if (seg === 'docs') return ctx.getDocsEditing()
        // gRPC 请求消息是 JSON textarea，同样支持查找
        if (seg === 'message') return ctx.isGrpc()
        return false
    }

    function findTargets(): HTMLTextAreaElement[] {
        if (!segBodyEl.value) return []
        return Array.from(segBodyEl.value.querySelectorAll('textarea')).filter(
            (ta) => ta.offsetParent !== null && !ta.disabled,
        )
    }

    async function openFind(): Promise<void> {
        if (!findable()) return
        findOn.value = true
        await nextTick()
        // 先选中/滚动到第一处，再把焦点交回搜索框：打开即可直接输入关键字
        applyFind()
        findBarRef.value?.focus()
    }

    /**
     * 重算命中并（默认）选中第一处。
     * select=false 用于「编辑区内容变化」：只更新计数，不重新选区/抢焦点，避免光标被拽回第一处。
     */
    function applyFind(options: { select?: boolean; focus?: boolean } = {}): void {
        const { select = true, focus = true } = options
        findMatches = findInTextareas(findTargets(), findKw.value)
        findTotal.value = findMatches.length
        findIndex.value = findMatches.length ? 0 : -1
        if (select && findMatches.length) selectTextareaMatch(findMatches[0], focus)
    }

    function findNext(): void {
        if (!findMatches.length) return
        const i = (findIndex.value + 1) % findMatches.length
        findIndex.value = i
        selectTextareaMatch(findMatches[i])
    }
    function findPrev(): void {
        if (!findMatches.length) return
        const i = (findIndex.value - 1 + findMatches.length) % findMatches.length
        findIndex.value = i
        selectTextareaMatch(findMatches[i])
    }

    watch(
        [findKw, () => ctx.getSeg(), () => ctx.getDocsEditing(), () => ctx.getBodyType(), () => ctx.getRespView?.(), findOn],
        async () => {
            // 切到没有可查找 textarea 的分段（或 Docs 切回预览）：查找条自动收起
            if (findOn.value && !findable()) {
                findOn.value = false
                return
            }
            if (!findOn.value) {
                findMatches = []
                findTotal.value = 0
                findIndex.value = -1
                return
            }
            await nextTick()
            applyFind()
            // 关键字/分段变化后把焦点留在搜索框：否则选区聚焦到 textarea，下一字符会输进正文
            findBarRef.value?.focus()
        },
    )

    // window 级监听：编辑器根 div 不可聚焦，焦点在 body / 表格输入框时元素上的 @keydown 收不到
    // 事件（Ctrl+F 不会冒泡经过根 div）。编辑器为活动 tab 单实例，挂 window 安全；
    // 当前分段没有可查找 textarea 时不拦截，交回浏览器原生查找。
    function onGlobalKeydown(e: KeyboardEvent): void {
        if (!(e.ctrlKey || e.metaKey) || e.key.toLowerCase() !== 'f' || !findable()) return
        // 焦点在响应区内时交给 ResponsePanel（两侧都挂了 window 监听，避免双开）
        if ((e.target as HTMLElement | null)?.closest('.resp')) return
        e.preventDefault()
        void openFind()
    }
    onMounted(() => window.addEventListener('keydown', onGlobalKeydown))
    onBeforeUnmount(() => window.removeEventListener('keydown', onGlobalKeydown))

    return { segBodyEl, findOn, findKw, findTotal, findIndex, findBarRef, findable, openFind, applyFind, findNext, findPrev }
}

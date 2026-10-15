// 响应只读区的页内查找（DOM 高亮版）：用 DomHighlighter 在渲染容器里标记命中并逐项定位。
// 从 ResponsePanel 抽出：组件只提供「影响内容呈现」的响应式来源（分段/美化原始/响应体/头/尾），
// 查找条状态、高亮器、Ctrl+F 监听与卸载清理都由本 composable 自持。
import FindBar from '@/components/FindBar.vue'
import { DomHighlighter } from '@/lib/find'
import type { KV } from '@/types'
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

/** useDomFind 的上下文：内容变化会影响高亮命中，以 getter 注入供重算 watch 追踪。 */
export interface DomFindCtx {
    getSeg: () => string
    getView: () => string
    getBody: () => string
    getHeaders: () => KV[] | null | undefined
    getTrailers: () => KV[] | null | undefined
    /** 是否存在可查找的响应（无响应时不拦截 Ctrl+F） */
    hasResponse: () => boolean
}

/** 建立响应区查找：返回查找条状态、容器/查找条 ref 与操作入口。 */
export function useDomFind(ctx: DomFindCtx) {
    const findOn = ref(false)
    const findKw = ref('')
    const findTotal = ref(0)
    const findIndex = ref(-1)
    const paneRef = ref<HTMLElement | null>(null)
    const findBarRef = ref<InstanceType<typeof FindBar> | null>(null)
    const highlighter = new DomHighlighter()

    function openFind(): void {
        findOn.value = true
        void nextTick(() => findBarRef.value?.focus())
    }

    watch(findOn, (on) => {
        if (!on) {
            findKw.value = ''
            highlighter.clear()
            findTotal.value = 0
            findIndex.value = -1
        } else {
            void nextTick(() => findBarRef.value?.focus())
        }
    })

    watch(
        [findKw, () => ctx.getSeg(), () => ctx.getView(), () => ctx.getBody(), findOn, () => ctx.getHeaders(), () => ctx.getTrailers()],
        async () => {
            if (!findOn.value) return
            await nextTick() // 等页签 / 美化原始切换渲染完
            findTotal.value = highlighter.apply(paneRef.value, findKw.value)
            findIndex.value = findTotal.value ? 0 : -1
            if (findTotal.value) highlighter.select(0)
        },
    )

    function findNext(): void {
        if (!findTotal.value) return
        highlighter.step(1)
        findIndex.value = highlighter.index
    }
    function findPrev(): void {
        if (!findTotal.value) return
        highlighter.step(-1)
        findIndex.value = highlighter.index
    }

    // window 级监听：面板根 div 不可聚焦，Playwright/用户在非按钮焦点下按 Ctrl+F 时事件落在
    // body，元素上的 @keydown 收不到冒泡。面板为活动 tab 单实例，挂 window 安全。
    function onGlobalKeydown(e: KeyboardEvent): void {
        if (!(e.ctrlKey || e.metaKey) || e.key.toLowerCase() !== 'f' || !ctx.hasResponse()) return
        const target = e.target as HTMLElement | null
        // 焦点在请求编辑器内（含编辑器自己的查找条）时交给 RequestEditor
        if (target?.closest('.editor')) return
        // 焦点在中性区域（页签栏 / 空白处 / body）：编辑器当前分段可查找时优先让给编辑器，避免两个查找条同开
        if (!target?.closest('.resp') && document.querySelector('.editor .find-btn')) return
        e.preventDefault()
        openFind()
    }
    onMounted(() => window.addEventListener('keydown', onGlobalKeydown))
    onBeforeUnmount(() => {
        window.removeEventListener('keydown', onGlobalKeydown)
        // Vue 下次重渲染前必须清标记，避免 <mark> 包裹与 Custom Highlight 残留
        highlighter.clear()
    })

    return { findOn, findKw, findTotal, findIndex, paneRef, findBarRef, openFind, findNext, findPrev }
}

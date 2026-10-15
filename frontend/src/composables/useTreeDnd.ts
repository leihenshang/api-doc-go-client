// 集合树的指针拖拽（不依赖 HTML5 dataTransfer，自己按 pointermove/up + elementFromPoint 命中落点）。
// 从 Sidebar 抽出：组件只提供「能否拖」「落点重名提示」「松手后如何搬运」三个回调，
// 拖拽过程中的按下/移动/松手/取消、阈值判定与落点高亮状态由本 composable 自持。
import { canDrop } from '@/lib/collTree'
import type { TreeNode } from '@/types'
import { computed, ref } from 'vue'

/** 当前指针下的放置目标；ok=false 表示非法，给「禁止」反馈而不是静默。 */
export interface DropHint {
    root: string
    uid: string
    path: string
    ok: boolean
    tip?: string
}

/** useTreeDnd 的上下文：把与集合/存储/i18n 耦合的决策留回组件，以回调注入。 */
export interface TreeDndCtx {
    /** 此刻是否允许拖拽（搜索过滤 / 重命名中禁用） */
    dragEnabled: () => boolean
    /** 落点非法原因提示（如目标下已有同名分组）；返回 undefined 表示无额外提示 */
    dropTip: (node: TreeNode, destRoot: string, destPath: string) => string | undefined
    /** 松手落在合法目标上时执行移动（同根改上级 / 跨根整棵搬） */
    onDrop: (srcRoot: string, node: TreeNode, destRoot: string, destPath: string) => void
}

/** 位移小于它算点击，不算拖动 */
const DRAG_THRESHOLD = 4

/** 建立树拖拽：返回拖拽状态、点击抑制标记与行按下入口。 */
export function useTreeDnd(ctx: TreeDndCtx) {
    const dragNode = ref<{ root: string; node: TreeNode } | null>(null)
    /** 按下但还没越过阈值：等他动了才算拖动，避免影响正常点击打开请求 */
    const pending = ref<{ root: string; node: TreeNode; x: number; y: number } | null>(null)
    const dropHint = ref<DropHint | null>(null)
    const dragging = computed(() => dragNode.value !== null)
    /** 拖动结束后紧跟的那次 click 要吞掉，否则松手会顺手打开请求 */
    const clickSuppressed = ref(false)

    function onRowPointerDown(root: string, node: TreeNode, ev: PointerEvent): void {
        if (!ctx.dragEnabled() || ev.button !== 0) return
        // 只排除「行内操作控件」：展开箭头、悬停动作按钮、重命名输入框、下拉与链接。
        // 注意不能一刀切排除所有 button —— 请求/分组的名字本身就是个 <button class="rname">，
        // 一刀切会让整行都拖不动（实测就是这个原因导致鼠标拖起来毫无反应）。
        const el = ev.target as HTMLElement | null
        if (el?.closest('.act, .caret, .n-dropdown, .n-input, input, textarea, select, a')) return
        pending.value = { root, node, x: ev.clientX, y: ev.clientY }
        window.addEventListener('pointermove', onPointerMove)
        window.addEventListener('pointerup', onPointerUp)
        window.addEventListener('pointercancel', cancelDrag)
        window.addEventListener('keydown', onDragKeydown)
    }

    function detachDragListeners(): void {
        window.removeEventListener('pointermove', onPointerMove)
        window.removeEventListener('pointerup', onPointerUp)
        window.removeEventListener('pointercancel', cancelDrag)
        window.removeEventListener('keydown', onDragKeydown)
    }

    /** 指针所在位置对应的放置目标（用 elementFromPoint 命中行，不依赖事件冒泡顺序）。 */
    function hintAt(x: number, y: number): DropHint | null {
        const drag = dragNode.value
        if (!drag) return null
        const el = document.elementFromPoint(x, y) as HTMLElement | null
        const row = el?.closest('[data-testid="tree.row"], [data-testid="tree.root"]') as HTMLElement | null
        if (!row) return null
        const root = row.dataset.root ?? ''
        const isRootRow = row.dataset.testid === 'tree.root'
        if (!isRootRow && row.dataset.kind !== 'folder') return null // 请求行不能当容器
        const path = isRootRow ? '' : (row.dataset.path ?? '')
        // 跨根搬分组时目标下已有同名分组：后端会拒（CreateFolder 会把同名目录复用掉），
        // 这里提前置灰并把原因写在落点提示上，别让用户拖完才吃一个报错
        const tip = drag.node.type === 'folder' ? ctx.dropTip(drag.node, root, path) : undefined
        return {
            root,
            uid: isRootRow ? '__root__' : (row.dataset.uid ?? ''),
            path,
            ok: canDrop(drag.node, drag.root, root, path) && !tip,
            tip,
        }
    }

    function onPointerMove(ev: PointerEvent): void {
        const p = pending.value
        if (!p) return
        if (!dragNode.value) {
            if (Math.abs(ev.clientX - p.x) < DRAG_THRESHOLD && Math.abs(ev.clientY - p.y) < DRAG_THRESHOLD) return
            dragNode.value = { root: p.root, node: p.node }
        }
        ev.preventDefault() // 拖拽期间不选中文字
        dropHint.value = hintAt(ev.clientX, ev.clientY)
    }

    function onDragKeydown(ev: KeyboardEvent): void {
        if (ev.key === 'Escape') cancelDrag()
    }

    function cancelDrag(): void {
        detachDragListeners()
        pending.value = null
        dragNode.value = null
        dropHint.value = null
    }

    function onPointerUp(ev: PointerEvent): void {
        const drag = dragNode.value
        const hint = drag ? hintAt(ev.clientX, ev.clientY) : null
        const dragged = drag !== null
        detachDragListeners()
        pending.value = null
        dragNode.value = null
        dropHint.value = null
        if (!dragged) return // 只是点击：交给 click 处理
        clickSuppressed.value = true
        setTimeout(() => (clickSuppressed.value = false), 0)
        if (drag && hint?.ok) ctx.onDrop(drag.root, drag.node, hint.root, hint.path)
    }

    return { dragNode, dropHint, dragging, clickSuppressed, onRowPointerDown }
}

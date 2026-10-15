// 分栏与侧栏宽度：请求/响应区的可拖动分隔条（改 flex-basis 百分比）+ 侧栏拖宽（改 CSS 变量），
// 松手后落盘（响应区尺寸走全局设置，侧栏宽记 localStorage）。从 App.vue 抽出，组件只绑定这些 ref 与入口。
import { useSettingsStore } from '@/stores/settings'
import { computed, ref, watch } from 'vue'

const SIDE_W_KEY = 'apidoc.sideW'
/** 标题测不出来时的兜底最小宽（与默认 260 同量级） */
const SIDE_W_MIN_FALLBACK = 200
/** 根行右侧操作区的固定占位（同步钮 + ··· 钮 + 行内边距）：actions hover 才渲染，量不到，按占位估 */
const SIDE_W_ROW_CHROME = 8 + 16 + 6 + 44 + 16

/** 建立分栏 / 侧栏宽度状态与拖拽入口。返回的容器 ref 供模板绑定，也可被外层读滚动位置。 */
export function useSplitLayout() {
    const settings = useSettingsStore()

    const workEl = ref<HTMLElement | null>(null)
    const editorEl = ref<HTMLElement | null>(null)
    const respEl = ref<HTMLElement | null>(null)
    const respSize = ref(settings.responseSize)
    // 拖动中不覆盖本地值，避免落盘往返把拖动位置"回跳"
    let resizing = false

    watch(
        () => settings.responseSize,
        (v) => {
            if (!resizing) respSize.value = v
        },
    )

    // 响应区尺寸：flex-basis 百分比（right 生效为宽度、bottom 生效为高度）
    const respStyle = computed(() => ({ flexBasis: `${respSize.value}%` }))

    function setLayout(layout: 'right' | 'bottom'): void {
        if (settings.responseLayout === layout) return
        void settings.save({ ...settings.form, responseLayout: layout })
    }

    function startResize(e: PointerEvent): void {
        const box = workEl.value
        if (!box) return
        e.preventDefault()
        resizing = true
        const horizontal = settings.responseLayout === 'right'
        // 四舍五入 + 20~80 夹取，与 Go 侧 config.Normalize 的范围一致
        const clamp = (n: number): number => Math.min(80, Math.max(20, Math.round(n)))

        const move = (ev: PointerEvent): void => {
            const r = box.getBoundingClientRect()
            respSize.value = horizontal
                ? clamp(((r.right - ev.clientX) / r.width) * 100)
                : clamp(((r.bottom - ev.clientY) / r.height) * 100)
        }
        const stop = (): void => {
            resizing = false
            window.removeEventListener('pointermove', move)
            window.removeEventListener('pointerup', stop)
            window.removeEventListener('pointercancel', stop)
            document.body.classList.remove('dragging', 'col', 'row')
            void settings.save({ ...settings.form, responseSize: respSize.value })
        }

        document.body.classList.add('dragging', horizontal ? 'col' : 'row')
        window.addEventListener('pointermove', move)
        window.addEventListener('pointerup', stop)
        window.addEventListener('pointercancel', stop)
    }

    function minSideWidth(): number {
        let min = SIDE_W_MIN_FALLBACK
        const rows = document.querySelectorAll<HTMLElement>('.side .row.root')
        if (!rows.length) return min
        // .rname 有 ellipsis，被截断后 DOM 读不到完整文本宽 —— 用 canvas 按标题字体量
        const ctx = document.createElement('canvas').getContext('2d')
        if (!ctx) return min
        rows.forEach((row) => {
            const name = row.querySelector<HTMLElement>('.rname')
            if (!name) return
            ctx.font = getComputedStyle(name).font
            let w = SIDE_W_ROW_CHROME + ctx.measureText(name.textContent ?? '').width
            const badge = row.querySelector<HTMLElement>('.badge')
            if (badge) w += badge.offsetWidth + 4
            min = Math.max(min, Math.ceil(w))
        })
        return min
    }

    function maxSideWidth(): number {
        return Math.round(document.documentElement.clientWidth / 2)
    }

    /** 写入 --app-side-w（TitleBar 左侧标识区引用同一变量，宽度变化后自动保持对齐）。 */
    function applySideWidth(w: number): void {
        const clamped = Math.min(maxSideWidth(), Math.max(minSideWidth(), Math.round(w)))
        document.documentElement.style.setProperty('--app-side-w', `${clamped}px`)
    }

    function startSideResize(e: PointerEvent): void {
        e.preventDefault()
        const move = (ev: PointerEvent): void => applySideWidth(ev.clientX)
        const stop = (): void => {
            window.removeEventListener('pointermove', move)
            window.removeEventListener('pointerup', stop)
            window.removeEventListener('pointercancel', stop)
            document.body.classList.remove('dragging', 'col', 'side-resizing')
            const cur = parseFloat(document.documentElement.style.getPropertyValue('--app-side-w'))
            if (cur > 0) localStorage.setItem(SIDE_W_KEY, String(cur))
        }
        document.body.classList.add('dragging', 'col', 'side-resizing')
        window.addEventListener('pointermove', move)
        window.addEventListener('pointerup', stop)
        window.addEventListener('pointercancel', stop)
    }

    /** 窗口缩放后把已保存的侧栏宽夹回 [标题宽, 50%]（如放大后再缩小窗口）。 */
    function clampSideWidth(): void {
        const saved = Number(localStorage.getItem(SIDE_W_KEY))
        if (saved > 0) applySideWidth(saved)
    }

    return { workEl, editorEl, respEl, respSize, respStyle, setLayout, startResize, startSideResize, clampSideWidth }
}

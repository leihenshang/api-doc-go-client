import { findInTextareas, selectTextareaMatch } from '@/lib/find';
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
/**
 * 建立编辑区查找：返回查找条状态、容器 ref（供模板 ref 绑定）与操作入口。
 * 内部维护「关键字/分段变化时重算命中」的 watch 与 window 级 Ctrl+F 监听。
 */
export function useTextareaFind(ctx) {
    const segBodyEl = ref(null);
    const findOn = ref(false);
    const findKw = ref('');
    const findTotal = ref(0);
    const findIndex = ref(-1);
    const findBarRef = ref(null);
    let findMatches = [];
    /** 当前分段是否有可查找的 textarea（Docs 仅编辑态有；预览是渲染 HTML，不接入）。 */
    function findable() {
        const seg = ctx.getSeg();
        if (seg === 'body')
            return ctx.getBodyType() === 'json' || ctx.getBodyType() === 'text';
        if (seg === 'resp')
            return ctx.getRespView?.() === 'body';
        if (seg === 'script')
            return true;
        if (seg === 'docs')
            return ctx.getDocsEditing();
        // gRPC 请求消息是 JSON textarea，同样支持查找
        if (seg === 'message')
            return ctx.isGrpc();
        return false;
    }
    function findTargets() {
        if (!segBodyEl.value)
            return [];
        return Array.from(segBodyEl.value.querySelectorAll('textarea')).filter((ta) => ta.offsetParent !== null && !ta.disabled);
    }
    async function openFind() {
        if (!findable())
            return;
        findOn.value = true;
        await nextTick();
        // 先选中/滚动到第一处，再把焦点交回搜索框：打开即可直接输入关键字
        applyFind();
        findBarRef.value?.focus();
    }
    /**
     * 重算命中并（默认）选中第一处。
     * select=false 用于「编辑区内容变化」：只更新计数，不重新选区/抢焦点，避免光标被拽回第一处。
     */
    function applyFind(options = {}) {
        const { select = true, focus = true } = options;
        findMatches = findInTextareas(findTargets(), findKw.value);
        findTotal.value = findMatches.length;
        findIndex.value = findMatches.length ? 0 : -1;
        if (select && findMatches.length)
            selectTextareaMatch(findMatches[0], focus);
    }
    function findNext() {
        if (!findMatches.length)
            return;
        const i = (findIndex.value + 1) % findMatches.length;
        findIndex.value = i;
        selectTextareaMatch(findMatches[i]);
    }
    function findPrev() {
        if (!findMatches.length)
            return;
        const i = (findIndex.value - 1 + findMatches.length) % findMatches.length;
        findIndex.value = i;
        selectTextareaMatch(findMatches[i]);
    }
    watch([findKw, () => ctx.getSeg(), () => ctx.getDocsEditing(), () => ctx.getBodyType(), () => ctx.getRespView?.(), findOn], async () => {
        // 切到没有可查找 textarea 的分段（或 Docs 切回预览）：查找条自动收起
        if (findOn.value && !findable()) {
            findOn.value = false;
            return;
        }
        if (!findOn.value) {
            findMatches = [];
            findTotal.value = 0;
            findIndex.value = -1;
            return;
        }
        await nextTick();
        applyFind();
        // 关键字/分段变化后把焦点留在搜索框：否则选区聚焦到 textarea，下一字符会输进正文
        findBarRef.value?.focus();
    });
    // window 级监听：编辑器根 div 不可聚焦，焦点在 body / 表格输入框时元素上的 @keydown 收不到
    // 事件（Ctrl+F 不会冒泡经过根 div）。编辑器为活动 tab 单实例，挂 window 安全；
    // 当前分段没有可查找 textarea 时不拦截，交回浏览器原生查找。
    function onGlobalKeydown(e) {
        if (!(e.ctrlKey || e.metaKey) || e.key.toLowerCase() !== 'f' || !findable())
            return;
        // 焦点在响应区内时交给 ResponsePanel（两侧都挂了 window 监听，避免双开）
        if (e.target?.closest('.resp'))
            return;
        e.preventDefault();
        void openFind();
    }
    onMounted(() => window.addEventListener('keydown', onGlobalKeydown));
    onBeforeUnmount(() => window.removeEventListener('keydown', onGlobalKeydown));
    return { segBodyEl, findOn, findKw, findTotal, findIndex, findBarRef, findable, openFind, applyFind, findNext, findPrev };
}

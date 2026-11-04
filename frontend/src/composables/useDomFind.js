import { DomHighlighter } from '@/lib/find';
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
/** 建立响应区查找：返回查找条状态、容器/查找条 ref 与操作入口。 */
export function useDomFind(ctx) {
    const findOn = ref(false);
    const findKw = ref('');
    const findTotal = ref(0);
    const findIndex = ref(-1);
    const paneRef = ref(null);
    const findBarRef = ref(null);
    const highlighter = new DomHighlighter();
    function openFind() {
        findOn.value = true;
        void nextTick(() => findBarRef.value?.focus());
    }
    watch(findOn, (on) => {
        if (!on) {
            findKw.value = '';
            highlighter.clear();
            findTotal.value = 0;
            findIndex.value = -1;
        }
        else {
            void nextTick(() => findBarRef.value?.focus());
        }
    });
    watch([findKw, () => ctx.getSeg(), () => ctx.getView(), () => ctx.getBody(), findOn, () => ctx.getHeaders(), () => ctx.getTrailers()], async () => {
        if (!findOn.value)
            return;
        await nextTick(); // 等页签 / 美化原始切换渲染完
        findTotal.value = highlighter.apply(paneRef.value, findKw.value);
        findIndex.value = findTotal.value ? 0 : -1;
        if (findTotal.value)
            highlighter.select(0);
    });
    function findNext() {
        if (!findTotal.value)
            return;
        highlighter.step(1);
        findIndex.value = highlighter.index;
    }
    function findPrev() {
        if (!findTotal.value)
            return;
        highlighter.step(-1);
        findIndex.value = highlighter.index;
    }
    // window 级监听：面板根 div 不可聚焦，Playwright/用户在非按钮焦点下按 Ctrl+F 时事件落在
    // body，元素上的 @keydown 收不到冒泡。面板为活动 tab 单实例，挂 window 安全。
    function onGlobalKeydown(e) {
        if (!(e.ctrlKey || e.metaKey) || e.key.toLowerCase() !== 'f' || !ctx.hasResponse())
            return;
        const target = e.target;
        // 焦点在请求编辑器内（含编辑器自己的查找条）时交给 RequestEditor
        if (target?.closest('.editor'))
            return;
        // 焦点在中性区域（页签栏 / 空白处 / body）：编辑器当前分段可查找时优先让给编辑器，避免两个查找条同开
        if (!target?.closest('.resp') && document.querySelector('.editor .find-btn'))
            return;
        e.preventDefault();
        openFind();
    }
    onMounted(() => window.addEventListener('keydown', onGlobalKeydown));
    onBeforeUnmount(() => {
        window.removeEventListener('keydown', onGlobalKeydown);
        // Vue 下次重渲染前必须清标记，避免 <mark> 包裹与 Custom Highlight 残留
        highlighter.clear();
    });
    return { findOn, findKw, findTotal, findIndex, paneRef, findBarRef, openFind, findNext, findPrev };
}

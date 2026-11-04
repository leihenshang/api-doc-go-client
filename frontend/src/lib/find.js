// 内容查找（页内查找，统一支持响应只读区与请求编辑区）：
// - TextareaMatcher：原生 textarea（JSON / 文本请求体、脚本、断言、Docs 源码编辑）——
//   纯文本算命中，setSelectionRange 选中并让浏览器自动滚动；可跨同一分段内多个可见 textarea。
// - DomHighlighter：只读渲染区（响应美化树 / 原始文本 / 响应头）——
//   首选 CSS Custom Highlight API（Range 标记，零 DOM 改动）；不支持时退回 <mark> 包裹。
//   历史教训：直接拆改 Vue 托管的文本节点会让 vdom 的 el 引用脱离文档，后续 patch 断链，
//   表现为「搜索后切原始 / 响应头按钮全部失效」。
// 一律大小写不敏感（接口数据 / JSON 场景足够），纯字面量子串匹配（不引入正则副作用）。
/** 在多个 textarea 的 value 里收集全部命中（按元素顺序、元素内位置排序）。 */
export function findInTextareas(els, query) {
    const q = query.toLowerCase();
    if (!q)
        return [];
    const out = [];
    for (const el of els) {
        const hay = el.value.toLowerCase();
        let from = 0;
        for (;;) {
            const idx = hay.indexOf(q, from);
            if (idx < 0)
                break;
            out.push({ el, start: idx, end: idx + query.length });
            from = idx + q.length;
        }
    }
    return out;
}
/** 选中某个 textarea 命中（可只设选区不抢焦点；JsonBodyEditor 的 @scroll 会同步高亮层）。 */
export function selectTextareaMatch(m, focus = true) {
    if (focus)
        m.el.focus();
    m.el.setSelectionRange(m.start, m.end);
}
/** 遍历文本节点时跳过的父节点：脚本 / 样式 / 已有标记不重复包裹。 */
function acceptableText(node, q) {
    const p = node.parentElement;
    if (!p)
        return false;
    if (p.closest('mark.find-hit') || p.tagName === 'SCRIPT' || p.tagName === 'STYLE')
        return false;
    return (p.textContent ?? '').toLowerCase().includes(q);
}
const HL_ALL = 'find-hit';
const HL_CURRENT = 'find-current';
function hlApi() {
    const Ctor = globalThis.Highlight;
    const reg = typeof CSS !== 'undefined' ? CSS.highlights : undefined;
    return Ctor && reg ? { Ctor, reg } : null;
}
/** 单文本节点的全部命中 Range（文档序；与旧路径的切片口径一致）。 */
function rangesInText(node, q) {
    const text = node.nodeValue ?? '';
    const lower = text.toLowerCase();
    const out = [];
    let from = 0;
    for (;;) {
        const idx = lower.indexOf(q, from);
        if (idx < 0)
            return out;
        const r = document.createRange();
        r.setStart(node, idx);
        r.setEnd(node, idx + q.length);
        out.push(r);
        from = idx + q.length;
    }
}
export class DomHighlighter {
    constructor() {
        this.ranges = [];
        this.marks = [];
        this.current = -1;
        /** 非 null = 走零 DOM 改动的 Highlight API 路径 */
        this.api = hlApi();
    }
    /** 标记全部命中，返回命中数；query 为空或 root 缺失时先清空再返回 0。 */
    apply(root, query) {
        this.clear();
        const q = query.trim().toLowerCase();
        if (!root || !q)
            return 0;
        const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
            acceptNode: (node) => (acceptableText(node, q) ? NodeFilter.FILTER_ACCEPT : NodeFilter.FILTER_REJECT),
        });
        const targets = [];
        let n;
        while ((n = walker.nextNode()))
            targets.push(n);
        if (this.api) {
            for (const node of targets)
                this.ranges.push(...rangesInText(node, q));
            this.paint();
            return this.ranges.length;
        }
        for (const node of targets)
            this.wrapOne(node, q);
        this.marks = Array.from(root.querySelectorAll('mark.find-hit'));
        return this.marks.length;
    }
    /** 把当前 ranges 刷进命名高亮层：命中集排除当前项，避免两组叠色。 */
    paint() {
        if (!this.api)
            return;
        const hit = new this.api.Ctor();
        this.ranges.forEach((r, i) => {
            if (i !== this.current)
                hit.add(r);
        });
        this.api.reg.set(HL_ALL, hit);
        const cur = this.ranges[this.current];
        if (cur)
            this.api.reg.set(HL_CURRENT, new this.api.Ctor(cur));
        else
            this.api.reg.delete(HL_CURRENT);
    }
    /** 把单个文本节点按命中切片并包裹（大小写不敏感，切片取原文，保持原样展示）。 */
    wrapOne(node, q) {
        const text = node.nodeValue ?? '';
        const lower = text.toLowerCase();
        const frag = document.createDocumentFragment();
        let from = 0;
        for (;;) {
            const idx = lower.indexOf(q, from);
            if (idx < 0) {
                frag.appendChild(document.createTextNode(text.slice(from)));
                break;
            }
            if (idx > from)
                frag.appendChild(document.createTextNode(text.slice(from, idx)));
            const mark = document.createElement('mark');
            mark.className = 'find-hit';
            mark.textContent = text.slice(idx, idx + q.length);
            frag.appendChild(mark);
            from = idx + q.length;
        }
        node.parentNode?.replaceChild(frag, node);
    }
    /** 选中第 i 个命中（环形）：点亮当前项并滚动到可见区。 */
    select(i) {
        const total = this.api ? this.ranges.length : this.marks.length;
        if (!total) {
            this.current = -1;
            return;
        }
        let idx = i % total;
        if (idx < 0)
            idx += total;
        this.current = idx;
        if (this.api) {
            this.paint();
            // Range 没有自己的盒：滚它的宿主元素（同一行内多个命中滚父级足够）
            this.ranges[idx].startContainer.parentElement?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
            return;
        }
        this.marks.forEach((x, j) => x.classList.toggle('on', j === idx));
        this.marks[idx].scrollIntoView({ block: 'nearest', inline: 'nearest' });
    }
    get index() {
        return this.current;
    }
    /** 相对当前项步进（1 下一个 / -1 上一个，环形）。 */
    step(dir) {
        this.select(this.current + dir);
    }
    /** 拆掉全部标记：命名高亮层直接清空；旧路径还原文本节点（Vue 下次重渲染前必须调用，避免重复包裹）。 */
    clear() {
        if (this.api) {
            this.api.reg.delete(HL_ALL);
            this.api.reg.delete(HL_CURRENT);
            this.ranges = [];
        }
        else {
            for (const m of this.marks) {
                const parent = m.parentNode;
                if (!parent)
                    continue;
                parent.replaceChild(document.createTextNode(m.textContent ?? ''), m);
                parent.normalize();
            }
            this.marks = [];
        }
        this.current = -1;
    }
}

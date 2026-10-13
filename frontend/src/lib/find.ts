// 内容查找（页内查找，统一支持响应只读区与请求编辑区）：
// - TextareaMatcher：原生 textarea（JSON / 文本请求体、脚本、断言、Docs 源码编辑）——
//   纯文本算命中，setSelectionRange 选中并让浏览器自动滚动；可跨同一分段内多个可见 textarea。
// - DomHighlighter：只读渲染区（响应美化树 / 原始文本 / 响应头）——
//   遍历文本节点包裹 <mark class="find-hit">，当前项加 .on 并 scrollIntoView。
// 一律大小写不敏感（接口数据 / JSON 场景足够），纯字面量子串匹配（不引入正则副作用）。

export interface TextareaMatch {
  el: HTMLTextAreaElement
  start: number
  end: number
}

/** 在多个 textarea 的 value 里收集全部命中（按元素顺序、元素内位置排序）。 */
export function findInTextareas(els: HTMLTextAreaElement[], query: string): TextareaMatch[] {
  const q = query.toLowerCase()
  if (!q) return []
  const out: TextareaMatch[] = []
  for (const el of els) {
    const hay = el.value.toLowerCase()
    let from = 0
    for (;;) {
      const idx = hay.indexOf(q, from)
      if (idx < 0) break
      out.push({ el, start: idx, end: idx + query.length })
      from = idx + q.length
    }
  }
  return out
}

/** 选中某个 textarea 命中（聚焦 + 原生选区自动滚动；JsonBodyEditor 的 @scroll 会同步高亮层）。 */
export function selectTextareaMatch(m: TextareaMatch): void {
  m.el.focus()
  m.el.setSelectionRange(m.start, m.end)
}

/** 遍历文本节点时跳过的父节点：脚本 / 样式 / 已有标记不重复包裹。 */
function acceptableText(node: Node, q: string): boolean {
  const p = node.parentElement
  if (!p) return false
  if (p.closest('mark.find-hit') || p.tagName === 'SCRIPT' || p.tagName === 'STYLE') return false
  return (p.textContent ?? '').toLowerCase().includes(q)
}

/**
 * DOM 高亮器：对一个渲染容器做关键字标记 / 取消标记 / 逐项定位。
 * Vue 重渲染会抹掉标记，因此每次内容变化后需重新 apply（调用方在 watcher 里做）。
 */
export class DomHighlighter {
  private marks: HTMLElement[] = []
  private current = -1

  /** 标记全部命中，返回命中数；query 为空或 root 缺失时先清空再返回 0。 */
  apply(root: HTMLElement | null, query: string): number {
    this.clear()
    const q = query.trim().toLowerCase()
    if (!root || !q) return 0
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
      acceptNode: (node) => (acceptableText(node, q) ? NodeFilter.FILTER_ACCEPT : NodeFilter.FILTER_REJECT),
    })
    const targets: Text[] = []
    let n: Node | null
    while ((n = walker.nextNode())) targets.push(n as Text)
    for (const node of targets) this.wrapOne(node, q)
    this.marks = Array.from(root.querySelectorAll('mark.find-hit'))
    return this.marks.length
  }

  /** 把单个文本节点按命中切片并包裹（大小写不敏感，切片取原文，保持原样展示）。 */
  private wrapOne(node: Text, q: string): void {
    const text = node.nodeValue ?? ''
    const lower = text.toLowerCase()
    const frag = document.createDocumentFragment()
    let from = 0
    for (;;) {
      const idx = lower.indexOf(q, from)
      if (idx < 0) {
        frag.appendChild(document.createTextNode(text.slice(from)))
        break
      }
      if (idx > from) frag.appendChild(document.createTextNode(text.slice(from, idx)))
      const mark = document.createElement('mark')
      mark.className = 'find-hit'
      mark.textContent = text.slice(idx, idx + q.length)
      frag.appendChild(mark)
      from = idx + q.length
    }
    node.parentNode?.replaceChild(frag, node)
  }

  /** 选中第 i 个命中（环形）：点亮 .on 并滚动到可见区。 */
  select(i: number): void {
    if (!this.marks.length) {
      this.current = -1
      return
    }
    let idx = i % this.marks.length
    if (idx < 0) idx += this.marks.length
    const m = this.marks[idx]
    this.marks.forEach((x) => x.classList.toggle('on', x === m))
    this.current = idx
    m.scrollIntoView({ block: 'nearest', inline: 'nearest' })
  }

  get index(): number {
    return this.current
  }

  /** 相对当前项步进（1 下一个 / -1 上一个，环形）。 */
  step(dir: 1 | -1): void {
    this.select(this.current + dir)
  }

  /** 拆掉全部标记并还原为文本节点（Vue 下次重渲染前必须调用，避免重复包裹）。 */
  clear(): void {
    for (const m of this.marks) {
      const parent = m.parentNode
      if (!parent) continue
      parent.replaceChild(document.createTextNode(m.textContent ?? ''), m)
      parent.normalize()
    }
    this.marks = []
    this.current = -1
  }
}

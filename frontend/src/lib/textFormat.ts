// 一键格式化的纯文本助手（请求体原文 / 响应原文 / 响应数据共用）。
// 只做缩进与换行，不改变节点顺序、属性与文本内容；无法解析时返回 null，由调用方保持原文。
// JSON 走 JSON.parse/stringify；XML 用 DOMParser 校验后递归序列化（不引第三方依赖）。

export type TextFormatKind = 'json' | 'xml'

const INDENT = '  '

/** 轻量判断（只看首个非空白字符，不解析）：用于格式化按钮的显隐，避免每次输入都解析大文本。 */
export function detectTextKind(text: string): TextFormatKind | null {
    const first = /\S/.exec(text)?.[0]
    if (!first) return null
    if (first === '{' || first === '[') return 'json'
    if (first === '<') return 'xml'
    return null
}

/** JSON 缩进；非法 JSON 返回 null。 */
export function prettyJsonText(text: string): string | null {
    try {
        return JSON.stringify(JSON.parse(text), null, 2)
    } catch {
        return null
    }
}

/** XML 缩进；非法/无法解析返回 null。 */
export function prettyXmlText(text: string): string | null {
    if (typeof DOMParser === 'undefined') return null
    const doc = new DOMParser().parseFromString(text, 'application/xml')
    if (
        !doc.documentElement ||
        doc.getElementsByTagName('parsererror').length > 0 ||
        doc.querySelector('parsererror') ||
        doc.documentElement.nodeName === 'parsererror'
    ) {
        return null
    }

    // DOMParser 不保留 XML 声明；从原文头部取回，避免格式化后丢失 encoding/standalone。
    const decl = text.match(/^\s*(<\?xml[\s\S]*?\?>)/i)?.[1] ?? ''
    const doctype = extractDoctype(text)
    const nodes = Array.from(doc.childNodes)
        .filter((n) => n.nodeType !== Node.DOCUMENT_TYPE_NODE)
        .filter((n) => n.nodeType !== Node.TEXT_NODE || (n.nodeValue ?? '').trim() !== '')
    const body = nodes.map((n) => serializeXmlNode(n, 0)).filter(Boolean).join('\n')
    const out = [decl, doctype, body].filter(Boolean).join('\n')
    return out || null
}

/** 统一入口：按内容类型格式化；非 JSON/XML 或解析失败返回 null。 */
export function prettyText(text: string): { kind: TextFormatKind; text: string } | null {
    const kind = detectTextKind(text)
    if (!kind) return null
    const out = kind === 'json' ? prettyJsonText(text) : prettyXmlText(text)
    return out ? { kind, text: out } : null
}

/** 元素序列化：空白文本节点丢弃；有元素子节点的块级结构逐行缩进；混合内容保持原样（避免改变文本语义）。 */
function serializeXmlNode(node: Node, depth: number): string {
    const pad = INDENT.repeat(depth)

    if (node.nodeType === Node.ELEMENT_NODE) {
        const el = node as Element
        const attrs = serializeAttrs(el)
        const rawChildren = Array.from(el.childNodes)
        const children = rawChildren.filter(
            (c) => c.nodeType !== Node.TEXT_NODE || (c.nodeValue ?? '').trim() !== '',
        )
        const hasElement = rawChildren.some((c) => c.nodeType === Node.ELEMENT_NODE)
        const hasText = rawChildren.some(
            (c) => c.nodeType === Node.TEXT_NODE && (c.nodeValue ?? '').trim() !== '',
        )

        // 混合内容（文本 + 元素）不重排，只去掉外层缩进；强行换行会改变 textContent。
        if (hasElement && hasText) {
            return `${pad}<${el.tagName}${attrs}>${rawChildren.map(serializeXmlInline).join('')}</${el.tagName}>`
        }
        if (!children.length) {
            // 只有空白文本（xml:space="preserve" 等）：保持原样，不改成自闭合。
            if (rawChildren.length) {
                return `${pad}<${el.tagName}${attrs}>${rawChildren.map(serializeXmlInline).join('')}</${el.tagName}>`
            }
            return `${pad}<${el.tagName}${attrs}/>`
        }
        if (!hasElement) {
            // 纯文本 / CDATA / 注释：保持单行，不额外引入空白。
            return `${pad}<${el.tagName}${attrs}>${children.map(serializeXmlInline).join('')}</${el.tagName}>`
        }
        const inner = children.map((c) => serializeXmlNode(c, depth + 1)).join('\n')
        return `${pad}<${el.tagName}${attrs}>\n${inner}\n${pad}</${el.tagName}>`
    }

    if (node.nodeType === Node.TEXT_NODE) {
        const val = node.nodeValue ?? ''
        return val.trim() ? `${pad}${escapeXmlText(val)}` : ''
    }
    if (node.nodeType === Node.CDATA_SECTION_NODE) {
        return `${pad}<![CDATA[${node.nodeValue ?? ''}]]>`
    }
    if (node.nodeType === Node.COMMENT_NODE) {
        return `${pad}<!--${node.nodeValue ?? ''}-->`
    }
    if (node.nodeType === Node.PROCESSING_INSTRUCTION_NODE) {
        const pi = node as ProcessingInstruction
        return `${pad}<?${pi.target}${pi.data ? ` ${pi.data}` : ''}?>`
    }
    return ''
}

/** 混合内容的内联序列化：不加缩进/换行，尽量保持原文本。 */
function serializeXmlInline(node: Node): string {
    if (node.nodeType === Node.ELEMENT_NODE) {
        const el = node as Element
        const attrs = serializeAttrs(el)
        const inner = Array.from(el.childNodes).map(serializeXmlInline).join('')
        return inner ? `<${el.tagName}${attrs}>${inner}</${el.tagName}>` : `<${el.tagName}${attrs}/>`
    }
    if (node.nodeType === Node.TEXT_NODE) return escapeXmlText(node.nodeValue ?? '')
    if (node.nodeType === Node.CDATA_SECTION_NODE) return `<![CDATA[${node.nodeValue ?? ''}]]>`
    if (node.nodeType === Node.COMMENT_NODE) return `<!--${node.nodeValue ?? ''}-->`
    if (node.nodeType === Node.PROCESSING_INSTRUCTION_NODE) {
        const pi = node as ProcessingInstruction
        return `<?${pi.target}${pi.data ? ` ${pi.data}` : ''}?>`
    }
    return ''
}

function serializeAttrs(el: Element): string {
    return Array.from(el.attributes)
        .map((a) => ` ${a.name}="${escapeXmlAttr(a.value)}"`)
        .join('')
}

/** 原文里的 DOCTYPE（含内部子集）；DOMParser 的 doctype 节点会丢内部子集，故从文本取。 */
function extractDoctype(text: string): string {
    const m = /<!DOCTYPE[^[>]*(?:\[[\s\S]*?\])?[^>]*>/i.exec(text)
    return m?.[0] ?? ''
}

function escapeXmlText(s: string): string {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

function escapeXmlAttr(s: string): string {
    return s.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;')
}

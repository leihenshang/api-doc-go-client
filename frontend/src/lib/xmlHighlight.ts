// XML 着色：与 JSON 高亮同一套「片段 + 类名」约定（保序、不丢字符）。
// 只用于编辑器的展示层，不参与解析；半成品 XML / 未闭合标签同样按字符原样输出。
import type { JsonPart } from './jsonHighlight'

const WS = new Set([' ', '\t', '\n', '\r'])
const TAG_END = new Set([' ', '\t', '\n', '\r', '/', '>'])

/** 把 XML 文本切成带类名的片段：标签/属性名 k、属性值 s、标点 p，正文不着色。 */
export function highlightXml(src: string): JsonPart[] {
    const out: JsonPart[] = []
    const push = (text: string, cls = ''): void => {
        if (!text) return
        const last = out[out.length - 1]
        if (last && last.cls === cls) last.text += text
        else out.push({ text, cls })
    }

    const n = src.length
    let i = 0
    while (i < n) {
        if (src.startsWith('<!--', i)) {
            const end = src.indexOf('-->', i + 4)
            const next = end < 0 ? n : end + 3
            push(src.slice(i, next), 'p')
            i = next
            continue
        }
        if (src.startsWith('<![CDATA[', i)) {
            const end = src.indexOf(']]>', i + 9)
            const next = end < 0 ? n : end + 3
            push(src.slice(i, next))
            i = next
            continue
        }
        if (src[i] !== '<') {
            const start = i
            while (i < n && src[i] !== '<') i++
            push(src.slice(start, i))
            continue
        }
        // <?xml ...?> / <?...?>：整体压暗，避免把声明里的引号误识别成属性
        if (src.startsWith('<?', i)) {
            const end = src.indexOf('?>', i + 2)
            const next = end < 0 ? n : end + 2
            push(src.slice(i, next), 'p')
            i = next
            continue
        }

        push('<', 'p')
        i++
        if (src[i] === '/') {
            push('/', 'p')
            i++
        }
        const nameStart = i
        while (i < n && !TAG_END.has(src[i])) i++
        if (i > nameStart) push(src.slice(nameStart, i), 'k')

        // 属性区：name / = / "value" / 空白；遇到 > 结束
        while (i < n && src[i] !== '>') {
            if (src[i] === '/' && src[i + 1] === '>') {
                push('/', 'p')
                i++
                continue
            }
            if (WS.has(src[i])) {
                const start = i
                while (i < n && WS.has(src[i])) i++
                push(src.slice(start, i))
                continue
            }
            if (src[i] === '=') {
                push('=', 'p')
                i++
                continue
            }
            if (src[i] === '"' || src[i] === "'") {
                const quote = src[i]
                let j = i + 1
                while (j < n && src[j] !== quote) j++
                j = Math.min(j + 1, n)
                push(src.slice(i, j), 's')
                i = j
                continue
            }
            const start = i
            while (i < n && !WS.has(src[i]) && src[i] !== '=' && src[i] !== '>' && src[i] !== '/') i++
            if (i === start) {
                push(src[i])
                i++
            } else {
                push(src.slice(start, i), 'k')
            }
        }
        if (src[i] === '>') {
            push('>', 'p')
            i++
        }
    }
    return out
}

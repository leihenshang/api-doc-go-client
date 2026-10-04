// 请求体 JSON 高亮：把文本切成「片段 + 类名」，由模板渲染成 <span>。
//
// 两条设计约束：
//  1. 只做「分词」，不拼 HTML 字符串 → 不需要 v-html，天然没有注入风险，也不会因为
//     转义细节（& < 引号）和文本内容打架；
//  2. 解析失败也**不报错、不丢字符**：认不出来的字符原样输出，颜色退化但编辑体验不受影响
//     （JSON 编辑过程中必然有一段时间是半成品）。
export interface JsonPart {
  /** 片段原文 */
  text: string
  /** 类名：k=键 s=字符串 n=数字 b=布尔 z=null p=标点 v={{变量}} ''=其他 */
  cls: string
}

const WS = new Set([' ', '\t', '\n', '\r'])
const PUNCT = '{}[],:'

/** 把 JSON 文本切成带类名的片段（保序、无丢失）。 */
export function highlightJson(src: string): JsonPart[] {
  const out: JsonPart[] = []
  const n = src.length
  // 合并同色相邻片段：一个长 JSON 会因此少掉大量节点，渲染更省
  const push = (text: string, cls = ''): void => {
    if (!text) return
    const last = out[out.length - 1]
    if (last && last.cls === cls) last.text += text
    else out.push({ text, cls })
  }

  let i = 0
  while (i < n) {
    const ch = src[i]
    if (WS.has(ch)) {
      push(ch)
      i++
      continue
    }
    if (ch === '"') {
      const { text, next } = readString(src, i)
      // 是键还是值？看它后面第一个非空白字符是不是冒号
      let j = next
      while (j < n && WS.has(src[j])) j++
      pushValue(text, src[j] === ':' ? 'k' : 's', push)
      i = next
      continue
    }
    if (ch === '-' || (ch >= '0' && ch <= '9')) {
      const m = /^-?\d+(\.\d+)?([eE][+-]?\d+)?/.exec(src.slice(i, i + 32))
      if (m) {
        push(m[0], 'n')
        i += m[0].length
        continue
      }
    }
    if (src.startsWith('true', i) || src.startsWith('false', i)) {
      const lit = src.startsWith('true', i) ? 'true' : 'false'
      push(lit, 'b')
      i += lit.length
      continue
    }
    if (src.startsWith('null', i)) {
      push('null', 'z')
      i += 4
      continue
    }
    if (PUNCT.includes(ch)) {
      push(ch, 'p')
      i++
      continue
    }
    push(ch) // 认不出来的字符（半成品 JSON、注释等）：原样展示
    i++
  }
  return out
}

/** 读一个字符串字面量（含首尾引号；遇到反斜杠跳过下一个字符）。 */
function readString(src: string, start: number): { text: string; next: number } {
  let i = start + 1
  while (i < src.length) {
    if (src[i] === '\\') {
      i += 2
      continue
    }
    if (src[i] === '"') {
      i++
      break
    }
    i++
  }
  return { text: src.slice(start, i), next: i }
}

const VAR_RE = /\{\{[^{}]*\}\}/g

/**
 * 写一个字符串片段：串内的 {{变量}} 单独着色（与 URL/参数输入框里的变量高亮同一套视觉），
 * 其余部分用字符串/键的颜色。
 */
function pushValue(text: string, cls: string, push: (t: string, c?: string) => void): void {
  let last = 0
  VAR_RE.lastIndex = 0
  let m: RegExpExecArray | null
  while ((m = VAR_RE.exec(text)) !== null) {
    if (m.index > last) push(text.slice(last, m.index), cls)
    push(m[0], 'v')
    last = m.index + m[0].length
  }
  if (last < text.length) push(text.slice(last), cls)
}

/** 文本里是否含 {{变量}} 占位符。 */
export function hasVarPlaceholder(src: string): boolean {
  VAR_RE.lastIndex = 0
  return VAR_RE.test(src)
}

/**
 * 是否是「可解析的 JSON」。
 * 含 {{变量}} 时不判断（`{"id": {{id}}}` 这类模板化请求体本来就无法直接 parse，
 * 不该因此报红），由调用方用 hasVarPlaceholder 先筛。
 */
export function isParsableJson(src: string): boolean {
  try {
    JSON.parse(src)
    return true
  } catch {
    return false
  }
}

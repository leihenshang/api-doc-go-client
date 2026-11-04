const WS = new Set([' ', '\t', '\n', '\r']);
const PUNCT = '{}[],:';
/** 把 JSON 文本切成带类名的片段（保序、无丢失）。 */
export function highlightJson(src) {
    const out = [];
    const n = src.length;
    // 合并同色相邻片段：一个长 JSON 会因此少掉大量节点，渲染更省
    const push = (text, cls = '') => {
        if (!text)
            return;
        const last = out[out.length - 1];
        if (last && last.cls === cls)
            last.text += text;
        else
            out.push({ text, cls });
    };
    let i = 0;
    while (i < n) {
        const ch = src[i];
        if (WS.has(ch)) {
            push(ch);
            i++;
            continue;
        }
        if (ch === '"') {
            const { text, next } = readString(src, i);
            // 是键还是值？看它后面第一个非空白字符是不是冒号
            let j = next;
            while (j < n && WS.has(src[j]))
                j++;
            pushValue(text, src[j] === ':' ? 'k' : 's', push);
            i = next;
            continue;
        }
        if (ch === '-' || (ch >= '0' && ch <= '9')) {
            const m = /^-?\d+(\.\d+)?([eE][+-]?\d+)?/.exec(src.slice(i, i + 32));
            if (m) {
                push(m[0], 'n');
                i += m[0].length;
                continue;
            }
        }
        if (src.startsWith('true', i) || src.startsWith('false', i)) {
            const lit = src.startsWith('true', i) ? 'true' : 'false';
            push(lit, 'b');
            i += lit.length;
            continue;
        }
        if (src.startsWith('null', i)) {
            push('null', 'z');
            i += 4;
            continue;
        }
        if (PUNCT.includes(ch)) {
            push(ch, 'p');
            i++;
            continue;
        }
        push(ch); // 认不出来的字符（半成品 JSON、注释等）：原样展示
        i++;
    }
    return out;
}
/** 读一个字符串字面量（含首尾引号；遇到反斜杠跳过下一个字符）。 */
function readString(src, start) {
    let i = start + 1;
    while (i < src.length) {
        if (src[i] === '\\') {
            i += 2;
            continue;
        }
        if (src[i] === '"') {
            i++;
            break;
        }
        i++;
    }
    return { text: src.slice(start, i), next: i };
}
const VAR_RE = /\{\{[^{}]*\}\}/g;
/**
 * 写一个字符串片段：串内的 {{变量}} 单独着色（与 URL/参数输入框里的变量高亮同一套视觉），
 * 其余部分用字符串/键的颜色。
 */
function pushValue(text, cls, push) {
    let last = 0;
    VAR_RE.lastIndex = 0;
    let m;
    while ((m = VAR_RE.exec(text)) !== null) {
        if (m.index > last)
            push(text.slice(last, m.index), cls);
        push(m[0], 'v');
        last = m.index + m[0].length;
    }
    if (last < text.length)
        push(text.slice(last), cls);
}
/** 文本里是否含 {{变量}} 占位符。 */
export function hasVarPlaceholder(src) {
    VAR_RE.lastIndex = 0;
    return VAR_RE.test(src);
}
/**
 * 是否是「可解析的 JSON」。
 * 含 {{变量}} 时不判断（`{"id": {{id}}}` 这类模板化请求体本来就无法直接 parse，
 * 不该因此报红），由调用方用 hasVarPlaceholder 先筛。
 */
export function isParsableJson(src) {
    try {
        JSON.parse(src);
        return true;
    }
    catch {
        return false;
    }
}

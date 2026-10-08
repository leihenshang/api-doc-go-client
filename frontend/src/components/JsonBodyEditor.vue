<script setup lang="ts">
// 请求体 JSON 编辑器：**透明文字的 textarea 叠一层着色层**（与 VarInput 同一套做法）。
//
// 为什么不换 CodeMirror/Monaco：需求只是「JSON 着色」，换编辑器会带来依赖体积、主题适配，
// 以及与现有 undo/redo（页签快照）、Ctrl+S 落盘逻辑的对接成本。叠加法保留原生 textarea 的
// 全部行为（光标、选区、输入法、Ctrl+Z、原生滚动），只把颜色画在下层，编辑路径零改动。
//
// 两层必须严格重叠：同一字体（--app-mono）、同字号/行高/内边距/换行规则，否则光标会落在
// 字形上（VarInput 里踩过：两层字体不一致时同一串文本宽度差 48px，光标与文字错位）。
import { highlightJson } from '@/lib/jsonHighlight';
import { computed, nextTick, ref } from 'vue';

const props = defineProps<{
  modelValue: string
  placeholder?: string
  /** 行数（只影响高度：高度 = 行数 × 行高 + 上下内边距） */
  rows?: number
}>()
const emit = defineEmits<{ 'update:modelValue': [v: string]; input: [] }>()

const hl = ref<HTMLElement | null>(null)
const ta = ref<HTMLTextAreaElement | null>(null)
const parts = computed(() => highlightJson(props.modelValue))

function onInput(ev: Event): void {
  emit('update:modelValue', (ev.target as HTMLTextAreaElement).value)
  emit('input')
}

/** 回落一个新值并把光标放到指定位置（配合受控 v-model）。 */
function commit(next: string, caret: number): void {
  emit('update:modelValue', next)
  emit('input')
  void nextTick().then(() => {
    const el2 = ta.value
    if (!el2) return
    el2.focus()
    el2.setSelectionRange(caret, caret)
  })
}

// 配对的开放/闭合字符表：`{`/`[`/`"` 自动补闭合并把光标居中；再输闭合符或碰到已闭合则跳过。
// JSON 只处理这三对（用户已确认不含单引号/圆括号）。
const PAIR: Record<string, [string, string]> = {
  '{': ['{', '}'],
  '[': ['[', ']'],
  ']': ['[', ']'],
  '}': ['{', '}'],
  '"': ['"', '"'],
}

function onKeydown(e: KeyboardEvent): void {
  if (e.metaKey || e.ctrlKey || e.altKey) return
  const pair = PAIR[e.key]
  if (!pair) return
  const el2 = ta.value
  if (!el2) return
  const s = el2.selectionStart
  const en = el2.selectionEnd
  const v = props.modelValue
  const [open, close] = pair
  // 有选区：用「open + 选中 + close」包裹
  if (en > s) {
    e.preventDefault()
    commit(v.slice(0, s) + open + v.slice(s, en) + close + v.slice(en), s + open.length)
    return
  }
  // 无选区：光标后已是闭合符（typed over）→ 只右移光标，不重复自补
  if (v[s] === close) {
    e.preventDefault()
    el2.setSelectionRange(s + 1, s + 1)
    return
  }
  // 输入的是开放符 → 补「开放 + 闭合」并把光标放中间
  if (e.key === '{' || e.key === '[' || e.key === '"') {
    e.preventDefault()
    commit(v.slice(0, s) + open + close + v.slice(s), s + open.length)
  }
  // 其余（单独补闭合符且光标后没有闭合）→ 放行默认输入单个字符
}

/** 滚动同步：textarea 负责滚动，高亮层跟随（否则两层会错位）。 */
function sync(ev: Event): void {
  const t = ev.target as HTMLTextAreaElement
  const el2 = hl.value
  if (!el2) return
  el2.scrollTop = t.scrollTop
  el2.scrollLeft = t.scrollLeft
}
</script>

<template>
  <div class="jb" :style="{ '--jb-rows': String(props.rows ?? 12) }">
    <!-- 着色层：aria-hidden，不接收指针事件（编辑只发生在 textarea 上）。
         用 div 而不是 <pre>：Vue 对 <pre> 会强制保留模板换行，会在文本里混入空白。 -->
    <div ref="hl" class="hl mono" aria-hidden="true">
      <span v-for="(p, i) in parts" :key="i" :class="p.cls">{{ p.text }}</span>
      <span v-if="!modelValue" class="ph">{{ props.placeholder }}</span>
    </div>
    <textarea
      ref="ta"
      class="ta mono"
      :value="props.modelValue"
      spellcheck="false"
      autocomplete="off"
      data-testid="req.bodyRaw"
      @input="onInput"
      @keydown="onKeydown"
      @scroll="sync"
    />
  </div>
</template>

<style scoped>
.jb {
  position: relative;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: var(--app-panel);
  /* 高度按行数算，观感与原来的 textarea rows=12 一致 */
  height: calc(var(--jb-rows, 12) * var(--jb-lh) + 20px);
  overflow: hidden;
}

.jb:focus-within {
  border-color: var(--app-accent);
}

.hl,
.ta {
  position: absolute;
  inset: 0;
  margin: 0;
  padding: 10px;
  border: none;
  font-family: var(--app-mono);
  font-size: 12.5px;
  line-height: var(--jb-lh);
  /* 换行规则两层必须完全一致（soft wrap + 长串断行），否则行数与高度会分叉 */
  white-space: pre-wrap;
  word-break: break-word;
  overflow-wrap: anywhere;
  tab-size: 2;
}

.ta {
  background: transparent;
  /* 文字透明、只留光标与选区：颜色由下层着色层显示 */
  color: transparent;
  caret-color: var(--app-text);
  resize: none;
  overflow: auto;
  outline: none;
}

.ta::selection {
  background: var(--app-active);
}

.hl {
  color: var(--app-text);
  pointer-events: none;
  overflow: hidden;
}

.ph {
  color: var(--app-placeholder);
}

/* ---- JSON 语法着色 ----
   复用既有的「代码」调色板 --app-code-*（明暗各一套），与响应里的代码/JSON 观感一致；
   **不要**动 --app-json-*（那是响应 JSON 树专用，另有配色）。 */
.hl .k {
  color: var(--app-code-key);
}

.hl .s {
  color: var(--app-code-string);
}

.hl .n {
  color: var(--app-code-number);
}

.hl .b {
  color: var(--app-code-boolean);
}

.hl .z {
  color: var(--app-code-null);
}

/* 标点（{}[],:）压暗一档，让结构不抢内容的注意力 */
.hl .p {
  color: var(--app-muted);
}

/* {{变量}}：与 URL/参数输入框里的变量同一套视觉（主色 + 淡底） */
.hl .v {
  color: var(--app-accent-dark);
  background: var(--app-accent-tint);
  border-radius: 3px;
}
</style>

<script setup lang="ts">
// 变量高亮输入框（design-spec §6.7）：文字透明的原生 input 叠一层高亮文本，
// 使 {{变量}} 显示为主色底纹；input 保留真实光标与选区。
// 变量取值不再单独占一行展示（原「替换后」预览行已移除），改为鼠标悬停在变量上时给出：
// 已定义 → 显示取值（敏感变量只显示掩码）；未定义 → 提示去「环境设置…」里补；内置变量 → 说明发送时生成。
import { computed, nextTick, ref } from 'vue';
import { useI18n } from 'vue-i18n';

const props = defineProps<{
  modelValue: string
  placeholder?: string
  /** 已定义变量的取值（名字 → 值），来自后端 ResolveText */
  vars?: Record<string, string>
  /** 文本里引用了但未定义的变量名 */
  missing?: string[]
  /** 敏感变量名：提示里只显示掩码 */
  secrets?: string[]
  /** 变量自动提示候选（当前环境变量名等）；不传则禁用自动提示 */
  suggestions?: string[]
}>()
const emit = defineEmits<{ 'update:modelValue': [v: string] }>()
const { t } = useI18n()

// 透传属性（如 data-testid）要落在真实 input 上，而不是高亮层的外层 div
defineOptions({ inheritAttrs: false })

interface Part {
  text: string
  cls: string
  /** 变量片段才有：花括号内的名字（内置变量带 $ 前缀） */
  name: string
}

type TipKind = 'value' | 'secret' | 'empty' | 'missing' | 'builtin'

interface Tip {
  name: string
  kind: TipKind
  value: string
  left: number
}

const NAME_RE = /^\{\{\s*(\$?[A-Za-z_][A-Za-z0-9_]*)\s*\}\}$/

// 敏感值的展示掩码（与共享包 varx.MaskedValue 保持一致）
const SECRET_MASK = '••••••'

const hl = ref<HTMLElement | null>(null)
const wrap = ref<HTMLElement | null>(null)
const el = ref<HTMLInputElement | null>(null)
const tip = ref<Tip | null>(null)

// ---- 变量自动提示（输入 {{ 前缀后弹出候选，Tab 录入）----
// 候选 = 使用方传入的环境变量名 + 内置动态变量；不传 suggestions 则整体禁用。
const BUILTIN_NAMES = ['$uuid', '$timestamp', '$isoTimestamp', '$randomInt']
const sugOpen = ref(false)
const sugs = ref<string[]>([])
const sugHi = ref(0)
/** 当前 open `{{` 后变量名的起点（用于替换补全） */
let sugOpenIdx = -1

const suggestPool = computed<string[]>(() => {
  const set = new Set<string>(props.suggestions ?? [])
  for (const n of BUILTIN_NAMES) set.add(n)
  return Array.from(set)
})

/** 根据文本与光标位置判定当前是否处于一个 open `{{…` 内，若是则返回已输入变量名前缀（可能为空串）。 */
function varPrefixAt(text: string, sel: number): string | null {
  const head = text.slice(0, sel)
  const openCount = (head.match(/\{\{/g) ?? []).length
  const closeCount = (head.match(/\}\}/g) ?? []).length
  if (openCount <= closeCount) return null // 不在 open 花括号内
  // 取光标左侧最近的一段「{{ + 空白 + 变量名前缀」；只输入到 {{（或后随空白）记为空前缀，也弹候选
  const m = /(\{\{\s*)?([A-Za-z_$][A-Za-z0-9_]*)?$/.exec(head)
  const prefix = m?.[2] ?? ''
  sugOpenIdx = sel - prefix.length // 变量名起点 = 紧跟 open {{ 之后
  return prefix
}

function openSuggest(): void {
  const input = el.value
  if (!input || !(props.suggestions?.length)) return
  // 用实时 input 值（prop 在 emit 后异步更新，会滞后一个渲染）
  const prefix = varPrefixAt(input.value || '', input.selectionStart ?? 0)
  if (prefix === null) {
    sugOpen.value = false
    return
  }
  const lower = prefix.toLowerCase()
  const list = suggestPool.value.filter((n) => n.toLowerCase().startsWith(lower))
  if (!list.length) {
    sugOpen.value = false
    return
  }
  sugs.value = list
  sugHi.value = sugOpen.value ? Math.min(sugHi.value, list.length - 1) : 0
  sugOpen.value = true
}

async function applySuggestion(): Promise<void> {
  const input = el.value
  if (!input || !sugOpen.value || !sugs.value[sugHi.value]) return
  const name = sugs.value[sugHi.value]
  const sel = input.selectionStart ?? 0
  const v = input.value
  // 在应用时按当前光标重新定位 open `{{` 后的变量名起点（方向键可能移动过光标）
  if (varPrefixAt(v, sel) === null) {
    sugOpen.value = false
    return
  }
  const start = sugOpenIdx
  sugOpen.value = false
  if (start < 0 || start > sel) return
  // 光标后若有一段同变量名的残余并以 `}}` 结尾（用户把名字打完、又移回中间），整段吞掉、不再补闭合
  const rest = v.slice(sel)
  const closePos = rest.indexOf('}}')
  const hadClose = closePos >= 0 && /^[A-Za-z_$][A-Za-z0-9_]*$/.test(rest.slice(0, closePos))
  const end = hadClose ? sel + closePos : sel
  // 替换 [start, end) 后立刻补闭合 `}}`，再把原光标之后的文本（如 /test）接在闭合符后面：{{name}}/test
  const next = v.slice(0, start) + name + (hadClose ? '' : '}}') + v.slice(end)
  emit('update:modelValue', next)
  const caret = start + name.length
  await nextTick()
  input.focus()
  input.setSelectionRange(caret, caret)
}

function onKeydown(e: KeyboardEvent): void {
  if (!sugOpen.value) return
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    sugHi.value = (sugHi.value + 1) % sugs.value.length
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    sugHi.value = (sugHi.value - 1 + sugs.value.length) % sugs.value.length
  } else if (e.key === 'Tab' || e.key === 'Enter') {
    e.preventDefault()
    void applySuggestion()
  } else if (e.key === 'Escape') {
    sugOpen.value = false
  }
}

function onBlur(): void {
  sugOpen.value = false
  tip.value = null
}

const parts = computed<Part[]>(() => {
  const out: Part[] = []
  const re = /\{\{[^{}]*\}\}/g
  let last = 0
  let m: RegExpExecArray | null
  while ((m = re.exec(props.modelValue)) !== null) {
    if (m.index > last) out.push({ text: props.modelValue.slice(last, m.index), cls: '', name: '' })
    const name = NAME_RE.exec(m[0])?.[1] ?? ''
    out.push({ text: m[0], cls: name && props.missing?.includes(name) ? 'var miss' : 'var', name })
    last = m.index + m[0].length
  }
  if (last < props.modelValue.length) out.push({ text: props.modelValue.slice(last), cls: '', name: '' })
  return out
})

// 高亮层不随 input 滚动，需手动同步横向偏移
function sync(): void {
  if (hl.value && el.value) hl.value.scrollLeft = el.value.scrollLeft
}

function onInput(e: Event): void {
  emit('update:modelValue', (e.target as HTMLInputElement).value)
  tip.value = null
  openSuggest()
  sync()
}

function focus(): void {
  el.value?.focus()
}

// 判定某个变量该给出什么提示；null = 解析结果还没回来，先不提示（避免误报「未定义」）
function classify(name: string): Omit<Tip, 'name' | 'left'> | null {
  if (name.startsWith('$')) return { kind: 'builtin', value: '' }
  if (props.missing?.includes(name)) return { kind: 'missing', value: '' }
  const values = props.vars ?? {}
  if (!(name in values)) return null
  const value = values[name]
  if (value === '') return { kind: 'empty', value: '' }
  // 敏感变量只给掩码：真正的明文留在环境设置里看（避免悬停就把密钥摊开）
  if (props.secrets?.includes(name)) return { kind: 'secret', value: SECRET_MASK }
  return { kind: 'value', value }
}

// 光标落在哪个变量片段上：高亮层与 input 完全重叠，直接用变量片段的客户区矩形判定
function onMove(e: MouseEvent): void {
  const layer = hl.value
  const box = wrap.value
  if (!layer || !box) return
  const spans = layer.querySelectorAll<HTMLElement>('span[data-name]')
  let hit: HTMLElement | null = null
  for (const s of spans) {
    const r = s.getBoundingClientRect()
    if (e.clientX >= r.left && e.clientX <= r.right) {
      hit = s
      break
    }
  }
  if (!hit) {
    tip.value = null
    return
  }
  const name = hit.dataset.name ?? ''
  const info = name ? classify(name) : null
  if (!info) {
    tip.value = null
    return
  }
  const boxRect = box.getBoundingClientRect()
  const spanRect = hit.getBoundingClientRect()
  const maxLeft = Math.max(0, boxRect.width - 320)
  tip.value = {
    name,
    ...info,
    left: Math.min(Math.max(0, spanRect.left - boxRect.left), maxLeft),
  }
}

function onLeave(): void {
  tip.value = null
}
</script>

<template>
  <div ref="wrap" class="vw">
    <div class="vi" :class="{ 'has-miss': (missing?.length ?? 0) > 0 }" @click="focus">
      <div ref="hl" class="hl mono" aria-hidden="true">
        <span v-for="(p, i) in parts" :key="i" :class="p.cls" :data-name="p.name || undefined">{{ p.text }}</span>
        <span v-if="!modelValue" class="ph">{{ placeholder }}</span>
      </div>
      <input
        ref="el"
        v-bind="$attrs"
        class="in mono"
        :value="modelValue"
        spellcheck="false"
        autocomplete="off"
        @input="onInput"
        @scroll="sync"
        @mousemove="onMove"
        @mouseleave="onLeave"
        @blur="onBlur"
        @keydown="onKeydown"
      />
    </div>

    <!-- 变量自动提示浮层：光标落在 {{… 内时给出候选，Tab/Enter 录入 -->
    <div
      v-if="sugOpen"
      class="sug mono"
      data-testid="var.suggest"
    >
      <div
        v-for="(s, i) in sugs"
        :key="s"
        class="sug-item"
        :class="{ on: i === sugHi }"
        data-testid="var.suggest.item"
        @mouseenter="sugHi = i"
        @mousedown.prevent="applySuggestion"
      >
        {{ s }}
      </div>
    </div>

    <div v-if="tip" class="tip mono" :style="{ left: `${tip.left}px` }" data-testid="req.varTip">
      <template v-if="tip.kind === 'missing'">{{ t('editor.varUndefined', { name: tip.name }) }}</template>
      <template v-else-if="tip.kind === 'builtin'">{{ t('editor.varBuiltin', { name: tip.name }) }}</template>
      <template v-else-if="tip.kind === 'secret'">
        <b>{{ tip.name }}</b> = {{ tip.value }}<span class="note">{{ t('editor.varSecret') }}</span>
      </template>
      <template v-else-if="tip.kind === 'empty'">
        <b>{{ tip.name }}</b> = <span class="note">{{ t('editor.varEmpty') }}</span>
      </template>
      <template v-else>
        <b>{{ tip.name }}</b> = {{ tip.value }}
      </template>
    </div>
  </div>
</template>

<style scoped>
.vw {
  position: relative;
  flex: 1 1 auto;
  min-width: 0;
}

.vi {
  position: relative;
  /* 高度由使用方通过 --vi-h 指定，保证高亮层与 input 完全重叠 */
  height: var(--vi-h, 28px);
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: var(--app-panel);
  overflow: hidden;
}

.vi:focus-within {
  border-color: var(--app-accent);
}

/* 有未定义变量时描边给一点警告色，具体是哪个变量由悬停提示说明 */
.vi.has-miss {
  border-color: var(--app-warn);
}

.hl,
.in {
  position: absolute;
  inset: 0;
  padding: 0 10px;
  font-size: 13px;
  /* 两层必须用同一套字体：文字宽度决定光标位置，宽度不一致时光标会落在高亮文字的字形上
     （表现为「URL 输入框的文字与光标重叠」）。此处显式声明，不再依赖 .mono 类 ——
     组件内 `.in { font-family: inherit }` 的选择器优先级高于全局 `.mono`，曾导致
     input 用正文比例字体（Noto Sans SC）、高亮层用等宽字体（JetBrains Mono），
     同一串文本宽度差 48px（225 vs 273）。 */
  font-family: var(--app-mono);
  line-height: calc(var(--vi-h, 28px) - 2px);
  white-space: pre;
  overflow: hidden;
}

.hl {
  color: var(--app-text);
  pointer-events: none;
}

.hl .var {
  color: var(--app-accent-dark);
  background: var(--app-accent-tint);
  border-radius: 3px;
}

.hl .var.miss {
  color: var(--app-warn);
  background: var(--app-warn-tint);
}

.ph {
  color: var(--app-placeholder);
}

.in {
  border: none;
  outline: none;
  background: transparent;
  color: transparent;
  caret-color: var(--app-text);
  width: 100%;
}

/* 悬停提示：定位在变量左边缘下方，可溢出输入框（.vw 不裁切） */
.tip {
  position: absolute;
  top: calc(var(--vi-h, 28px) + 4px);
  z-index: 20;
  max-width: min(420px, 100%);
  padding: 4px 8px;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: var(--app-surface-2);
  box-shadow: var(--app-shadow-sm, 0 4px 12px rgb(0 0 0 / 12%));
  color: var(--app-text);
  font-size: 11.5px;
  line-height: 1.5;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  pointer-events: none;
}

/* 变量自动提示浮层：定位在输入框下方，可溢出输入框（.vw 不裁切） */
.sug {
  position: absolute;
  top: calc(var(--vi-h, 28px) + 4px);
  left: 0;
  z-index: 30;
  min-width: 160px;
  max-width: min(320px, 100%);
  max-height: 200px;
  overflow: auto;
  padding: 4px;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: var(--app-surface-2);
  box-shadow: var(--app-shadow-sm, 0 4px 12px rgb(0 0 0 / 12%));
}

.sug-item {
  padding: 4px 8px;
  border-radius: 4px;
  font-size: 12px;
  color: var(--app-text);
  cursor: pointer;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.sug-item.on,
.sug-item:hover {
  background: var(--app-accent-tint);
  color: var(--app-accent-dark);
}

.tip b {
  color: var(--app-accent-dark);
  font-weight: 600;
}

.tip .note {
  margin-left: 6px;
  color: var(--app-muted);
}
</style>

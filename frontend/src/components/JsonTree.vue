<script setup lang="ts">
// JSON 树（response-panel design-spec §3/§4）：行高 24、每级缩进 20px、hover 行圆角 4；
// key 绿 / 字符串橙 / 数组索引蓝 / 折叠箭头灰 10×10。非 JSON 由调用方回退纯文本。
// 大响应保留行数上限，避免一次渲染上万行卡住界面。
import { NIcon } from 'naive-ui'
import { ChevronDownOutline, ChevronForwardOutline } from '@vicons/ionicons5'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const MAX_ROWS = 2000

const props = defineProps<{ text: string; wrap: boolean }>()
const { t } = useI18n()

const collapsed = ref(new Set<string>())

interface Row {
  depth: number
  path: string
  key: string
  /** 数组下标：着索引蓝 */
  isIndex: boolean
  /** 折叠节点显示 {…} / […] 与子项数量 */
  value: string
  type: 'string' | 'number' | 'boolean' | 'null' | 'object' | 'array'
  expandable: boolean
  open: boolean
  tail: string
}

// 解析结果用 ok 标记，避免 JSON 标量（0/false/null）被当成"解析失败"
const parsed = computed<{ ok: true; value: unknown } | { ok: false }>(() => {
  try {
    return { ok: true, value: JSON.parse(props.text) }
  } catch {
    return { ok: false }
  }
})

function childCount(v: unknown): number {
  if (Array.isArray(v)) return v.length
  if (v && typeof v === 'object') return Object.keys(v as object).length
  return 0
}

function typeOf(v: unknown): Row['type'] {
  if (v === null) return 'null'
  if (Array.isArray(v)) return 'array'
  switch (typeof v) {
    case 'number':
      return 'number'
    case 'boolean':
      return 'boolean'
    case 'object':
      return 'object'
    default:
      return 'string'
  }
}

function display(v: unknown): string {
  if (typeof v === 'string') {
    const s = v.length > 600 ? `${v.slice(0, 600)}…` : v
    return JSON.stringify(s)
  }
  if (v === null) return 'null'
  if (typeof v === 'object') return Array.isArray(v) ? '[…]' : '{…}'
  return String(v)
}

const rows = computed<Row[]>(() => {
  const out: Row[] = []
  if (!parsed.value.ok) return out
  const data = parsed.value.value

  const walk = (v: unknown, key: string, depth: number, path: string, last: boolean, isIndex: boolean): void => {
    if (out.length >= MAX_ROWS) return
    const type = typeOf(v)
    const expandable = type === 'object' || type === 'array'
    const isOpen = expandable && !collapsed.value.has(path)
    out.push({
      depth,
      path,
      key,
      isIndex,
      type,
      expandable,
      open: isOpen,
      value: expandable && !isOpen ? (type === 'array' ? `[…] ${childCount(v)}` : `{…} ${childCount(v)}`) : display(v),
      tail: last ? '' : ',',
    })
    if (!expandable || !isOpen) return
    const entries: [string, unknown, boolean][] = Array.isArray(v)
      ? v.map((x, i) => [String(i), x, true] as [string, unknown, boolean])
      : Object.entries(v as Record<string, unknown>).map(([k, x]) => [k, x, false] as [string, unknown, boolean])
    entries.forEach(([k, child, idx], i) => {
      walk(child, k, depth + 1, `${path}/${k}`, i === entries.length - 1, idx)
    })
  }

  const rootType = typeOf(data)
  if (rootType === 'object' || rootType === 'array') {
    walk(data, '', 0, '$', true, false)
  } else {
    out.push({ depth: 0, path: '$', key: '', isIndex: false, type: rootType, expandable: false, open: false, value: display(data), tail: '' })
  }
  return out
})

const truncated = computed(() => rows.value.length >= MAX_ROWS)

function toggle(row: Row): void {
  if (!row.expandable) return
  const next = new Set(collapsed.value)
  if (next.has(row.path)) next.delete(row.path)
  else next.add(row.path)
  collapsed.value = next
}

function collapseAll(): void {
  const all = new Set<string>()
  for (const r of rows.value) if (r.expandable && r.depth > 0) all.add(r.path)
  collapsed.value = all
}

function expandAll(): void {
  collapsed.value = new Set()
}

defineExpose({ collapseAll, expandAll })
</script>

<template>
  <div class="jt mono" :class="{ wrap }" data-testid="resp.json">
    <div v-if="!parsed.ok" class="muted hint">{{ t('json.invalid') }}</div>
    <template v-else>
      <div v-for="(row, i) in rows" :key="i" class="jr" :title="row.path">
        <span class="indent" :style="{ width: row.depth * 20 + 'px' }" />
        <span class="caret" :class="{ dim: !row.expandable }" @click="toggle(row)">
          <n-icon
            v-if="row.expandable"
            :component="row.open ? ChevronDownOutline : ChevronForwardOutline"
            :size="10"
          />
        </span>
        <span v-if="row.key !== ''" class="k" :class="{ idx: row.isIndex }">{{ row.key }}</span>
        <span v-if="row.key !== ''" class="colon">:</span>
        <span class="v" :class="row.type">{{ row.value }}</span>
        <span class="tail">{{ row.tail }}</span>
      </div>
      <div v-if="truncated" class="muted hint">{{ t('json.truncated', { n: MAX_ROWS }) }}</div>
    </template>
  </div>
</template>

<style scoped>
.jt {
  font-size: 12px;
  line-height: 24px;
  padding: 12px 16px;
}

.jr {
  display: flex;
  align-items: center;
  height: 24px;
  white-space: pre;
  border-radius: 4px;
}

/* 换行开关：打开后长值折行，行高放开 */
.jt.wrap .jr {
  height: auto;
  min-height: 24px;
  white-space: pre-wrap;
  word-break: break-all;
}

.jr:hover {
  background: var(--app-code-bg);
}

.indent {
  flex: 0 0 auto;
}

.caret {
  width: 12px;
  height: 10px;
  flex: 0 0 auto;
  color: var(--app-muted);
  cursor: pointer;
  font-size: 10px;
  text-align: center;
  display: inline-flex;
  align-items: center;
}

.caret.dim {
  cursor: default;
}

/* 着色对齐 response-panel design-spec §2：key 绿 / 字符串橙 / 数组索引蓝 */
.k {
  color: var(--app-json-key);
}

.k.idx {
  color: var(--app-json-idx);
}

.colon {
  color: var(--app-placeholder);
  margin-right: 4px;
}

.v.string {
  color: var(--app-json-str);
}

.v.number {
  color: var(--app-code-number);
}

.v.boolean {
  color: var(--app-code-boolean);
}

.v.null {
  color: var(--app-code-null);
}

.v.object,
.v.array {
  color: var(--app-muted);
}

.tail {
  color: var(--app-placeholder);
}

.hint {
  font-size: 11.5px;
  padding: 4px 2px;
}
</style>

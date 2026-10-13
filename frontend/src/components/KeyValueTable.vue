<script setup lang="ts">
// 键值表（参数 / 请求头 / 表单体通用）：表格式编辑 + 批量编辑（每行 key: value）。
// 批量模式按「名称」回填原有启用态与说明，避免来回切换丢字段。
// showType 时增加「类型」列（multipart 的 text / file，file 的 value 是文件路径）。
import { api } from '@/lib/ipc'
import type { KV } from '@/types'
import { CloseOutline, FolderOpenOutline } from '@vicons/ionicons5'
import { NButton, NCheckbox, NIcon, NInput, NSelect } from 'naive-ui'
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ rows: KV[]; label: string; showType?: boolean }>()
const emit = defineEmits<{ change: [] }>()
const { t } = useI18n()

const bulk = ref(false)
const bulkText = ref('')

const columns = computed(() => {
  const cols = [t('editor.colName'), t('editor.colValue')]
  if (props.showType) cols.push(t('editor.colType'))
  cols.push(t('editor.colDesc'))
  return cols
})

const typeOptions = computed(() => [
  { label: t('editor.partText'), value: 'text' },
  { label: t('editor.partFile'), value: 'file' },
])

// 列宽可拖拽：列宽以 fr 权重存储（默认沿用改动前的 1.1 / 1.4 / 1.4 比例），
// 拖拽把像素位移换算成权重增量 —— 任何状态下各列都按比例分享宽度，
// 「说明」列不会像「固定 px + 唯一弹性列」那样在宽窗口下吞掉全部剩余空间。
// 同一组 grid-template-columns 同时作用于表头与每一行，拖任意一行的输入框右缘即可整列变宽。
const MIN_FR = 0.3
const weights = reactive({ name: 1.1, value: 1.4, desc: 1.4 })
const gridCols = computed(() => {
  const f = (n: number): string => `${Math.round(n * 100) / 100}fr`
  return props.showType
    ? `24px ${f(weights.name)} ${f(weights.value)} 88px 24px ${f(weights.desc)} 24px`
    : `24px ${f(weights.name)} ${f(weights.value)} ${f(weights.desc)} 24px`
})

let drag: { key: 'name' | 'value' | 'desc'; startX: number; startFr: number; pxPerFr: number } | null = null
function onDragMove(e: PointerEvent): void {
  if (!drag) return
  weights[drag.key] = Math.max(MIN_FR, drag.startFr + (e.clientX - drag.startX) / drag.pxPerFr)
}
function onDragEnd(): void {
  if (!drag) return
  drag = null
  window.removeEventListener('pointermove', onDragMove)
  window.removeEventListener('pointerup', onDragEnd)
}
function startResize(key: 'name' | 'value' | 'desc', e: PointerEvent): void {
  e.preventDefault()
  // 量出当前列的渲染宽度，换算「1fr = 多少 px」，拖拽位移才能按比例映射回权重
  const cell = (e.currentTarget as HTMLElement).parentElement
  const pxW = cell ? cell.getBoundingClientRect().width : 0
  const pxPerFr = pxW > 0 ? pxW / weights[key] : 200
  drag = { key, startX: e.clientX, startFr: weights[key], pxPerFr }
  window.addEventListener('pointermove', onDragMove)
  window.addEventListener('pointerup', onDragEnd)
}
onBeforeUnmount(onDragEnd)

/** 一行空数据（新参数 / 表单行）。 */
function blankKV(): KV {
  return { name: '', value: '', enabled: true, type: props.showType ? 'text' : undefined }
}

/** 该行是否已有输入：名称 / 值 / 说明任一非空（只有说明也算，别把已写的备注当空行覆盖掉）。 */
function hasInput(r: KV): boolean {
  return r.name.trim() !== '' || r.value.trim() !== '' || (r.description ?? '').trim() !== ''
}

/** 末尾始终留一行空行：末行一有输入就自动补一行，空表也补一行 —— 省去每次手动点「添加一行」。
 *  只有用户的真实编辑才由输入事件 emit('change') 标脏；这里补的占位空行**不标脏**，
 *  否则打开请求 / 切换页签（URL 回填参数表）都会冒出未保存圆点。 */
function ensureTrailingBlank(): void {
  const last = props.rows[props.rows.length - 1]
  if (last && !hasInput(last)) return
  props.rows.push(blankKV())
}

function add(): void {
  props.rows.push(blankKV())
  emit('change')
}

onMounted(ensureTrailingBlank)
watch(() => props.rows, ensureTrailingBlank, { deep: true })

function del(i: number): void {
  props.rows.splice(i, 1)
  emit('change')
}

async function pickFile(i: number): Promise<void> {
  try {
    const path = await api.pickFile()
    if (path) {
      props.rows[i].value = path
      emit('change')
    }
  } catch {
    // 用户取消或非桌面环境：忽略
  }
}

function toText(): string {
  return props.rows
    .filter((r) => r.name.trim() !== '')
    .map((r) => `${r.name}: ${r.value}`)
    .join('\n')
}

// 提交批量文本；空行与 # 注释忽略，同名的旧行沿用 enabled / description / type
function apply(): void {
  const prev = new Map(props.rows.map((r) => [r.name, r]))
  const next: KV[] = []
  for (const raw of bulkText.value.split('\n')) {
    const line = raw.trim()
    if (!line || line.startsWith('#')) continue
    const sep = line.indexOf(':')
    const name = (sep < 0 ? line : line.slice(0, sep)).trim()
    if (!name) continue
    const old = prev.get(name)
    next.push({
      name,
      value: sep < 0 ? '' : line.slice(sep + 1).trim(),
      enabled: old?.enabled ?? true,
      description: old?.description,
      type: old?.type,
    })
  }
  props.rows.splice(0, props.rows.length, ...next)
  emit('change')
}

function toggle(): void {
  if (bulk.value) apply()
  else bulkText.value = toText()
  bulk.value = !bulk.value
}
</script>

<template>
  <div class="kvt" data-testid="kv">
    <div class="sec">
      <span class="sec-lb">{{ label }}</span>
      <span class="sp" />
      <button class="link" type="button" data-testid="kv.bulk" @click="toggle">
        {{ bulk ? t('common.tableEdit') : t('common.bulkEdit') }}
      </button>
    </div>

    <template v-if="!bulk">
      <div class="th" :class="{ typed: showType }" :style="{ gridTemplateColumns: gridCols }">
        <span class="ck" />
        <span v-for="c in columns" :key="c">{{ c }}</span>
        <span v-if="showType" class="op" />
        <span class="op" />
      </div>
      <div v-for="(row, i) in rows" :key="i" class="row" :class="{ typed: showType }"
        :style="{ gridTemplateColumns: gridCols }" data-testid="kv.row">
        <n-checkbox v-model:checked="row.enabled" size="small" @update:checked="emit('change')" />
        <div class="cell">
          <n-input v-model:value="row.name" data-testid="kv.name" size="small" placeholder="name"
            @input="emit('change')" />
          <span class="rs" data-testid="kv.resizeName" @pointerdown.prevent="startResize('name', $event)" />
        </div>
        <div class="cell">
          <n-input v-model:value="row.value" data-testid="kv.value" size="small" placeholder="value"
            @input="emit('change')" />
          <span class="rs" data-testid="kv.resizeValue" @pointerdown.prevent="startResize('value', $event)" />
        </div>
        <n-select v-if="showType" v-model:value="row.type" data-testid="kv.type" size="small" class="type"
          :options="typeOptions" @update:value="emit('change')" />
        <!-- 只在 showType 时渲染：非 showType 的 grid 只有 5 列（无 op 格），
             多出的空 span 会把说明输入框挤进 24px 删除列、删除按钮溢出到下一行 -->
        <span v-if="showType" class="op">
          <button v-if="showType && row.type === 'file'" class="act pick" type="button" data-testid="kv.pickFile"
            :title="t('editor.pickFile')" @click="pickFile(i)">
            <n-icon :component="FolderOpenOutline" :size="13" />
          </button>
        </span>
        <div class="cell">
          <n-input v-model:value="row.description" data-testid="kv.desc" size="small"
            :placeholder="t('common.optional')" @input="emit('change')" />
          <span class="rs" data-testid="kv.resizeDesc" @pointerdown.prevent="startResize('desc', $event)" />
        </div>
        <button class="act" type="button" data-testid="kv.remove" :title="t('common.delete')" @click="del(i)">
          <n-icon :component="CloseOutline" :size="13" />
        </button>
      </div>
      <n-button text size="tiny" type="primary" class="add" data-testid="kv.add" @click="add">{{ t('editor.addRow')
        }}</n-button>
    </template>

    <div v-else class="bulk">
      <p class="bulk-hint">{{ t('editor.bulkHint') }}</p>
      <n-input v-model:value="bulkText" type="textarea" :rows="Math.min(14, Math.max(4, rows.length + 1))"
        class="mono bulk-text" data-testid="kv.bulkText" @blur="apply" />
    </div>
  </div>
</template>

<style scoped>
.kvt {
  display: flex;
  flex-direction: column;
}

.sec {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 2px 0 6px;
}

.sec-lb {
  font-size: 12px;
  font-weight: 500;
  color: var(--app-text-2);
}

.sp {
  flex: 1 1 auto;
}

.link {
  border: none;
  background: none;
  padding: 0;
  font-size: 12px;
  font-family: inherit;
  color: var(--app-accent);
  cursor: pointer;
}

.link:hover {
  text-decoration: underline;
}

.th,
.row {
  display: grid;
  align-items: center;
  gap: 8px;
}

/* name / value 单元格：相对定位，右侧挂拖拽手柄 */
.cell {
  position: relative;
  display: flex;
  min-width: 0;
}

.cell :deep(.n-input) {
  width: 100%;
}

/* 列宽拖拽手柄：悬停单元格时显示分隔条，拖动改变 name / value 列宽 */
.rs {
  position: absolute;
  top: 0;
  right: -5px;
  width: 10px;
  height: 100%;
  cursor: col-resize;
  z-index: 2;
  touch-action: none;
}

.rs::after {
  content: '';
  position: absolute;
  top: 4px;
  bottom: 4px;
  right: 4px;
  width: 2px;
  border-radius: 1px;
  background: var(--app-border);
  opacity: 0;
  transition: opacity 0.12s;
}

.cell:hover .rs::after {
  opacity: 1;
}

.rs:hover::after {
  background: var(--app-accent);
  opacity: 1;
}

.th {
  padding: 6px 8px;
  border: 1px solid var(--app-border);
  border-radius: 6px 6px 0 0;
  background: var(--app-surface-2);
  font-size: 11.5px;
  color: var(--app-muted);
}

.row {
  padding: 5px 8px;
  border: 1px solid var(--app-border);
  border-top: none;
}

.row:last-of-type {
  border-radius: 0 0 6px 6px;
}

.row:hover {
  background: var(--app-hover-soft);
}

.ck,
.op {
  width: 24px;
}

.act {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: none;
  padding: 0;
  color: var(--app-placeholder);
  cursor: pointer;
}

.act:hover {
  color: var(--app-danger);
}

.add {
  align-self: flex-start;
  margin-top: 8px;
}

.bulk {
  border: 1px solid var(--app-border);
  border-radius: 8px;
  padding: 10px 12px 12px;
  background: var(--app-surface-2);
}

.bulk-hint {
  margin: 0 0 8px;
  font-size: 11.5px;
  color: var(--app-muted);
}

.bulk-text {
  font-size: 12.5px;
}
</style>

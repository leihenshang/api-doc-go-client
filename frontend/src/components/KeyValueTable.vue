<script setup lang="ts">
// 键值表（参数 / 请求头 / 表单体通用）：表格式编辑 + 批量编辑（每行 key: value）。
// 批量模式按「名称」回填原有启用态与说明，避免来回切换丢字段。
// showType 时增加「类型」列（multipart 的 text / file，file 的 value 是文件路径）。
import { NButton, NCheckbox, NIcon, NInput, NSelect } from 'naive-ui'
import { CloseOutline, FolderOpenOutline } from '@vicons/ionicons5'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/ipc'
import type { KV } from '@/types'

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

function add(): void {
  props.rows.push({ name: '', value: '', enabled: true, type: props.showType ? 'text' : undefined })
  emit('change')
}

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
      <div class="th" :class="{ typed: showType }">
        <span class="ck" />
        <span v-for="c in columns" :key="c">{{ c }}</span>
        <span v-if="showType" class="op" />
        <span class="op" />
      </div>
      <div
        v-for="(row, i) in rows"
        :key="i"
        class="row"
        :class="{ typed: showType, 'no-pick': showType && row.type !== 'file' }"
        data-testid="kv.row"
      >
        <n-checkbox v-model:checked="row.enabled" size="small" @update:checked="emit('change')" />
        <n-input v-model:value="row.name" data-testid="kv.name" size="small" placeholder="name" @input="emit('change')" />
        <n-input v-model:value="row.value" data-testid="kv.value" size="small" placeholder="value" @input="emit('change')" />
        <n-select
          v-if="showType"
          v-model:value="row.type"
          data-testid="kv.type"
          size="small"
          class="type"
          :options="typeOptions"
          @update:value="emit('change')"
        />
        <button
          v-if="showType && row.type === 'file'"
          class="act pick"
          type="button"
          data-testid="kv.pickFile"
          :title="t('editor.pickFile')"
          @click="pickFile(i)"
        >
          <n-icon :component="FolderOpenOutline" :size="13" />
        </button>
        <n-input
          v-model:value="row.description"
          data-testid="kv.desc"
          size="small"
          :placeholder="t('common.optional')"
          @input="emit('change')"
        />
        <button class="act" type="button" data-testid="kv.remove" :title="t('common.delete')" @click="del(i)">
          <n-icon :component="CloseOutline" :size="13" />
        </button>
      </div>
      <n-button text size="tiny" type="primary" class="add" data-testid="kv.add" @click="add">{{ t('editor.addRow') }}</n-button>
    </template>

    <div v-else class="bulk">
      <p class="bulk-hint">{{ t('editor.bulkHint') }}</p>
      <n-input
        v-model:value="bulkText"
        type="textarea"
        :rows="Math.min(14, Math.max(4, rows.length + 1))"
        class="mono bulk-text"
        data-testid="kv.bulkText"
        @blur="apply"
      />
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
  grid-template-columns: 24px minmax(0, 1.1fr) minmax(0, 1.4fr) minmax(0, 1.4fr) 24px;
  align-items: center;
  gap: 8px;
}

/* multipart：多一列类型 + 文件选择按钮 */
.th.typed,
.row.typed {
  grid-template-columns: 24px minmax(0, 1fr) minmax(0, 1.2fr) 88px 24px minmax(0, 1fr) 24px;
}

.row.typed.no-pick {
  grid-template-columns: 24px minmax(0, 1fr) minmax(0, 1.2fr) 88px minmax(0, 1fr) 24px;
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

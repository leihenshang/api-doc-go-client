<script setup lang="ts">
// 回收站（实际问题记录 item 20/21）：列出当前工作区 .trash 的删除记录；
// 每条支持——预览（文件内容 / 目录清单）、恢复（点击后下拉选恢复位置：原工作区或指定工作区）、
// 彻底删除（不可恢复，删除前二次确认）。
import { api } from '@/lib/ipc'
import { dialog, message } from '@/lib/notice'
import { useCollectionStore } from '@/stores/collection'
import type { TrashEntry, TrashPreview } from '@/types'
import type { DropdownOption } from 'naive-ui'
import { NButton, NDropdown, NModal } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ 'update:show': [v: boolean] }>()
const { t } = useI18n()
const coll = useCollectionStore()

const items = ref<TrashEntry[]>([])
const error = ref('')
/** 正在恢复的条目文件名（恢复期间禁用其它操作） */
const busy = ref('')
/** 展开预览的条目文件名（'' = 收起）；预览数据懒加载 */
const previewFile = ref('')
const previewData = ref<TrashPreview | null>(null)

/** 恢复位置选项：默认「原工作区（原路径）」，也可选任意已打开的工作区。 */
const restoreDestOptions = computed<DropdownOption[]>(() => [
  { label: t('trash.targetOriginal'), key: '' },
  ...coll.roots.map((r) => ({ label: r.info.name || r.root, key: r.root })),
])

watch(
  () => props.show,
  (v) => {
    if (v) void reload()
  },
)

function msg(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

async function reload(): Promise<void> {
  error.value = ''
  previewFile.value = ''
  previewData.value = null
  try {
    items.value = await api.listTrash()
  } catch (e) {
    error.value = msg(e)
  }
}

function kindLabel(k: string): string {
  if (k === 'folder') return t('trash.kindFolder')
  if (k === 'env') return t('trash.kindEnv')
  if (k === 'request') return t('trash.kindRequest')
  return t('trash.kindFile')
}

function clock(ms: number): string {
  return ms ? new Date(ms).toLocaleString() : '—'
}

/** 预览：同一行再点一次收起；换行则重新拉取。 */
async function togglePreview(e: TrashEntry): Promise<void> {
  if (previewFile.value === e.file) {
    previewFile.value = ''
    previewData.value = null
    return
  }
  previewFile.value = e.file
  previewData.value = null
  try {
    previewData.value = await api.previewTrash(e.file)
  } catch (err) {
    error.value = msg(err)
    previewFile.value = ''
  }
}

/** 恢复：下拉选完位置立即执行；目标重名会被后端拒绝并显示原因。 */
async function onRestoreSelect(key: string | number, e: TrashEntry): Promise<void> {
  const target = String(key)
  busy.value = e.file
  error.value = ''
  try {
    const rel = await api.restoreTrash(e.file, '', target)
    message.success(t('trash.restored', { path: rel }))
    await reload()
    // 恢复改变了哪个工作区就刷新哪棵（跨工作区时目标树要出现新文件）
    if (target && target !== coll.activeRoot) await coll.reloadRoot(target)
    else await coll.reload()
  } catch (err) {
    error.value = msg(err)
  } finally {
    busy.value = ''
  }
}

/** 彻底删除：不可恢复，先弹居中确认框（与关闭集合同风格）。 */
function askRemove(e: TrashEntry): void {
  dialog.warning({
    title: t('trash.remove'),
    content: t('trash.removeConfirm', { name: e.name }),
    positiveText: t('common.delete'),
    negativeText: t('common.cancel'),
    onPositiveClick: async () => {
      try {
        await api.deleteFromTrash(e.file)
        message.success(t('trash.removed'))
        await reload()
      } catch (err) {
        error.value = msg(err)
      }
    },
  })
}
</script>

<template>
  <n-modal :show="show" preset="card" :title="t('trash.title')" style="width: 680px"
    @update:show="emit('update:show', false)">
    <div class="wrap">
      <div class="hd">
        <span class="muted">{{ t('trash.count', { n: items.length }) }}</span>
        <span class="sp" />
        <n-button size="tiny" tertiary data-testid="trash.reload" @click="reload">{{ t('trash.refresh') }}</n-button>
      </div>

      <p v-if="error" class="err" data-testid="trash.error">{{ error }}</p>
      <div v-if="!items.length && !error" class="empty muted">{{ t('trash.empty') }}</div>

      <div v-for="e in items" :key="e.file" class="cell" data-testid="trash.row">
        <div class="row">
          <span class="kind">{{ kindLabel(e.kind) }}</span>
          <span class="nm mono" :title="e.origin || e.name">{{ e.name }}</span>
          <span class="org mono muted" :title="e.origin">{{ e.origin || '—' }}</span>
          <span class="time muted">{{ clock(e.deletedAt) }}</span>
          <button class="mini" type="button" data-testid="trash.preview"
            @click="togglePreview(e)">{{ previewFile === e.file ? t('trash.collapse') : t('trash.preview') }}</button>
          <n-dropdown trigger="click" placement="bottom-end" :options="restoreDestOptions"
            @select="(k: string | number) => onRestoreSelect(k, e)">
            <button class="mini primary" type="button" data-testid="trash.restore" :disabled="busy !== ''">
              {{ t('trash.restore') }}
            </button>
          </n-dropdown>
          <button class="mini danger" type="button" data-testid="trash.remove" :disabled="busy !== ''"
            @click="askRemove(e)">
            {{ t('trash.remove') }}
          </button>
        </div>
        <!-- 预览：文件给内容（截断），目录给文件清单 -->
        <div v-if="previewFile === e.file" class="preview" data-testid="trash.previewBody">
          <pre v-if="previewData?.kind === 'file'" class="body mono">{{ previewData.content || '—' }}</pre>
          <ul v-else-if="previewData" class="flist">
            <li v-for="f in previewData.files ?? []" :key="f" class="mono">{{ f }}</li>
            <li v-if="!(previewData.files ?? []).length" class="muted">—</li>
          </ul>
          <div v-if="previewData?.truncated" class="muted cut">{{ t('trash.previewTruncated') }}</div>
        </div>
      </div>
    </div>
  </n-modal>
</template>

<style scoped>
.wrap {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 60vh;
  overflow: auto;
}

.hd {
  display: flex;
  align-items: center;
  gap: 8px;
}

.sp {
  flex: 1 1 auto;
}

.cell {
  border-radius: 5px;
}

.row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px 6px;
  border-radius: 5px;
  font-size: 12.5px;
}

.row:hover {
  background: var(--app-row-hover);
}

.kind {
  flex: 0 0 auto;
  font-size: 10.5px;
  padding: 0 6px;
  border-radius: 4px;
  background: var(--app-chip);
  color: var(--app-text-2);
}

.nm {
  flex: 0 1 auto;
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--app-text);
}

.org {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 11.5px;
}

.time {
  flex: 0 0 auto;
  font-size: 11.5px;
}

/* 行内小操作按钮：预览（中性）/ 恢复（主色）/ 彻底删除（危险） */
.mini {
  flex: 0 0 auto;
  border: 1px solid var(--app-border);
  background: var(--app-panel);
  color: var(--app-text-2);
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 4px;
  cursor: pointer;
}

.mini:hover:not(:disabled) {
  border-color: var(--app-accent);
  color: var(--app-accent);
}

.mini.primary {
  color: var(--app-accent);
}

.mini.danger:hover:not(:disabled) {
  border-color: var(--app-danger);
  color: var(--app-danger);
}

.mini:disabled {
  opacity: 0.5;
  cursor: default;
}

.preview {
  padding: 2px 8px 8px 34px;
}

.preview .body {
  margin: 0;
  padding: 8px 10px;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: var(--app-surface-2);
  font-size: 11px;
  line-height: 1.55;
  max-height: 200px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
}

.preview .flist {
  margin: 0;
  padding: 6px 10px;
  list-style: none;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: var(--app-surface-2);
  font-size: 11.5px;
  max-height: 200px;
  overflow: auto;
}

.preview .cut {
  font-size: 11px;
  margin-top: 4px;
}

.empty {
  padding: 24px;
  text-align: center;
}

.err {
  color: var(--app-danger);
  font-size: 12.5px;
  margin: 0;
}
</style>

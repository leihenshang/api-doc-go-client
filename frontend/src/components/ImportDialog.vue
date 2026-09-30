<script setup lang="ts">
// 导入（H9）：Postman Collection JSON / OpenAPI JSON → 当前集合的指定分组。
import { NButton, NInput, NModal, NSelect } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/ipc'
import { message } from '@/lib/notice'
import { useCollectionStore } from '@/stores/collection'
import type { ImportSummary, TreeNode } from '@/types'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ 'update:show': [v: boolean]; imported: [] }>()
const { t } = useI18n()
const coll = useCollectionStore()

const format = ref('postman')
const text = ref('')
const dest = ref('')
const error = ref('')
const summary = ref<ImportSummary | null>(null)
const importing = ref(false)
const brunoDir = ref('')

const formatOptions = computed(() => [
  { label: t('import.formatPostman'), value: 'postman' },
  { label: t('import.formatOpenapi'), value: 'openapi' },
  { label: t('import.formatBruno'), value: 'bruno' },
])

const destOptions = computed(() => {
  const out: { label: string; value: string }[] = [{ label: t('tree.moveRoot'), value: '' }]
  const walk = (nodes: TreeNode[]): void => {
    for (const n of nodes) {
      if (n.type !== 'folder') continue
      out.push({ label: n.name, value: n.path })
      if (n.children) walk(n.children)
    }
  }
  walk(coll.tree)
  return out
})

watch(
  () => props.show,
  (v) => {
    if (!v) return
    text.value = ''
    error.value = ''
    summary.value = null
    dest.value = ''
  },
)

async function pickFile(): Promise<void> {
  // 复用文件选择后读内容：桌面端 PickFile 只给路径，这里用浏览器 FileReader 读粘贴/拖入；
  // 也支持 <input type=file>（浏览器与桌面 webview 都可用）
  const input = document.createElement('input')
  input.type = 'file'
  input.accept = '.json,.yaml,.yml,application/json'
  input.onchange = async () => {
    const f = input.files?.[0]
    if (!f) return
    text.value = await f.text()
  }
  input.click()
}

async function submit(): Promise<void> {
  if (format.value === 'bruno') {
    if (!brunoDir.value.trim()) {
      error.value = t('import.needBrunoDir')
      return
    }
    importing.value = true
    error.value = ''
    summary.value = null
    try {
      const sum = await api.importBrunoDir(brunoDir.value)
      summary.value = sum
      message.success(t('import.done', { n: sum.imported, s: sum.skipped }))
      emit('imported')
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      importing.value = false
    }
    return
  }
  if (!text.value.trim()) {
    error.value = t('import.needContent')
    return
  }
  importing.value = true
  error.value = ''
  summary.value = null
  try {
    const sum = await api.importCollection(dest.value, format.value, text.value)
    summary.value = sum
    message.success(t('import.done', { n: sum.imported, s: sum.skipped }))
    emit('imported')
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    importing.value = false
  }
}
</script>

<template>
  <n-modal
    :show="show"
    preset="card"
    :title="t('import.title')"
    style="width: 680px"
    @update:show="emit('update:show', false)"
  >
    <div class="wrap">
      <div class="row">
        <span class="lbl">{{ t('import.format') }}</span>
        <n-select v-model:value="format" :options="formatOptions" size="small" class="fmt" data-testid="import.format" />
      </div>
      <div class="row">
        <span class="lbl">{{ t('import.dest') }}</span>
        <n-select v-model:value="dest" :options="destOptions" size="small" class="fmt" data-testid="import.dest" />
      </div>
      <div v-if="format === 'bruno'" class="row">
        <span class="lbl">{{ t('import.brunoDir') }}</span>
        <n-input v-model:value="brunoDir" size="small" data-testid="import.brunoDir" :placeholder="t('import.brunoDirPlaceholder')" />
      </div>
      <div v-else class="row">
        <span class="lbl">{{ t('import.content') }}</span>
        <n-button size="small" tertiary data-testid="import.pick" @click="pickFile">
          {{ t('import.pickFile') }}
        </n-button>
      </div>
      <n-input
        v-if="format !== 'bruno'"
        v-model:value="text"
        type="textarea"
        :rows="12"
        class="mono area"
        data-testid="import.text"
        :placeholder="t('import.placeholder')"
      />
      <p v-if="error" class="err">{{ error }}</p>
      <div v-if="summary" class="sum" data-testid="import.summary">
        <span>{{ t('import.done', { n: summary.imported, s: summary.skipped }) }}</span>
        <ul v-if="summary.failures?.length" class="fails">
          <li v-for="(f, i) in summary.failures" :key="i">{{ f }}</li>
        </ul>
      </div>
    </div>
    <template #footer>
      <div class="ft">
        <n-button size="small" @click="emit('update:show', false)">{{ t('common.cancel') }}</n-button>
        <n-button size="small" type="primary" :loading="importing" data-testid="import.ok" @click="submit">
          {{ t('import.submit') }}
        </n-button>
      </div>
    </template>
  </n-modal>
</template>

<style scoped>
.wrap {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.lbl {
  width: 72px;
  font-size: 12px;
  color: var(--app-muted);
}

.fmt {
  flex: 1;
}

.area {
  font-size: 12px;
}

.err {
  margin: 0;
  color: var(--app-danger, #d03050);
  font-size: 12px;
}

.sum {
  font-size: 12px;
  color: var(--app-accent);
}

.fails {
  margin: 6px 0 0;
  padding-left: 18px;
  color: var(--app-danger, #d03050);
}

.ft {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>

<script setup lang="ts">
// 项目字段字典：字段路径 → 类型/说明，供接口返回参数复用。
// 单文件 field_mappings.yml 存储；关联服务端项目后随集合双向同步（按条目摘要脏检测）。
import { api } from '@/lib/ipc'
import { message } from '@/lib/notice'
import type { FieldMapping } from '@/types'
import { NButton, NIcon, NInput, NModal, NSelect } from 'naive-ui'
import { SaveOutline, TrashOutline } from '@vicons/ionicons5'
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ 'update:show': [v: boolean] }>()
const { t } = useI18n()

const rows = ref<FieldMapping[]>([])
const busyIdx = ref(-1)
const error = ref('')

const typeOptions = ['string', 'integer', 'number', 'boolean', 'object', 'array', 'any'].map((v) => ({
  label: v,
  value: v,
}))

watch(
  () => props.show,
  async (v) => {
    if (v) await load()
  },
)

async function load(): Promise<void> {
  error.value = ''
  try {
    rows.value = (await api.listFieldMappings()).map((m) => ({ ...m }))
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

function addRow(): void {
  rows.value.push({ uid: '', field: '', type: 'string', description: '' })
}

async function saveRow(i: number): Promise<void> {
  const row = rows.value[i]
  if (!row) return
  if (!row.field.trim() || !row.type.trim()) {
    error.value = t('fieldMap.required')
    return
  }
  error.value = ''
  busyIdx.value = i
  try {
    const saved = await api.saveFieldMapping({
      uid: row.uid,
      field: row.field.trim(),
      type: row.type.trim(),
      description: (row.description ?? '').trim(),
    })
    if (saved) rows.value.splice(i, 1, saved)
    message.success(t('fieldMap.saved'))
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busyIdx.value = -1
  }
}

async function removeRow(i: number): Promise<void> {
  const row = rows.value[i]
  if (!row) return
  error.value = ''
  busyIdx.value = i
  try {
    if (row.uid) await api.deleteFieldMapping(row.uid)
    rows.value.splice(i, 1)
    message.success(t('fieldMap.deleted'))
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busyIdx.value = -1
  }
}
</script>

<template>
  <n-modal :show="show" preset="card" :title="t('fieldMap.title')" style="width: 780px"
    data-testid="fieldMap.dialog" @update:show="emit('update:show', false)">
    <div class="wrap">
      <p class="hint">{{ t('fieldMap.hint') }}</p>

      <div class="head">
        <span class="c-field">{{ t('fieldMap.field') }}</span>
        <span class="c-type">{{ t('fieldMap.type') }}</span>
        <span class="c-desc">{{ t('fieldMap.desc') }}</span>
        <span class="c-ops"></span>
      </div>

      <div v-if="rows.length === 0" class="empty" data-testid="fieldMap.empty">{{ t('fieldMap.empty') }}</div>

      <div v-for="(row, i) in rows" :key="row.uid || 'new-' + i" class="row" data-testid="fieldMap.row">
        <n-input v-model:value="row.field" size="small" :placeholder="t('fieldMap.fieldHint')"
          data-testid="fieldMap.field" />
        <n-select v-model:value="row.type" size="small" filterable tag :options="typeOptions"
          data-testid="fieldMap.type" />
        <n-input v-model:value="row.description" size="small" :placeholder="t('fieldMap.descHint')"
          data-testid="fieldMap.desc" />
        <div class="ops">
          <n-button size="tiny" secondary :loading="busyIdx === i" :title="t('fieldMap.save')"
            data-testid="fieldMap.save" @click="saveRow(i)">
            <template #icon>
              <n-icon :component="SaveOutline" />
            </template>
          </n-button>
          <n-button size="tiny" quaternary type="error" :disabled="busyIdx === i" :title="t('fieldMap.remove')"
            data-testid="fieldMap.remove" @click="removeRow(i)">
            <template #icon>
              <n-icon :component="TrashOutline" />
            </template>
          </n-button>
        </div>
      </div>

      <p class="sync-hint">{{ t('fieldMap.syncHint') }}</p>
      <p v-if="error" class="err" data-testid="fieldMap.error">{{ error }}</p>
    </div>

    <template #footer>
      <div class="foot">
        <n-button size="small" secondary data-testid="fieldMap.add" @click="addRow">
          {{ t('fieldMap.add') }}
        </n-button>
        <n-button size="small" data-testid="fieldMap.close" @click="emit('update:show', false)">
          {{ t('common.close') }}
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
  max-height: 60vh;
  overflow-y: auto;
}

.hint,
.sync-hint {
  margin: 0;
  font-size: 12px;
  color: var(--app-text-dim);
  line-height: 1.6;
}

.head,
.row {
  display: grid;
  grid-template-columns: minmax(0, 1.2fr) 120px minmax(0, 1.6fr) 64px;
  gap: 8px;
  align-items: center;
}

.head {
  font-size: 12px;
  color: var(--app-text-dim);
  padding-bottom: 4px;
  border-bottom: 1px solid var(--app-border);
}

.ops {
  display: flex;
  justify-content: flex-end;
  gap: 4px;
}

.empty {
  padding: 18px 0;
  text-align: center;
  font-size: 12.5px;
  color: var(--app-text-dim);
}

.err {
  margin: 0;
  font-size: 12px;
  color: var(--app-danger);
}

.foot {
  display: flex;
  justify-content: space-between;
  gap: 8px;
}
</style>

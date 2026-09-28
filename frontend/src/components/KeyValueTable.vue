<script setup lang="ts">
// 键值表（参数 / 请求头 / 表单体通用）：启用勾选 + 名称 + 值 + 删除。
import { NButton, NCheckbox, NInput } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import type { KV } from '@/types'

const props = defineProps<{ rows: KV[] }>()
const emit = defineEmits<{ change: [] }>()
const { t } = useI18n()

function add(): void {
  props.rows.push({ name: '', value: '', enabled: true })
  emit('change')
}

function del(i: number): void {
  props.rows.splice(i, 1)
  emit('change')
}
</script>

<template>
  <div class="kvt">
    <div v-for="(row, i) in rows" :key="i" class="row">
      <n-checkbox
        v-model:checked="row.enabled"
        size="small"
        @update:checked="emit('change')"
      />
      <n-input
        v-model:value="row.name"
        size="small"
        placeholder="name"
        class="kn"
        @input="emit('change')"
      />
      <n-input
        v-model:value="row.value"
        size="small"
        placeholder="value"
        class="kv"
        @input="emit('change')"
      />
      <n-button text size="tiny" class="del" @click="del(i)">✕</n-button>
    </div>
    <n-button text size="tiny" type="primary" @click="add">{{ t('editor.addRow') }}</n-button>
  </div>
</template>

<style scoped>
.kvt {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.kn {
  width: 200px;
  flex: 0 0 auto;
}

.kv {
  flex: 1 1 auto;
}

.del {
  color: var(--app-muted);
}

.del:hover {
  color: #d03050;
}
</style>

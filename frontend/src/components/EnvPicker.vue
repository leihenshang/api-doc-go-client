<script setup lang="ts">
// 环境选择器：显示当前环境 + 打开环境管理。
import { NButton, NSelect } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import type { Env } from '@/types'

const props = defineProps<{ envs: Env[]; modelValue: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string]; manage: [] }>()
const { t } = useI18n()

const options = props.envs.map((e) => ({ label: e.name, value: e.name }))
</script>

<template>
  <span class="ep">
    <n-select
      :value="modelValue"
      :options="options"
      size="small"
      class="sel"
      :placeholder="t('env.title')"
      @update:value="emit('update:modelValue', $event)"
    />
    <n-button size="tiny" quaternary :title="t('env.title')" @click="emit('manage')">⚙</n-button>
  </span>
</template>

<style scoped>
.ep {
  display: inline-flex;
  align-items: center;
  gap: 2px;
}

.sel {
  width: 150px;
}
</style>

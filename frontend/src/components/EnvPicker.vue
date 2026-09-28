<script setup lang="ts">
// 环境选择器：最后一项固定为「环境设置…」，选中即打开环境管理（不再单列齿轮按钮）。
import { NSelect } from 'naive-ui'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Env } from '@/types'

const MANAGE = '__manage__'

const props = defineProps<{ envs: Env[]; modelValue: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string]; manage: [] }>()
const { t } = useI18n()

const options = computed(() => [
  ...props.envs.map((e) => ({ label: e.name, value: e.name })),
  { label: t('env.manage'), value: MANAGE },
])

function pick(v: string): void {
  if (v === MANAGE) {
    emit('manage')
    return
  }
  emit('update:modelValue', v)
}
</script>

<template>
  <n-select
    :value="modelValue"
    :options="options"
    size="small"
    class="sel"
    :placeholder="t('env.none')"
    @update:value="pick"
  />
</template>

<style scoped>
.sel {
  width: 158px;
}
</style>

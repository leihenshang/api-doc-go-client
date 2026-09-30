<script setup lang="ts">
// 环境选择器：最后一项固定为「环境设置…」，选中即打开环境管理。
// 图标只在选择框箭头处显示，下拉选项不带图标。
import { NIcon, NSelect } from 'naive-ui'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { GlobeOutline } from '@vicons/ionicons5'
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
  >
    <template #arrow>
      <n-icon :component="GlobeOutline" :size="14" class="arrow-ic" />
    </template>
  </n-select>
</template>

<style scoped>
.sel {
  width: 158px;
}
.arrow-ic {
  color: var(--app-muted);
}
</style>

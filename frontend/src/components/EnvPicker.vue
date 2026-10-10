<script setup lang="ts">
// 环境选择器：外观与「集合切换」一致（左侧地球图标 + 环境名 + 右侧下拉箭头），
// 下拉最后一项固定为「环境设置…」，选中即打开环境管理。
import type { Env } from '@/types'
import { CheckmarkOutline, ChevronDownOutline, GlobeOutline, SettingsOutline } from '@vicons/ionicons5'
import type { DropdownOption } from 'naive-ui'
import { NDropdown, NIcon } from 'naive-ui'
import { computed, h } from 'vue'
import { useI18n } from 'vue-i18n'

const MANAGE = '__manage__'

const props = defineProps<{ envs: Env[]; modelValue: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string]; manage: [] }>()
const { t } = useI18n()

/** 当前环境打勾；末尾固定「环境设置…」。 */
const options = computed<DropdownOption[]>(() => [
  ...props.envs.map((e) => ({
    key: e.name,
    label: e.name,
    icon: e.name === props.modelValue ? () => h(NIcon, { component: CheckmarkOutline }) : undefined,
  })),
  { key: MANAGE, label: t('env.manage'), icon: () => h(NIcon, { component: SettingsOutline }) },
])

const current = computed(() => props.modelValue || t('env.none'))

function pick(key: string | number): void {
  if (key === MANAGE) {
    emit('manage')
    return
  }
  emit('update:modelValue', String(key))
}
</script>

<template>
  <n-dropdown trigger="click" :options="options" @select="pick">
    <button class="env" type="button" data-testid="env.select" :title="t('env.current')">
      <n-icon :component="GlobeOutline" :size="15" class="ic" />
      <span class="nm">{{ current }}</span>
      <n-icon :component="ChevronDownOutline" :size="12" class="ar" />
    </button>
  </n-dropdown>
</template>

<style scoped>
/* 与集合切换按钮同款：同高、同边框、同底色，两个切换器并排观感一致 */
.env {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 28px;
  padding: 0 8px;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: var(--app-panel);
  color: var(--app-text);
  font-size: 13px;
  font-family: inherit;
  cursor: pointer;
}

.env:hover {
  background: var(--app-row-hover);
}

.ic {
  color: var(--app-muted);
}

.ar {
  color: var(--app-placeholder);
}

.nm {
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
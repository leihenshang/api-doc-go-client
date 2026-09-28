<script setup lang="ts">
// 底部状态栏：左侧本地集合概况，右侧离线优先声明（design-spec §2）。
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ requests: number; envs: number }>()
const { t } = useI18n()

// 与 frontend/package.json 的 version 保持一致（此处不引 JSON 模块，避免额外构建配置）
const VERSION = 'v0.0.1'

const left = computed(() =>
  [
    t('local.collection'),
    t('status.requests', { n: props.requests }),
    t('status.envs', { n: props.envs }),
    t('local.plain'),
  ].join(t('common.sep')),
)

const right = computed(() =>
  [t('local.privacy'), t('local.privacyHint'), VERSION].join(t('common.sep')),
)
</script>

<template>
  <footer class="statusbar">
    <span class="txt">{{ left }}</span>
    <span class="sp" />
    <span class="txt">{{ right }}</span>
  </footer>
</template>

<style scoped>
.statusbar {
  height: var(--app-statusbar-h);
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 12px;
  background: var(--app-bg);
  border-top: 1px solid var(--app-border);
  font-family: var(--app-mono);
  font-size: 11px;
  color: var(--app-muted);
  user-select: none;
}

.sp {
  flex: 1 1 auto;
}

.txt {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>

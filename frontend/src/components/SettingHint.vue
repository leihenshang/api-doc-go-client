<script setup lang="ts">
// 设置项的说明气泡：图标跟在**左侧标签**后面，悬停显示说明文字。
//
// 为什么单独抽组件：设置页有 6 处说明，且要按重要程度分级（普通 / 注意 / 风险 / 推荐）。
// 直接在每个 NTooltip 里重复图标、配色与宽度，改一处要改六处。
//
// tone 决定图标颜色与气泡内左侧色条：
//   info（默认，灰）：纯说明
//   warn（黄）：容易踩坑、改动后行为与预期不同
//   danger（红）：有安全 / 数据风险
//   ok（绿）：安心提示（说明某个行为不会造成损失）
import { AlertCircleOutline, CheckmarkCircleOutline, InformationCircleOutline, WarningOutline } from '@vicons/ionicons5'
import { NIcon, NTooltip } from 'naive-ui'
import { computed } from 'vue'

type HintTone = 'info' | 'warn' | 'danger' | 'ok'

/** maxWidth：气泡最大宽度（px）。设置页默认 320；请求编辑器里的窄面板传小一点，免得盖住正文。 */
const props = withDefaults(defineProps<{ text: string; tone?: HintTone; maxWidth?: number }>(), {
  tone: 'info',
  maxWidth: 320,
})

const ICONS = {
  info: InformationCircleOutline,
  warn: AlertCircleOutline,
  danger: WarningOutline,
  ok: CheckmarkCircleOutline,
} as const

const icon = computed(() => ICONS[props.tone])
</script>

<template>
  <n-tooltip placement="top-start" :style="{ maxWidth: `${maxWidth}px` }">
    <template #trigger>
      <n-icon class="hint-icon" :class="`tone-${tone}`" :component="icon" data-testid="hint.icon" />
    </template>
    <!-- 气泡底色统一深色（对比度最好），级别只用左侧色条表达，避免彩色文字在深底上读不清 -->
    <span class="hint-body" :class="`tone-${tone}`">{{ text }}</span>
  </n-tooltip>
</template>

<style scoped>
.hint-icon {
  margin-left: 6px;
  font-size: 13px;
  vertical-align: middle;
  cursor: help;
}

.hint-icon.tone-info {
  color: var(--app-muted);
}

.hint-icon.tone-warn {
  color: var(--app-warn);
}

.hint-icon.tone-danger {
  color: var(--app-danger);
}

.hint-icon.tone-ok {
  color: var(--app-ok);
}

.hint-body {
  display: block;
  padding-left: 8px;
  border-left: 3px solid var(--app-muted);
}

.hint-body.tone-warn {
  border-left-color: var(--app-warn);
}

.hint-body.tone-danger {
  border-left-color: var(--app-danger);
}

.hint-body.tone-ok {
  border-left-color: var(--app-ok);
}
</style>

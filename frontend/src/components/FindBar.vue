<script setup lang="ts">
// 页内查找条（响应只读区 / 请求编辑区统一外观）：关键字 + 命中计数 + 上一个/下一个 + 关闭。
// Enter=下一个、Shift+Enter=上一个、Esc=关闭；具体的命中/滚动由调用方驱动（见 lib/find）。
import { ChevronDownOutline, ChevronUpOutline, CloseOutline } from '@vicons/ionicons5'
import { NIcon } from 'naive-ui'
import { nextTick, ref } from 'vue'
import { useI18n } from 'vue-i18n'

defineProps<{ total: number; index: number }>()
const keyword = defineModel<string>('keyword', { required: true })
const emit = defineEmits<{ next: []; prev: []; close: [] }>()
const { t } = useI18n()

const inputEl = ref<HTMLInputElement | null>(null)

function onKey(e: KeyboardEvent): void {
  if (e.key === 'Enter') {
    e.preventDefault()
    if (e.shiftKey) emit('prev')
    else emit('next')
  } else if (e.key === 'Escape') {
    e.preventDefault()
    emit('close')
  }
}

/** 打开后聚焦输入框（调用方 nextTick 后调 open）。 */
function focus(): void {
  void nextTick().then(() => inputEl.value?.focus())
}
defineExpose({ focus })
</script>

<template>
  <div class="findbar" data-testid="find.bar" @keydown="onKey">
    <input
      ref="inputEl"
      :value="keyword"
      type="search"
      class="kw"
      data-testid="find.input"
      :placeholder="t('find.placeholder')"
      @input="keyword = ($event.target as HTMLInputElement).value"
    />
    <span class="cnt" data-testid="find.count">{{ total ? `${index + 1}/${total}` : t('find.none') }}</span>
    <button class="nav" type="button" data-testid="find.prev" :title="t('find.prev')" @click="emit('prev')">
      <n-icon :component="ChevronUpOutline" :size="14" />
    </button>
    <button class="nav" type="button" data-testid="find.next" :title="t('find.next')" @click="emit('next')">
      <n-icon :component="ChevronDownOutline" :size="14" />
    </button>
    <button class="x" type="button" data-testid="find.close" :title="t('find.close')" @click="emit('close')">
      <n-icon :component="CloseOutline" :size="14" />
    </button>
  </div>
</template>

<style scoped>
.findbar {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 0 0 auto;
  padding: 4px 6px;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: var(--app-panel);
  margin: 6px 0;
}

.kw {
  flex: 1 1 auto;
  min-width: 80px;
  border: 1px solid var(--app-border);
  border-radius: 4px;
  background: var(--app-surface-2, transparent);
  color: var(--app-text);
  font-size: 12px;
  font-family: inherit;
  padding: 3px 8px;
  outline: none;
}

.kw:focus {
  border-color: var(--app-accent);
}

.cnt {
  flex: 0 0 auto;
  font-size: 11.5px;
  color: var(--app-muted);
  min-width: 42px;
  text-align: center;
  font-variant-numeric: tabular-nums;
}

.nav,
.x {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border: none;
  border-radius: 4px;
  background: none;
  color: var(--app-text-2);
  cursor: pointer;
  padding: 0;
}

.nav:hover,
.x:hover {
  background: var(--app-row-hover);
  color: var(--app-accent);
}
</style>

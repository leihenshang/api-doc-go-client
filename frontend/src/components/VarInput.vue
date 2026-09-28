<script setup lang="ts">
// 变量高亮输入框（design-spec §6.7）：文字透明的原生 input 叠一层高亮文本，
// 使 {{变量}} 显示为主色底纹；input 保留真实光标与选区。
import { computed, ref } from 'vue'

const props = defineProps<{ modelValue: string; placeholder?: string }>()
const emit = defineEmits<{ 'update:modelValue': [v: string] }>()

interface Part {
  text: string
  cls: string
}

const hl = ref<HTMLElement | null>(null)
const el = ref<HTMLInputElement | null>(null)

const parts = computed<Part[]>(() => {
  const out: Part[] = []
  const re = /\{\{[^{}]*\}\}/g
  let last = 0
  let m: RegExpExecArray | null
  while ((m = re.exec(props.modelValue)) !== null) {
    if (m.index > last) out.push({ text: props.modelValue.slice(last, m.index), cls: '' })
    out.push({ text: m[0], cls: 'var' })
    last = m.index + m[0].length
  }
  if (last < props.modelValue.length) out.push({ text: props.modelValue.slice(last), cls: '' })
  return out
})

// 高亮层不随 input 滚动，需手动同步横向偏移
function sync(): void {
  if (hl.value && el.value) hl.value.scrollLeft = el.value.scrollLeft
}

function onInput(e: Event): void {
  emit('update:modelValue', (e.target as HTMLInputElement).value)
  sync()
}

function focus(): void {
  el.value?.focus()
}
</script>

<template>
  <div class="vi" @click="focus">
    <div ref="hl" class="hl mono" aria-hidden="true">
      <span v-for="(p, i) in parts" :key="i" :class="p.cls">{{ p.text }}</span>
      <span v-if="!modelValue" class="ph">{{ placeholder }}</span>
    </div>
    <input
      ref="el"
      class="in mono"
      :value="modelValue"
      spellcheck="false"
      autocomplete="off"
      @input="onInput"
      @scroll="sync"
    />
  </div>
</template>

<style scoped>
.vi {
  position: relative;
  flex: 1 1 auto;
  /* 高度由使用方通过 --vi-h 指定，保证高亮层与 input 完全重叠 */
  height: var(--vi-h, 28px);
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: var(--app-panel);
  overflow: hidden;
}

.vi:focus-within {
  border-color: var(--app-accent);
}

.hl,
.in {
  position: absolute;
  inset: 0;
  padding: 0 10px;
  font-size: 13px;
  line-height: calc(var(--vi-h, 28px) - 2px);
  white-space: pre;
  overflow: hidden;
}

.hl {
  color: var(--app-text);
  pointer-events: none;
}

.hl .var {
  color: var(--app-accent-dark);
  background: var(--app-accent-tint);
  border-radius: 3px;
}

.ph {
  color: var(--app-placeholder);
}

.in {
  border: none;
  outline: none;
  background: transparent;
  color: transparent;
  caret-color: var(--app-text);
  font-family: inherit;
  width: 100%;
}
</style>

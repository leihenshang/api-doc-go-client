<script setup lang="ts">
// 请求栏（design-spec §2 地址栏）：方法选择器（语义色）+ URL（{{变量}} 高亮）+ 格式化 + Send。
import { NIcon, NSelect, NTag } from 'naive-ui'
import type { SelectOption } from 'naive-ui'
import { computed, h, type VNode } from 'vue'
import { useI18n } from 'vue-i18n'
import { OptionsOutline, SendOutline, SyncOutline } from '@vicons/ionicons5'
import VarInput from '@/components/VarInput.vue'
import { methodColor, methodTint } from '@/lib/method'
import { useTabsStore } from '@/stores/tabs'
import type { Tab } from '@/stores/tabs'

const props = defineProps<{ tab: Tab }>()
const tabs = useTabsStore()
const { t } = useI18n()

const methods = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS'].map((m) => ({ label: m, value: m }))

const controlStyle = computed(
  () => `--vi-h:34px;--m-color:${methodColor(props.tab.request.method)};--m-tint:${methodTint(props.tab.request.method)}`,
)

// 仅当 URL 含变量（或变量缺失）时才占一行展示解析预览，保持地址栏与设计稿一致的单行观感
const showResolved = computed(() => props.tab.request.url.includes('{{') || !!props.tab.resolve?.missing.length)

// 下拉与已选值均按方法着色
function renderMethod(option: SelectOption): VNode {
  const v = String(option.value ?? '')
  return h(
    'span',
    { style: `color:${methodColor(v)};font-weight:600;font-family:var(--app-mono)` },
    String(option.label ?? v),
  )
}

function touch(): void {
  tabs.touch(props.tab.key)
}

function send(): void {
  void tabs.send(props.tab.key)
}

// 规范化：去首尾空白，并把 {{ var }} 收成 {{var}}（不改变 URL 语义）
function formatUrl(): void {
  props.tab.request.url = props.tab.request.url.trim().replace(/\{\{\s*([^{}]+?)\s*\}\}/g, '{{$1}}')
  touch()
}
</script>

<template>
  <div class="bar-wrap">
    <div class="req-line" :style="controlStyle">
      <n-select
        v-model:value="tab.request.method"
        :options="methods"
        :render-label="renderMethod"
        class="method"
        @update:value="touch"
      />
      <var-input v-model="tab.request.url" :placeholder="t('editor.urlPlaceholder')" @update:model-value="touch" />
      <button class="icon-btn" type="button" :title="t('editor.formatUrl')" @click="formatUrl">
        <n-icon :component="OptionsOutline" :size="16" />
      </button>
      <button class="send" type="button" :disabled="tab.sending" @click="send">
        <n-icon :component="tab.sending ? SyncOutline : SendOutline" :size="15" :class="{ spin: tab.sending }" />
        <span>{{ tab.sending ? t('editor.sending') : t('editor.send') }}</span>
      </button>
    </div>

    <div v-if="showResolved" class="resolved mono">
      <span class="lbl">{{ t('editor.resolvedUrl') }}</span>
      <span class="val" :class="{ miss: tab.resolve?.missing.length }">{{ tab.resolve?.text ?? '' }}</span>
      <n-tag v-if="tab.resolve?.missing.length" type="warning" size="small" :bordered="false">
        {{ t('editor.missingVars') }}: {{ tab.resolve.missing.join(', ') }}
      </n-tag>
    </div>
  </div>
</template>

<style scoped>
.bar-wrap {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--app-border);
  background: var(--app-panel);
}

.req-line {
  display: flex;
  align-items: center;
  gap: 8px;
}

.method {
  width: 118px;
  flex: 0 0 auto;
}

.method :deep(.n-base-selection) {
  background: var(--m-tint);
  border-radius: 6px;
}

.method :deep(.n-base-selection .n-base-selection-label) {
  color: var(--m-color);
  font-family: var(--app-mono);
  font-weight: 600;
}

/* 让方法选择器的输入区铺满整个高度，避免文字偏上 */
.method :deep(.n-base-selection-label) {
  height: 100%;
}

.icon-btn {
  flex: 0 0 auto;
  width: 34px;
  height: 34px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: var(--app-panel);
  color: var(--app-muted);
  cursor: pointer;
}

.icon-btn:hover {
  border-color: var(--app-accent);
  color: var(--app-accent);
}

.send {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 34px;
  border: none;
  background: var(--app-send);
  color: #fff;
  font-family: inherit;
  font-weight: 600;
  font-size: 13px;
  padding: 0 18px;
  border-radius: 6px;
  cursor: pointer;
}

.send:hover:not(:disabled) {
  background: var(--app-send-hover);
}

.send:disabled {
  opacity: 0.75;
  cursor: default;
}

.spin {
  animation: rot 0.9s linear infinite;
}

@keyframes rot {
  to {
    transform: rotate(360deg);
  }
}

.resolved {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 11.5px;
  overflow: hidden;
}

.resolved .lbl {
  color: var(--app-muted);
  flex: 0 0 auto;
}

.resolved .val {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.resolved .val.miss {
  color: #d08830;
}
</style>

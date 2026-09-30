<script setup lang="ts">
// 请求栏（design-spec §2 地址栏）：方法选择器（语义色）+ URL（{{变量}} 高亮）+ 格式化 + Send。
import { NIcon, NSelect } from 'naive-ui'
import type { SelectOption } from 'naive-ui'
import { computed, h, type VNode } from 'vue'
import { useI18n } from 'vue-i18n'
import { OptionsOutline, CodeOutline, SendOutline, SyncOutline } from '@vicons/ionicons5'
import VarInput from '@/components/VarInput.vue'
import { methodColor, methodTint } from '@/lib/method'
import { useCollectionStore } from '@/stores/collection'
import { useTabsStore } from '@/stores/tabs'
import type { Tab } from '@/stores/tabs'

const props = defineProps<{ tab: Tab }>()
const emit = defineEmits<{ codegen: [] }>()
const tabs = useTabsStore()
const coll = useCollectionStore()
const { t } = useI18n()

const methods = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS'].map((m) => ({ label: m, value: m }))

const controlStyle = computed(
  () => `--vi-h:34px;--m-color:${methodColor(props.tab.request.method)};--m-tint:${methodTint(props.tab.request.method)}`,
)

// 地址栏不再单独占一行展示「替换后」文本：值只在悬停变量时给出（见 VarInput）。
// 敏感变量在提示里只显示掩码，取当前环境的 secret 标记。
const secretNames = computed(() => {
  const env = coll.info?.envs.find((e) => e.name === coll.currentEnv)
  return env?.vars.filter((v) => v.secret).map((v) => v.name) ?? []
})

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

function cancel(): void {
  tabs.cancelSend(props.tab.key)
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
        data-testid="req.method"
        @update:value="touch"
      />
      <var-input
        v-model="tab.request.url"
        data-testid="req.url"
        :placeholder="t('editor.urlPlaceholder')"
        :vars="tab.resolve?.values ?? {}"
        :missing="tab.resolve?.missing ?? []"
        :secrets="secretNames"
        @update:model-value="touch"
      />
      <button class="icon-btn" type="button" data-testid="req.format" :title="t('editor.formatUrl')" @click="formatUrl">
        <n-icon :component="OptionsOutline" :size="16" />
      </button>
      <button class="icon-btn" type="button" data-testid="req.codegen" :title="t('codegen.title')" @click="emit('codegen')">
        <n-icon :component="CodeOutline" :size="16" />
      </button>
      <button
        class="send"
        type="button"
        data-testid="req.send"
        :disabled="tab.sending || coll.isReadOnly"
        :title="coll.isReadOnly ? t('sync.mirrorReadonly') : ''"
        @click="send"
      >
        <n-icon :component="tab.sending ? SyncOutline : SendOutline" :size="15" :class="{ spin: tab.sending }" />
        <span>{{ tab.sending ? t('editor.sending') : t('editor.send') }}</span>
      </button>
      <button
        v-if="tab.sending"
        class="cancel"
        type="button"
        data-testid="req.cancel"
        :title="t('editor.cancelSend')"
        @click="cancel"
      >
        {{ t('editor.cancelSend') }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.bar-wrap {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--app-border);
  background: var(--app-panel);
}

.req-line {
  display: flex;
  align-items: center;
  gap: 6px;
}

.method {
  width: 110px;
  flex: 0 0 auto;
}

/* 方法选择器整块统一底色，避免 arrow 区块与 label 区块色差分半 */
.method :deep(.n-base-selection) {
  background: var(--m-tint) !important;
  border-radius: 6px;
  --n-color: var(--m-tint) !important;
}

.method :deep(.n-base-selection .n-base-selection-label) {
  color: var(--m-color);
  font-family: var(--app-mono);
  font-weight: 600;
}

.method :deep(.n-base-selection-suffix),
.method :deep(.n-base-selection-arrow) {
  background: transparent;
  color: var(--m-color);
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
  color: var(--app-on-accent);
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

.cancel {
  flex: 0 0 auto;
  height: 34px;
  border: 1px solid var(--app-border);
  background: var(--app-panel);
  color: var(--app-danger, #d03050);
  font-family: inherit;
  font-weight: 600;
  font-size: 13px;
  padding:0 14px;
  border-radius: 6px;
  cursor: pointer;
}

.cancel:hover {
  border-color: var(--app-danger, #d03050);
}

.spin {
  animation: rot 0.9s linear infinite;
}

@keyframes rot {
  to {
    transform: rotate(360deg);
  }
}

</style>

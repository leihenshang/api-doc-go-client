<script setup lang="ts">
// 请求编辑区：默认直接可编辑（无查看/编辑两态）；URL 支持 {{变量}} 并实时显示替换预览。
import { NInput, NSelect, NTag } from 'naive-ui'
import type { SelectOption } from 'naive-ui'
import { computed, h, ref, watch, type VNode } from 'vue'
import { useI18n } from 'vue-i18n'
import KeyValueTable from '@/components/KeyValueTable.vue'
import { useTabsStore } from '@/stores/tabs'
import type { Tab } from '@/stores/tabs'
import type { Auth } from '@/types'

const props = defineProps<{ tab: Tab }>()
const tabs = useTabsStore()
const { t } = useI18n()

const methods = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS'].map((m) => ({ label: m, value: m }))
const seg = ref<'params' | 'headers' | 'auth' | 'body' | 'docs'>('params')

const auth = computed<Auth>(() => props.tab.request.auth ?? { type: 'none' })
const authType = computed({
  get: () => auth.value.type || 'none',
  set: (v: string) => {
    props.tab.request.auth = { ...auth.value, type: v }
    touch()
  },
})
const authOptions = computed(() => [
  { label: t('auth.none'), value: 'none' },
  { label: t('auth.basic'), value: 'basic' },
  { label: t('auth.bearer'), value: 'bearer' },
  { label: t('auth.apikey'), value: 'apikey' },
])
const authInOptions = computed(() => [
  { label: t('auth.header'), value: 'header' },
  { label: t('auth.query'), value: 'query' },
])
const hasAuth = computed(() => auth.value.type !== '' && auth.value.type !== 'none')

const methodColor: Record<string, string> = {
  GET: '#18a058',
  POST: '#4098fc',
  PUT: '#f0a020',
  DELETE: '#d03050',
  PATCH: '#8a2be2',
  HEAD: '#8a9199',
  OPTIONS: '#8a9199',
}

// 下拉与已选值均按方法着色。
function renderMethod(option: SelectOption): VNode {
  const v = String(option.value ?? '')
  const c = methodColor[v.toUpperCase()] ?? '#909399'
  return h('span', { style: `color:${c};font-weight:600` }, String(option.label ?? v))
}

const bodyType = computed({
  get: () => props.tab.request.body.type,
  set: (v: string) => {
    props.tab.request.body.type = v as typeof props.tab.request.body.type
    tabs.touch(props.tab.key)
  },
})

// 页签角标：该段有内容时显示小圆点（仿 Bruno）。
const hasParams = computed(() => props.tab.request.params.some((p) => p.name.trim()))
const hasHeaders = computed(() => props.tab.request.headers.some((h) => h.name.trim()))
const hasBody = computed(
  () => props.tab.request.body.type !== 'none' &&
    (props.tab.request.body.raw.trim() !== '' || props.tab.request.body.form.some((f) => f.name.trim())),
)
const hasDocs = computed(() => props.tab.request.docs.trim() !== '')

function touch(): void {
  tabs.touch(props.tab.key)
}

function send(): void {
  void tabs.send(props.tab.key)
}

// 切换 tab 时刷新解析预览
watch(
  () => props.tab.key,
  () => tabs.refreshResolve(),
)
</script>

<template>
  <div class="editor">
    <div class="req-line">
      <n-select
        v-model:value="tab.request.method"
        :options="methods"
        :render-label="renderMethod"
        size="small"
        class="method"
        @update:value="touch"
      />
      <n-input
        v-model:value="tab.request.url"
        size="small"
        class="url mono"
        :placeholder="t('editor.urlPlaceholder')"
        @input="touch"
      />
      <button class="send" :disabled="tab.sending" @click="send">
        <span v-if="tab.sending" class="spin">◌</span>
        {{ tab.sending ? t('editor.sending') : t('editor.send') }}
      </button>
    </div>

    <!-- 变量替换预览：未定义变量黄色告警 -->
    <div v-if="tab.resolve" class="resolved mono">
      <span class="lbl">{{ t('editor.resolvedUrl') }}</span>
      <span class="val" :class="{ miss: tab.resolve.missing.length }">{{ tab.resolve.text }}</span>
      <n-tag v-if="tab.resolve.missing.length" type="warning" size="small" :bordered="false">
        {{ t('editor.missingVars') }}: {{ tab.resolve.missing.join(', ') }}
      </n-tag>
    </div>

    <div class="name-line">
      <span class="lbl">{{ t('editor.name') }}</span>
      <n-input v-model:value="tab.request.name" size="small" class="name" @input="touch" />
    </div>

    <div class="seg">
      <button class="seg-tab" :class="{ on: seg === 'params' }" @click="seg = 'params'">
        {{ t('editor.params') }}<span v-if="hasParams" class="badge" />
      </button>
      <button class="seg-tab" :class="{ on: seg === 'headers' }" @click="seg = 'headers'">
        {{ t('editor.headers') }}<span v-if="hasHeaders" class="badge" />
      </button>
      <button class="seg-tab" :class="{ on: seg === 'auth' }" @click="seg = 'auth'">
        {{ t('auth.title') }}<span v-if="hasAuth" class="badge" />
      </button>
      <button class="seg-tab" :class="{ on: seg === 'body' }" @click="seg = 'body'">
        {{ t('editor.body') }}<span v-if="hasBody" class="badge" />
      </button>
      <button class="seg-tab" :class="{ on: seg === 'docs' }" @click="seg = 'docs'">
        {{ t('editor.docs') }}<span v-if="hasDocs" class="badge" />
      </button>
    </div>

    <div class="seg-body">
      <key-value-table v-if="seg === 'params'" :rows="tab.request.params" @change="touch" />
      <key-value-table v-else-if="seg === 'headers'" :rows="tab.request.headers" @change="touch" />
      <div v-else-if="seg === 'auth'" class="auth-pane">
        <div class="arow">
          <span class="lbl">{{ t('auth.type') }}</span>
          <n-select v-model:value="authType" :options="authOptions" size="small" class="atype" />
        </div>
        <template v-if="authType === 'basic'">
          <div class="arow">
            <span class="lbl">{{ t('auth.username') }}</span>
            <n-input v-model:value="auth.username" size="small" @input="touch" />
          </div>
          <div class="arow">
            <span class="lbl">{{ t('auth.password') }}</span>
            <n-input
              v-model:value="auth.password"
              size="small"
              type="password"
              show-password-on="click"
              @input="touch"
            />
          </div>
        </template>
        <template v-else-if="authType === 'bearer'">
          <div class="arow">
            <span class="lbl">{{ t('auth.token') }}</span>
            <n-input v-model:value="auth.token" size="small" @input="touch" />
          </div>
        </template>
        <template v-else-if="authType === 'apikey'">
          <div class="arow">
            <span class="lbl">{{ t('auth.key') }}</span>
            <n-input v-model:value="auth.key" size="small" @input="touch" />
          </div>
          <div class="arow">
            <span class="lbl">{{ t('auth.value') }}</span>
            <n-input v-model:value="auth.value" size="small" @input="touch" />
          </div>
          <div class="arow">
            <span class="lbl">{{ t('auth.in') }}</span>
            <n-select v-model:value="auth.in" :options="authInOptions" size="small" class="atype" @update:value="touch" />
          </div>
        </template>
      </div>
      <template v-else-if="seg === 'body'">
        <div class="body-pane">
          <n-select
            :value="bodyType"
            :options="[
              { label: t('editor.bodyNone'), value: 'none' },
              { label: t('editor.bodyJson'), value: 'json' },
              { label: t('editor.bodyText'), value: 'text' },
              { label: t('editor.bodyForm'), value: 'form' },
              { label: t('editor.bodyMultipart'), value: 'multipart' },
            ]"
            size="small"
            class="btype"
            @update:value="bodyType = $event"
          />
          <n-input
            v-if="bodyType === 'json' || bodyType === 'text'"
            v-model:value="tab.request.body.raw"
            type="textarea"
            :rows="10"
            class="mono raw"
            :placeholder="t('editor.rawPlaceholder')"
            @input="touch"
          />
          <key-value-table
            v-else-if="bodyType === 'form' || bodyType === 'multipart'"
            :rows="tab.request.body.form"
            @change="touch"
          />
        </div>
      </template>
      <n-input
        v-else
        v-model:value="tab.request.docs"
        type="textarea"
        :rows="10"
        class="docs"
        :placeholder="t('editor.docsPlaceholder')"
        @input="touch"
      />
    </div>
  </div>
</template>

<style scoped>
.editor {
  padding: 12px 14px 0;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.req-line {
  display: flex;
  gap: 8px;
}

.method {
  width: 112px;
  flex: 0 0 auto;
}

.url {
  flex: 1 1 auto;
}

.send {
  flex: 0 0 auto;
  border: none;
  background: var(--app-send);
  color: #fff;
  font-weight: 600;
  font-size: 13px;
  padding: 0 18px;
  border-radius: 5px;
  cursor: pointer;
}

.send:hover:not(:disabled) {
  background: var(--app-send-hover);
}

.send:disabled {
  opacity: 0.7;
  cursor: default;
}

.spin {
  display: inline-block;
  animation: rot 0.8s linear infinite;
}

@keyframes rot {
  to {
    transform: rotate(360deg);
  }
}

.resolved {
  margin-top: 8px;
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
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

.name-line {
  margin-top: 10px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.lbl {
  color: var(--app-muted);
  font-size: 12px;
  flex: 0 0 auto;
}

.name {
  max-width: 320px;
}

.seg {
  margin-top: 12px;
  display: flex;
  gap: 2px;
  border-bottom: 1px solid var(--app-border);
}

.seg-tab {
  position: relative;
  border: none;
  background: none;
  padding: 8px 12px;
  font-size: 12.5px;
  color: #666;
  cursor: pointer;
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
}

.seg-tab:hover {
  color: var(--app-text);
}

.seg-tab.on {
  color: var(--app-text);
  font-weight: 600;
  border-bottom-color: var(--app-accent);
}

.badge {
  position: absolute;
  top: 6px;
  right: 4px;
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: var(--app-accent);
}

.seg-body {
  padding: 12px 0;
  flex: 1 1 auto;
  overflow: auto;
}

.body-pane {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.auth-pane {
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-width: 560px;
}

.arow {
  display: flex;
  align-items: center;
  gap: 10px;
}

.arow .lbl {
  width: 90px;
}

.atype {
  width: 160px;
}

.btype {
  width: 130px;
}

.raw,
.docs {
  font-size: 12.5px;
}
</style>

<script setup lang="ts">
// 请求设置区（design-spec §2 请求面板）：Params / Body / Headers / Auth / Docs。
// 文档编辑器与服务端 Web 保持一致（md-editor-v3 的 MdEditor）。
import { NInput, NSelect } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MdEditor } from 'md-editor-v3'
import 'md-editor-v3/lib/preview.css'
import 'md-editor-v3/lib/style.css'
import KeyValueTable from '@/components/KeyValueTable.vue'
import { useTabsStore } from '@/stores/tabs'
import type { Tab } from '@/stores/tabs'
import type { Auth } from '@/types'

const props = defineProps<{ tab: Tab }>()
const tabs = useTabsStore()
const { t, locale } = useI18n()

type Seg = 'params' | 'body' | 'headers' | 'auth' | 'docs'

const seg = ref<Seg>('params')

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

const bodyType = computed({
  get: () => props.tab.request.body.type,
  set: (v: string) => {
    props.tab.request.body.type = v as typeof props.tab.request.body.type
    touch()
  },
})

// 文档：写回草稿即标脏（自动保存）
const docs = computed({
  get: () => props.tab.request.docs,
  set: (v: string) => {
    props.tab.request.docs = v
    touch()
  },
})

const mdLanguage = computed(() => (locale.value === 'en-US' ? 'en-US' : 'zh-CN'))

// 页签角标：该段有内容时显示小圆点（仿 Bruno）
const hasParams = computed(() => props.tab.request.params.some((p) => p.name.trim()))
const hasHeaders = computed(() => props.tab.request.headers.some((h) => h.name.trim()))
const hasBody = computed(
  () =>
    props.tab.request.body.type !== 'none' &&
    (props.tab.request.body.raw.trim() !== '' || props.tab.request.body.form.some((f) => f.name.trim())),
)
const hasDocs = computed(() => props.tab.request.docs.trim() !== '')

const segments = computed<{ key: Seg; label: string; dot: boolean }[]>(() => [
  { key: 'params', label: t('editor.params'), dot: hasParams.value },
  { key: 'body', label: t('editor.body'), dot: hasBody.value },
  { key: 'headers', label: t('editor.headers'), dot: hasHeaders.value },
  { key: 'auth', label: t('editor.auth'), dot: hasAuth.value },
  { key: 'docs', label: t('editor.docs'), dot: hasDocs.value },
])

function touch(): void {
  tabs.touch(props.tab.key)
}

// 一键美化请求体 JSON；非法 JSON 原样保留，交给用户自查
function formatJson(): void {
  try {
    props.tab.request.body.raw = JSON.stringify(JSON.parse(props.tab.request.body.raw), null, 2)
    touch()
  } catch {
    return
  }
}

// 切换 tab 时刷新解析预览
watch(
  () => props.tab.key,
  () => tabs.refreshResolve(),
)
</script>

<template>
  <div class="editor">
    <div class="seg">
      <button
        v-for="s in segments"
        :key="s.key"
        class="seg-tab"
        :class="{ on: seg === s.key }"
        type="button"
        @click="seg = s.key"
      >
        {{ s.label }}<span v-if="s.dot" class="badge" />
      </button>
    </div>

    <div class="seg-body">
      <key-value-table
        v-if="seg === 'params'"
        :rows="tab.request.params"
        :label="t('editor.query')"
        @change="touch"
      />
      <key-value-table
        v-else-if="seg === 'headers'"
        :rows="tab.request.headers"
        :label="t('editor.headers')"
        @change="touch"
      />
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
          <div class="brow">
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
            <button v-if="bodyType === 'json'" class="fmt" type="button" @click="formatJson">
              {{ t('editor.formatJson') }}
            </button>
          </div>
          <n-input
            v-if="bodyType === 'json' || bodyType === 'text'"
            v-model:value="tab.request.body.raw"
            type="textarea"
            :rows="12"
            class="mono raw"
            :placeholder="t('editor.rawPlaceholder')"
            @input="touch"
          />
          <key-value-table
            v-else-if="bodyType === 'form' || bodyType === 'multipart'"
            :rows="tab.request.body.form"
            :label="bodyType === 'form' ? t('editor.bodyForm') : t('editor.bodyMultipart')"
            @change="touch"
          />
        </div>
      </template>
      <md-editor
        v-else
        v-model="docs"
        :language="mdLanguage"
        preview-theme="default"
        class="md-edit"
      />
    </div>
  </div>
</template>

<style scoped>
.editor {
  padding: 0 14px;
  display: flex;
  flex-direction: column;
  min-height: 0;
  flex: 1 1 auto;
}

.seg {
  display: flex;
  gap: 2px;
  border-bottom: 1px solid var(--app-border);
  flex: 0 0 auto;
}

.seg-tab {
  position: relative;
  border: none;
  background: none;
  padding: 9px 12px;
  font-size: 12.5px;
  font-family: inherit;
  color: var(--app-muted);
  cursor: pointer;
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
}

.seg-tab:hover {
  color: var(--app-text);
}

.seg-tab.on {
  color: var(--app-accent);
  font-weight: 600;
  border-bottom-color: var(--app-accent);
}

.badge {
  position: absolute;
  top: 7px;
  right: 5px;
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: var(--app-accent);
}

.seg-body {
  padding: 12px 0;
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
  display: flex;
  flex-direction: column;
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
  font-size: 12.5px;
  color: var(--app-text-2);
}

.atype {
  width: 160px;
}

.brow {
  display: flex;
  align-items: center;
  gap: 8px;
}

.btype {
  width: 130px;
}

.fmt {
  border: 1px solid var(--app-border);
  background: var(--app-panel);
  border-radius: 6px;
  font-size: 11.5px;
  font-family: inherit;
  padding: 3px 9px;
  color: var(--app-text-2);
  cursor: pointer;
}

.fmt:hover {
  border-color: var(--app-accent);
  color: var(--app-accent);
}

.raw {
  font-size: 12.5px;
}

.md-edit {
  flex: 1 1 auto;
  min-height: 340px;
}
</style>

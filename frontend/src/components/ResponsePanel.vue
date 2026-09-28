<script setup lang="ts">
// 响应面板（design-spec §2）：响应头行「响应 · 200 OK · 12 ms · 1.2 KB」+ 页签（响应体 / 响应头 N）。
// 响应体可切换「美化(JSON 树) / 原始」，JSON 时附字段映射表。
import { NAlert, NIcon, NTag } from 'naive-ui'
import { ContractOutline, CopyOutline, ExpandOutline } from '@vicons/ionicons5'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import JsonViewer from '@/components/JsonViewer.vue'
import ResponseFields from '@/components/ResponseFields.vue'
import { formatBytes, httpStatusText } from '@/lib/format'
import type { Tab } from '@/stores/tabs'

const props = defineProps<{ tab: Tab }>()
const { t } = useI18n()

const view = ref<'pretty' | 'raw'>('pretty')
const seg = ref<'body' | 'headers'>('body')
const jv = ref<InstanceType<typeof JsonViewer> | null>(null)

const body = computed(() => props.tab.response?.body ?? '')

const isJson = computed(() => {
  const text = body.value.trim()
  if (!text || props.tab.response?.binary) return false
  return text.startsWith('{') || text.startsWith('[')
})

const rawText = computed(() => {
  if (view.value === 'raw' || !isJson.value) return body.value
  try {
    return JSON.stringify(JSON.parse(body.value), null, 2)
  } catch {
    return body.value
  }
})

const headerCount = computed(() => props.tab.response?.headers.length ?? 0)

const statusType = computed<'success' | 'warning' | 'error'>(() => {
  const s = props.tab.response?.status ?? 0
  if (s >= 200 && s < 300) return 'success'
  if (s >= 400 && s < 500) return 'warning'
  return 'error'
})

const statusLabel = computed(() => {
  const s = props.tab.response?.status ?? 0
  return `${s} ${httpStatusText(s)}`.trim()
})

const meta = computed(() => {
  const r = props.tab.response
  if (!r) return ''
  return `${r.timeMs} ms${t('common.sep')}${formatBytes(r.size)}`
})

const copied = ref(false)

async function copyBody(): Promise<void> {
  if (!props.tab.response) return
  await navigator.clipboard.writeText(props.tab.response.body)
  copied.value = true
  setTimeout(() => (copied.value = false), 1200)
}
</script>

<template>
  <div class="resp">
    <n-alert v-if="tab.error" type="error" :bordered="false" class="err">{{ tab.error }}</n-alert>

    <template v-else-if="tab.response">
      <div class="head">
        <span class="ttl">{{ t('resp.title') }}</span>
        <n-tag :type="statusType" size="small" :bordered="false" class="badge" :title="tab.response.proto">
          {{ statusLabel }}
        </n-tag>
        <span class="meta mono">{{ meta }}</span>
        <span class="sp" />
        <template v-if="seg === 'body'">
          <button v-if="isJson" class="toggle" :class="{ on: view === 'pretty' }" type="button" @click="view = 'pretty'">
            {{ t('resp.pretty') }}
          </button>
          <button v-if="isJson" class="toggle" :class="{ on: view === 'raw' }" type="button" @click="view = 'raw'">
            {{ t('resp.raw') }}
          </button>
          <button
            v-if="isJson && view === 'pretty'"
            class="toggle"
            type="button"
            :title="t('json.expandAll')"
            @click="jv?.expandAll()"
          >
            <n-icon :component="ExpandOutline" :size="14" />
          </button>
          <button
            v-if="isJson && view === 'pretty'"
            class="toggle"
            type="button"
            :title="t('json.collapseAll')"
            @click="jv?.collapseAll()"
          >
            <n-icon :component="ContractOutline" :size="14" />
          </button>
        </template>
        <button class="toggle" type="button" @click="copyBody">
          <n-icon :component="CopyOutline" :size="13" />
          {{ copied ? t('common.copied') : t('resp.copyBody') }}
        </button>
      </div>

      <div class="seg">
        <button class="seg-tab" :class="{ on: seg === 'body' }" type="button" @click="seg = 'body'">
          {{ t('resp.body') }}
        </button>
        <button class="seg-tab" :class="{ on: seg === 'headers' }" type="button" @click="seg = 'headers'">
          {{ t('resp.headers') }}<span v-if="headerCount" class="num">{{ headerCount }}</span>
        </button>
        <span class="url mono" :title="tab.response.url">{{ tab.response.url }}</span>
      </div>

      <div class="pane">
        <template v-if="seg === 'body'">
          <div v-if="tab.response.binary" class="binhint">{{ t('resp.binary') }}</div>
          <json-viewer v-if="isJson && view === 'pretty'" ref="jv" :text="tab.response.body" />
          <pre v-else class="raw mono">{{ rawText }}</pre>
          <response-fields v-if="isJson" :uid="tab.uid" :text="tab.response.body" />
        </template>
        <div v-else class="hlist">
          <div v-for="h in tab.response.headers" :key="h.name" class="hrow mono">
            <span class="hn">{{ h.name }}</span>
            <span class="hv">{{ h.value }}</span>
          </div>
        </div>
      </div>
    </template>

    <div v-else class="empty">
      <svg class="plane" viewBox="0 0 24 24" width="56" height="56" fill="none" stroke="currentColor" stroke-width="1.4">
        <path d="M22 2 11 13" />
        <path d="M22 2 15 22l-4-9-9-4 20-7Z" />
      </svg>
      <p class="etitle">{{ t('resp.empty') }}</p>
      <ul class="shortcuts">
        <li><kbd>Ctrl</kbd> + <kbd>Enter</kbd><span>{{ t('resp.sendHint') }}</span></li>
        <li><kbd>Ctrl</kbd> + <kbd>N</kbd><span>{{ t('resp.newHint') }}</span></li>
        <li><kbd>Ctrl</kbd> + <kbd>E</kbd><span>{{ t('resp.envHint') }}</span></li>
      </ul>
    </div>
  </div>
</template>

<style scoped>
.resp {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  padding: 10px 12px 10px;
}

.err {
  margin-bottom: 8px;
}

.head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: nowrap;
  overflow: hidden;
  flex: 0 0 auto;
}

.ttl {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--app-text);
  flex: 0 0 auto;
}

.badge {
  font-family: var(--app-mono);
  font-weight: 700;
  flex: 0 0 auto;
}

.meta {
  font-size: 11.5px;
  color: var(--app-muted);
  white-space: nowrap;
  flex: 0 0 auto;
}

.sp {
  flex: 1 1 auto;
}

.toggle {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border: 1px solid var(--app-border);
  background: var(--app-panel);
  border-radius: 5px;
  font-size: 11px;
  font-family: inherit;
  padding: 2px 9px;
  cursor: pointer;
  color: var(--app-text-2);
  white-space: nowrap;
  flex: 0 0 auto;
}

.toggle.on {
  border-color: var(--app-accent);
  color: var(--app-accent);
  background: var(--app-accent-tint);
}

.seg {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-top: 8px;
  border-bottom: 1px solid var(--app-border);
  flex: 0 0 auto;
}

.seg-tab {
  border: none;
  background: none;
  padding: 7px 10px;
  font-size: 12.5px;
  font-family: inherit;
  color: var(--app-muted);
  cursor: pointer;
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
  white-space: nowrap;
}

.seg-tab.on {
  color: var(--app-accent);
  font-weight: 600;
  border-bottom-color: var(--app-accent);
}

.num {
  margin-left: 4px;
  font-size: 10.5px;
  color: var(--app-placeholder);
}

.url {
  flex: 1 1 auto;
  margin-left: 8px;
  font-size: 11.5px;
  color: var(--app-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-align: right;
}

.pane {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
  margin-top: 8px;
}

.binhint {
  font-size: 12px;
  color: #d08830;
  margin-bottom: 6px;
}

.raw {
  margin: 0;
  padding: 10px;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: #fafafa;
  font-size: 12px;
  line-height: 1.55;
  white-space: pre-wrap;
  word-break: break-all;
}

.hlist {
  display: flex;
  flex-direction: column;
}

.hrow {
  display: flex;
  gap: 10px;
  font-size: 12px;
  padding: 3px 6px;
  border-bottom: 1px dashed #eef0f2;
}

.hrow:hover {
  background: #fafbfc;
}

.hn {
  color: var(--app-code-key);
  flex: 0 0 200px;
  overflow: hidden;
  text-overflow: ellipsis;
}

.hv {
  word-break: break-all;
}

.empty {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  color: var(--app-muted);
}

.plane {
  color: var(--app-border-strong);
  margin-bottom: 6px;
}

.etitle {
  margin: 0;
  font-size: 13px;
  color: var(--app-placeholder);
}

.shortcuts {
  list-style: none;
  margin: 10px 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.shortcuts li {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--app-placeholder);
}

.shortcuts span {
  margin-left: 6px;
}

kbd {
  border: 1px solid var(--app-border-strong);
  border-bottom-width: 2px;
  border-radius: 4px;
  padding: 1px 6px;
  font-family: var(--app-mono);
  font-size: 11px;
  background: var(--app-panel);
  color: var(--app-muted);
}
</style>

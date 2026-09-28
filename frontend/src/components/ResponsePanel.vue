<script setup lang="ts">
// 响应面板：状态 / 耗时 / 体积 / 响应头 / 响应体（Pretty/Raw）；无响应时显示空态（纸飞机 + 快捷键）。
import { NAlert, NTag } from 'naive-ui'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Tab } from '@/stores/tabs'

const props = defineProps<{ tab: Tab }>()
const { t } = useI18n()

const view = ref<'pretty' | 'raw'>('pretty')

const bodyText = computed<string>(() => {
  const body = props.tab.response?.body ?? ''
  if (view.value === 'raw') return body
  try {
    return JSON.stringify(JSON.parse(body), null, 2)
  } catch {
    return body
  }
})

const statusType = computed<'success' | 'warning' | 'error'>(() => {
  const s = props.tab.response?.status ?? 0
  if (s >= 200 && s < 300) return 'success'
  if (s >= 400 && s < 500) return 'warning'
  return 'error'
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
      <div class="bar">
        <n-tag :type="statusType" size="small" :bordered="false" class="status">{{ tab.response.status }}</n-tag>
        <span class="meta mono">{{ tab.response.proto }}</span>
        <span class="meta">{{ tab.response.timeMs }} ms</span>
        <span class="meta">{{ tab.response.size }} B</span>
        <span v-if="tab.response.contentType" class="meta mono ct">{{ tab.response.contentType }}</span>
        <span class="sp" />
        <button class="toggle" :class="{ on: view === 'pretty' }" @click="view = 'pretty'">{{ t('resp.pretty') }}</button>
        <button class="toggle" :class="{ on: view === 'raw' }" @click="view = 'raw'">{{ t('resp.raw') }}</button>
        <button class="copy" @click="copyBody">{{ copied ? t('common.copied') : t('resp.copyBody') }}</button>
      </div>

      <div class="hdr">
        <span class="htitle">{{ t('resp.headers') }}</span>
        <span class="hurl mono" :title="tab.response.url">{{ tab.response.url }}</span>
      </div>
      <div class="hlist">
        <div v-for="h in tab.response.headers" :key="h.name" class="hrow mono">
          <span class="hn">{{ h.name }}</span>
          <span class="hv">{{ h.value }}</span>
        </div>
      </div>

      <div v-if="tab.response.binary" class="binhint">{{ t('resp.binary') }}</div>
      <pre class="body mono">{{ bodyText }}</pre>
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
  overflow: auto;
  padding: 10px 12px;
}

.err {
  margin-bottom: 10px;
}

.bar {
  display: flex;
  align-items: center;
  gap: 10px;
}

.status {
  font-weight: 700;
}

.meta {
  font-size: 12px;
  color: var(--app-muted);
}

.ct {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 200px;
}

.binhint {
  margin-top: 8px;
  font-size: 12px;
  color: #d08830;
}

.sp {
  flex: 1 1 auto;
}

.toggle {
  border: 1px solid var(--app-border);
  background: var(--app-panel);
  border-radius: 4px;
  font-size: 11px;
  padding: 2px 9px;
  cursor: pointer;
  color: #555;
}

.toggle.on {
  border-color: var(--app-accent);
  color: var(--app-accent);
  background: var(--app-active);
}

.copy {
  border: 1px solid var(--app-border);
  background: var(--app-panel);
  border-radius: 4px;
  font-size: 11px;
  padding: 2px 9px;
  cursor: pointer;
  color: #555;
  white-space: nowrap;
}

.copy:hover {
  border-color: var(--app-accent);
  color: var(--app-accent);
}

.hdr {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 10px;
  padding-bottom: 4px;
  border-bottom: 1px solid var(--app-border);
}

.htitle {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 1px;
  text-transform: uppercase;
  color: var(--app-muted);
}

.hurl {
  font-size: 11.5px;
  color: var(--app-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.hlist {
  margin: 6px 0;
  max-height: 130px;
  overflow: auto;
}

.hrow {
  display: flex;
  gap: 8px;
  font-size: 12px;
  padding: 1px 0;
}

.hn {
  color: var(--app-muted);
  flex: 0 0 auto;
}

.body {
  margin: 8px 0 0;
  padding: 10px;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: var(--app-sidebar);
  font-size: 12px;
  line-height: 1.5;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
  flex: 1 1 auto;
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
  color: #9aa0a6;
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
  color: #9aa0a6;
}

.shortcuts span {
  margin-left: 6px;
}

kbd {
  border: 1px solid var(--app-border-strong);
  border-bottom-width: 2px;
  border-radius: 4px;
  padding: 1px 6px;
  font-family: ui-monospace, monospace;
  font-size: 11px;
  background: var(--app-panel);
  color: #6b7280;
}
</style>

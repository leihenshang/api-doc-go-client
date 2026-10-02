<script setup lang="ts">
// gRPC Options 分段（设计文档 G7 / P6）：连接安全（明文 / TLS / mTLS + 证书）、
// 请求级超时覆盖、跳过证书校验、请求压缩。
// 规则：连接设置落 `grpc.tls`；超时落请求级 `settings.timeoutSec`（留空 = 用全局设置）；
// 跳过校验「全局或请求级任一开启即生效」（执行器侧合并，G7.3）。
import { NButton, NCheckbox, NIcon, NInput, NInputNumber, NSelect } from 'naive-ui'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { FolderOpenOutline } from '@vicons/ionicons5'
import { grpcOf } from '@/lib/grpc'
import { api } from '@/lib/ipc'
import { message } from '@/lib/notice'
import { useCollectionStore } from '@/stores/collection'
import { useSettingsStore } from '@/stores/settings'
import { useTabsStore } from '@/stores/tabs'
import type { Tab } from '@/stores/tabs'

const props = defineProps<{ tab: Tab }>()
const tabs = useTabsStore()
const coll = useCollectionStore()
const settings = useSettingsStore()
const { t } = useI18n()

const grpc = computed(() => grpcOf(props.tab.request))
const readonly = computed(() => coll.isReadOnly)
/** 连接模式：明文（默认）/ TLS（mTLS 由是否给客户端证书决定） */
const mode = computed(() => {
  const m = (grpc.value.tls?.mode ?? '').trim().toLowerCase()
  return m === 'tls' || m === 'mtls' ? 'tls' : 'plaintext'
})
const tlsEnabled = computed(() => mode.value === 'tls')

function ensureTLS(): NonNullable<typeof grpc.value.tls> {
  const r = props.tab.request
  if (!r.grpc) r.grpc = { target: '', service: '', method: '', tls: {} }
  if (!r.grpc.tls) r.grpc.tls = { mode: 'plaintext' }
  return r.grpc.tls
}

function touch(): void {
  tabs.touch(props.tab.key)
}

function setMode(v: string | number): void {
  if (readonly.value) return
  const tls = ensureTLS()
  tls.mode = String(v)
  touch()
}

function setTLSField(field: 'ca' | 'cert' | 'key', value: string): void {
  if (readonly.value) return
  const tls = ensureTLS()
  tls[field] = value
  touch()
}

function setSkipVerify(v: boolean): void {
  if (readonly.value) return
  ensureTLS().insecureSkipVerify = v
  touch()
}

async function browse(field: 'ca' | 'cert' | 'key'): Promise<void> {
  try {
    const path = await api.pickFile()
    if (path) setTLSField(field, path)
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

/** 请求级超时（秒）：留空 = 用全局设置（G7.4）。 */
const timeout = computed<number | null>({
  get: () => props.tab.request.settings?.timeoutSec ?? null,
  set: (v) => {
    const r = props.tab.request
    const next = { ...(r.settings ?? {}) }
    if (v == null || v <= 0) delete next.timeoutSec
    else next.timeoutSec = Math.round(v)
    r.settings = Object.keys(next).length ? next : null
    touch()
  },
})

function setCompress(v: string | number): void {
  if (readonly.value) return
  const r = props.tab.request
  if (!r.grpc) return
  r.grpc.compress = String(v) === 'gzip' ? 'gzip' : ''
  touch()
}

const modeOptions = computed(() => [
  { label: t('grpc.options.plaintext'), value: 'plaintext' },
  { label: t('grpc.options.tls'), value: 'tls' },
])
const compressOptions = computed(() => [
  { label: t('grpc.options.compressNone'), value: '' },
  { label: 'gzip', value: 'gzip' },
])
</script>

<template>
  <div class="opt-pane">
    <div class="orow">
      <span class="lbl">{{ t('grpc.options.connection') }}</span>
      <n-select
        :value="mode"
        :options="modeOptions"
        size="small"
        class="ctl"
        :disabled="readonly"
        data-testid="grpc.options.mode"
        @update:value="setMode"
      />
      <span class="hint">{{ t('grpc.options.connectionHint') }}</span>
    </div>

    <template v-if="tlsEnabled">
      <div v-for="f in (['ca', 'cert', 'key'] as const)" :key="f" class="orow">
        <span class="lbl">{{ t(`grpc.options.${f}`) }}</span>
        <n-input
          :value="grpc.tls?.[f] ?? ''"
          size="small"
          class="ctl mono"
          :placeholder="t(`grpc.options.${f}Placeholder`)"
          :disabled="readonly"
          :data-testid="`grpc.options.${f}`"
          @update:value="setTLSField(f, $event)"
        />
        <n-button size="small" quaternary :disabled="readonly" @click="browse(f)">
          <template #icon><n-icon :component="FolderOpenOutline" /></template>
          {{ t('grpc.browse') }}
        </n-button>
      </div>
      <div class="orow">
        <span class="lbl">{{ t('grpc.options.skipVerify') }}</span>
        <n-checkbox
          :checked="grpc.tls?.insecureSkipVerify === true"
          :disabled="readonly"
          data-testid="grpc.options.skipVerify"
          @update:checked="setSkipVerify"
        />
        <span class="hint">
          {{ settings.form.insecureSsl ? t('grpc.options.skipVerifyGlobalOn') : t('grpc.options.skipVerifyHint') }}
        </span>
      </div>
    </template>

    <div class="orow">
      <span class="lbl">{{ t('grpc.options.timeout') }}</span>
      <n-input-number
        :value="timeout"
        size="small"
        class="ctl"
        :min="1"
        :max="3600"
        :placeholder="String(settings.form.timeoutSec ?? 30)"
        :disabled="readonly"
        data-testid="grpc.options.timeout"
        @update:value="timeout = $event"
      />
      <span class="hint">{{ t('grpc.options.timeoutHint', { n: settings.form.timeoutSec ?? 30 }) }}</span>
    </div>

    <div class="orow">
      <span class="lbl">{{ t('grpc.options.compress') }}</span>
      <n-select
        :value="grpc.compress ?? ''"
        :options="compressOptions"
        size="small"
        class="ctl"
        :disabled="readonly"
        data-testid="grpc.options.compress"
        @update:value="setCompress"
      />
      <span class="hint">{{ t('grpc.options.compressHint') }}</span>
    </div>
  </div>
</template>

<style scoped>
.opt-pane {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 12px;
  overflow: auto;
}

.orow {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.lbl {
  width: 96px;
  flex: 0 0 auto;
  font-size: 12px;
  color: var(--app-text);
}

.ctl {
  width: 240px;
  flex: 0 0 auto;
}

.hint {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 11.5px;
  color: var(--app-muted);
}
</style>

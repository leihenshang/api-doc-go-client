<script setup lang="ts">
// 同步（I1–I6）：绑定服务端项目 / 手动拉推 / 冲突三选一 / 状态展示。
import { NButton, NInput, NInputNumber, NModal, NSelect } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/ipc'
import { message } from '@/lib/notice'
import type { ConflictItem, SyncBindInfo, SyncReport } from '@/types'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ 'update:show': [v: boolean]; synced: [] }>()
const { t } = useI18n()

const bind = ref<SyncBindInfo>({ linked: false, serverUrl: '', projectId: 0, mode: 'auto', cursor: 0 })
const token = ref('')
const serverUrl = ref('')
const projectId = ref(0)
const mode = ref('auto')
const report = ref<SyncReport | null>(null)
const error = ref('')
const busy = ref(false)
const conflicts = ref<ConflictItem[]>([])

const modeOptions = computed(() => [
  { label: t('sync.modeAuto'), value: 'auto' },
  { label: t('sync.modeManual'), value: 'manual' },
  { label: t('sync.modeMirror'), value: 'mirror' },
])

watch(
  () => props.show,
  async (v) => {
    if (!v) return
    error.value = ''
    report.value = null
    try {
      bind.value = (await api.getSyncBind()) ?? bind.value
      serverUrl.value = bind.value.serverUrl ?? ''
      projectId.value = bind.value.projectId ?? 0
      mode.value = bind.value.mode || 'auto'
      await loadConflicts()
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  },
)

async function loadConflicts(): Promise<void> {
  try {
    conflicts.value = await api.listConflicts()
  } catch {
    conflicts.value = []
  }
}

async function doBind(): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    bind.value = (await api.setSyncBind(serverUrl.value, projectId.value, mode.value, token.value)) ?? bind.value
    message.success(t('sync.bound'))
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function doUnbind(): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    await api.unbindSync()
    bind.value = { linked: false, serverUrl: '', projectId: 0, mode: 'auto', cursor: 0 }
    message.success(t('sync.unbound'))
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function doSync(): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    report.value = await api.runSync(token.value)
    message.success(t('sync.done', { p: report.value?.pulled ?? 0, s: report.value?.pushed ?? 0 }))
    await loadConflicts()
    emit('synced')
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

/** 冲突三选一（I4）。 */
async function resolve(file: string, choice: 'local' | 'remote' | 'copy'): Promise<void> {
  try {
    await api.resolveConflict(file, choice)
    message.success(t('sync.resolved'))
    await loadConflicts()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}
</script>

<template>
  <n-modal
    :show="show"
    preset="card"
    :title="t('sync.title')"
    style="width: 560px"
    @update:show="emit('update:show', false)"
  >
    <div class="wrap">
      <p class="hint">{{ t('sync.hint') }}</p>

      <div class="row">
        <span class="lbl">{{ t('sync.serverUrl') }}</span>
        <n-input v-model:value="serverUrl" size="small" data-testid="sync.url" placeholder="http://127.0.0.1:2027" />
      </div>
      <div class="row">
        <span class="lbl">{{ t('sync.projectId') }}</span>
        <n-input-number v-model:value="projectId" size="small" :min="1" class="num" data-testid="sync.project" />
      </div>
      <div class="row">
        <span class="lbl">{{ t('sync.mode') }}</span>
        <n-select v-model:value="mode" :options="modeOptions" size="small" class="num" data-testid="sync.mode" />
      </div>
      <div class="row">
        <span class="lbl">PAT</span>
        <n-input
          v-model:value="token"
          size="small"
          type="password"
          show-password-on="click"
          data-testid="sync.token"
          :placeholder="t('sync.tokenPlaceholder')"
        />
      </div>

      <div class="row acts">
        <n-button v-if="!bind.linked" size="small" type="primary" :loading="busy" data-testid="sync.bind" @click="doBind">
          {{ t('sync.bind') }}
        </n-button>
        <template v-else>
          <span class="ok">{{ t('sync.linkedTo', { url: bind.serverUrl, id: bind.projectId }) }}</span>
          <span class="sp" />
          <n-button size="small" :loading="busy" data-testid="sync.run" @click="doSync">
            {{ t('sync.run') }}
          </n-button>
          <n-button size="small" quaternary :loading="busy" data-testid="sync.unbind" @click="doUnbind">
            {{ t('sync.unbind') }}
          </n-button>
        </template>
      </div>

      <div v-if="bind.linked && bind.mode !== 'mirror'" class="row">
        <span class="sp" />
        <n-button v-if="!bind.linked" size="tiny" />
      </div>

      <p v-if="report" class="rep" data-testid="sync.report">
        {{ t('sync.report', { p: report.pulled, s: report.pushed, c: report.conflicts, r: report.rejected }) }}
      </p>
      <p v-if="error" class="err">{{ error }}</p>

      <!-- 冲突三选一（I4） -->
      <div v-if="conflicts.length" class="conflicts" data-testid="sync.conflicts">
        <div class="chd">{{ t('sync.conflicts', { n: conflicts.length }) }}</div>
        <div v-for="c in conflicts" :key="c.file" class="crow" data-testid="sync.conflict">
          <span class="cname">{{ c.name || c.file }}</span>
          <span class="crev">rev {{ c.serverRev }}</span>
          <span class="sp" />
          <n-button size="tiny" data-testid="conflict.local" @click="resolve(c.file, 'local')">
            {{ t('sync.keepLocal') }}
          </n-button>
          <n-button size="tiny" data-testid="conflict.remote" @click="resolve(c.file, 'remote')">
            {{ t('sync.keepRemote') }}
          </n-button>
          <n-button size="tiny" quaternary data-testid="conflict.copy" @click="resolve(c.file, 'copy')">
            {{ t('sync.keepCopy') }}
          </n-button>
        </div>
      </div>
    </div>
  </n-modal>
</template>

<style scoped>
.wrap {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.hint {
  margin: 0;
  font-size: 12px;
  color: var(--app-muted);
}

.row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.lbl {
  width: 80px;
  font-size: 12px;
  color: var(--app-muted);
}

.num {
  width: 180px;
}

.sp {
  flex: 1;
}

.acts {
  margin-top: 6px;
}

.ok {
  font-size: 12px;
  color: var(--app-accent);
}

.rep {
  margin: 0;
  font-size: 12px;
  color: var(--app-text-2);
}

.err {
  margin: 0;
  color: var(--app-danger, #d03050);
  font-size: 12px;
}

.conflicts {
  border: 1px solid var(--app-border);
  border-radius: 6px;
  padding: 8px 10px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.chd {
  font-size: 12px;
  font-weight: 500;
  color: var(--app-danger, #d03050);
}

.crow {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
}

.cname {
  font-weight: 500;
}

.crev {
  color: var(--app-muted);
  font-family: var(--app-mono);
}
</style>

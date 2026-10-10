<script setup lang="ts">
// 同步（I1–I6）：绑定服务端项目 / 手动拉推 / 冲突应用内二选一 / 状态展示。
import ConflictResolver from '@/components/ConflictResolver.vue'
import { api } from '@/lib/ipc'
import { message } from '@/lib/notice'
import type { ConflictDetail, SyncBindInfo, SyncReport } from '@/types'
import { NButton, NInput, NInputNumber, NModal, NSelect } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

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
const details = ref<ConflictDetail[]>([])

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
    const items = await api.listConflicts()
    // 并发取每份副本的差异详情；单份损坏不影响其余展示
    details.value = (
      await Promise.all(
        items.map(async (it) => {
          try {
            return await api.getConflictDetail(it.file)
          } catch {
            return null
          }
        }),
      )
    ).filter((d): d is ConflictDetail => d !== null)
  } catch {
    details.value = []
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

/** 冲突二选一（I4）：应用选择后重新加载。 */
async function resolve(file: string, choice: 'local' | 'remote'): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    await api.resolveConflict(file, choice)
    message.success(t('sync.resolved'))
    await loadConflicts()
    emit('synced')
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

/** 批量二选一：「全部采用服务器」时自动跳过缺少服务器快照的旧副本。 */
async function resolveAll(choice: 'local' | 'remote'): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    for (const d of details.value) {
      if (choice === 'remote' && !d.hasPayload) continue
      await api.resolveConflict(d.file, choice)
    }
    message.success(t('sync.resolved'))
    await loadConflicts()
    emit('synced')
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <n-modal :show="show" preset="card" :title="t('sync.title')" style="width: 660px"
    @update:show="emit('update:show', false)">
    <div class="wrap">
      <!-- 连接信息（紧凑面板，与下方冲突分开） -->
      <section class="conn">
        <div class="grid">
          <div class="cell">
            <label class="cl">{{ t('sync.serverUrl') }}</label>
            <n-input v-model:value="serverUrl" size="small" data-testid="sync.url"
              placeholder="http://127.0.0.1:2027" />
          </div>
          <div class="cell">
            <label class="cl">
              {{ t('sync.projectId') }}
              <span class="cl-hint">{{ t('sync.projectIdHint') }}</span>
            </label>
            <n-input-number v-model:value="projectId" size="small" :min="1" class="full" data-testid="sync.project" />
          </div>
          <div class="cell">
            <label class="cl">{{ t('sync.mode') }}</label>
            <n-select v-model:value="mode" :options="modeOptions" size="small" data-testid="sync.mode" />
          </div>
          <div class="cell">
            <label class="cl">PAT</label>
            <n-input v-model:value="token" size="small" type="password" show-password-on="click"
              data-testid="sync.token" :placeholder="t('sync.tokenPlaceholder')" />
          </div>
        </div>

        <div class="conn-ft">
          <n-button v-if="!bind.linked" size="small" type="primary" :loading="busy" data-testid="sync.bind"
            @click="doBind">
            {{ t('sync.bind') }}
          </n-button>
          <template v-else>
            <span class="ok">{{ t('sync.linkedBadge') }}</span>
            <span class="sp" />
            <n-button size="small" :loading="busy" data-testid="sync.run" @click="doSync">
              {{ t('sync.run') }}
            </n-button>
            <n-button size="small" quaternary :loading="busy" data-testid="sync.unbind" @click="doUnbind">
              {{ t('sync.unbind') }}
            </n-button>
          </template>
        </div>
      </section>

      <p v-if="report" class="rep" data-testid="sync.report">
        {{ t('sync.report', { p: report.pulled, s: report.pushed, c: report.conflicts, r: report.rejected }) }}
      </p>
      <p v-if="error" class="err">{{ error }}</p>

      <!-- 冲突区：与连接信息分开，默认折叠列表，点开看差异 -->
      <section v-if="details.length" class="conflicts" data-testid="sync.conflicts">
        <div class="chd">
          <span>{{ t('sync.conflicts', { n: details.length }) }}</span>
          <span class="sp" />
          <n-button size="tiny" data-testid="conflict.allLocal" @click="resolveAll('local')">
            {{ t('sync.takeAllLocal') }}
          </n-button>
          <n-button size="tiny" data-testid="conflict.allRemote" @click="resolveAll('remote')">
            {{ t('sync.takeAllRemote') }}
          </n-button>
        </div>
        <ConflictResolver v-for="d in details" :key="d.file" :detail="d" :busy="busy"
          @resolve="(choice: 'local' | 'remote') => resolve(d.file, choice)" />
      </section>
    </div>
  </n-modal>
</template>

<style scoped>
.wrap {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

/* ---- 连接信息紧凑面板 ---- */
.conn {
  border: 1px solid var(--app-border);
  border-radius: 8px;
  padding: 10px;
  background: var(--app-hover-soft);
}

.grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px 12px;
}

.cell {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.cl {
  display: flex;
  align-items: baseline;
  gap: 6px;
  font-size: 11px;
  color: var(--app-muted);
}

.cl-hint {
  font-size: 10px;
  color: var(--app-placeholder);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.full {
  width: 100%;
}

.conn-ft {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 10px;
}

.sp {
  flex: 1;
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
  border-radius: 8px;
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-height: 440px;
  overflow-y: auto;
}

.chd {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  font-weight: 500;
  color: var(--app-danger);
  position: sticky;
  top: -10px;
  /* 抵消容器 padding，滚动时标题栏贴顶 */
  background: var(--app-panel);
  padding: 4px 0;
  z-index: 1;
}
</style>

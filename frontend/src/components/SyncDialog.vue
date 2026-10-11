<script setup lang="ts">
// 同步设置与冲突（I1–I6）：绑定服务端项目 / 解绑 / 模式与 PAT 维护 / 冲突应用内二选一。
// 「立即同步」不在这里触发 —— 侧栏根行的刷新按钮直接同步，同步后有冲突才打开本弹窗。
import ConflictResolver from '@/components/ConflictResolver.vue'
import { api } from '@/lib/ipc'
import { message } from '@/lib/notice'
import type { ConflictDetail, SyncBindInfo, SyncBindReq } from '@/types'
import { NButton, NInput, NInputNumber, NModal, NSelect } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ show: boolean; root?: string }>()
const emit = defineEmits<{ 'update:show': [v: boolean]; synced: [root: string] }>()
const { t } = useI18n()

const rootId = computed(() => props.root ?? '')

const bind = ref<SyncBindInfo>({ linked: false, serverUrl: '', projectId: 0, mode: 'auto', cursor: 0 })
const token = ref('')
const serverUrl = ref('')
const projectId = ref(0)
const mode = ref('auto')
const error = ref('')
const busy = ref(false)
const details = ref<ConflictDetail[]>([])

const modeOptions = computed(() => [
  { label: t('sync.modeAuto'), value: 'auto' },
  { label: t('sync.modeManual'), value: 'manual' },
  { label: t('sync.modeMirror'), value: 'mirror' },
])

watch(
  [() => props.show, () => props.root],
  async ([v]) => {
    if (!v) return
    error.value = ''
    try {
      bind.value = (await api.getSyncBindFor(rootId.value)) ?? bind.value
      serverUrl.value = bind.value.serverUrl ?? ''
      projectId.value = bind.value.projectId ?? 0
      mode.value = bind.value.mode || 'auto'
      // PAT 已随集合持久化：回显已保存令牌（留空保存 = 沿用），重启客户端无需重填
      token.value = bind.value.token ?? ''
      await loadConflicts()
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  },
)

async function loadConflicts(): Promise<void> {
  try {
    const items = await api.listConflictsFor(rootId.value)
    // 并发取每份副本的差异详情；单份损坏不影响其余展示
    details.value = (
      await Promise.all(
        items.map(async (it) => {
          try {
            return await api.getConflictDetailFor(rootId.value, it.file)
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

function bindReq(): SyncBindReq {
  return {
    root: rootId.value,
    serverUrl: serverUrl.value,
    projectId: projectId.value ?? 0,
    mode: mode.value,
    token: token.value,
  }
}

async function doBind(): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    bind.value = (await api.setSyncBindFor(bindReq())) ?? bind.value
    token.value = bind.value.token ?? token.value
    message.success(t('sync.bound'))
    emit('synced', rootId.value)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function doSave(): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    bind.value = (await api.setSyncBindFor(bindReq())) ?? bind.value
    token.value = bind.value.token ?? token.value
    message.success(t('sync.saved'))
    emit('synced', rootId.value)
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
    await api.unbindSyncFor(rootId.value)
    bind.value = { linked: false, serverUrl: '', projectId: 0, mode: 'auto', cursor: 0 }
    token.value = ''
    message.success(t('sync.unbound'))
    emit('synced', rootId.value)
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
    await api.resolveConflictFor(rootId.value, file, choice)
    message.success(t('sync.resolved'))
    await loadConflicts()
    emit('synced', rootId.value)
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
      await api.resolveConflictFor(rootId.value, d.file, choice)
    }
    message.success(t('sync.resolved'))
    await loadConflicts()
    emit('synced', rootId.value)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <n-modal :show="show" preset="card" :title="t('sync.settingsTitle')" style="width: 660px"
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
        <p class="hint">{{ t('sync.tokenSavedHint') }}</p>

        <div class="conn-ft">
          <n-button v-if="!bind.linked" size="small" type="primary" :loading="busy" data-testid="sync.bind"
            @click="doBind">
            {{ t('sync.bind') }}
          </n-button>
          <template v-else>
            <span class="ok">{{ t('sync.linkedBadge') }}</span>
            <span class="sp" />
            <n-button size="small" type="primary" :loading="busy" data-testid="sync.save" @click="doSave">
              {{ t('sync.save') }}
            </n-button>
            <n-button size="small" quaternary :loading="busy" data-testid="sync.unbind" @click="doUnbind">
              {{ t('sync.unbind') }}
            </n-button>
          </template>
        </div>
      </section>

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

.hint {
  margin: 6px 0 0;
  font-size: 10px;
  line-height: 1.5;
  color: var(--app-placeholder);
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

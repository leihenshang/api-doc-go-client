<script setup lang="ts">
// 根组件：工具栏（集合名/环境/动作）+ 侧栏（集合树）+ 多 TAB 工作台（编辑/响应横向分屏）。
// naive-ui 的 useMessage 必须在 provider 后代中调用，而 App 自身的 setup 不是自己的
// NMessageProvider 的后代 —— 因此这里用离散 API（createDiscreteApi）取 message。
import {
  createDiscreteApi,
  NButton,
  NConfigProvider,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NSelect,
  NSpin,
  NTag,
  zhCN,
  enUS,
} from 'naive-ui'
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import EnvManager from '@/components/EnvManager.vue'
import EnvPicker from '@/components/EnvPicker.vue'
import HistoryDialog from '@/components/HistoryDialog.vue'
import RequestEditor from '@/components/RequestEditor.vue'
import ResponsePanel from '@/components/ResponsePanel.vue'
import SettingsDialog from '@/components/SettingsDialog.vue'
import Sidebar from '@/components/Sidebar.vue'
import TabBar from '@/components/TabBar.vue'
import Welcome from '@/components/Welcome.vue'
import { api } from '@/lib/ipc'
import { useCollectionStore } from '@/stores/collection'
import { useTabsStore } from '@/stores/tabs'

const coll = useCollectionStore()
const tabs = useTabsStore()
const { t, locale } = useI18n()

const naiveLocale = computed(() => (locale.value === 'en-US' ? enUS : zhCN))
const { message } = createDiscreteApi(['message'], {
  configProviderProps: computed(() => ({ locale: naiveLocale.value })),
})

const showEnvManager = ref(false)
const showSettings = ref(false)
const showHistory = ref(false)
const sideOpen = ref(true)
const lastDir = localStorage.getItem('client.lastDir') ?? ''

// ---- 打开 / 切换集合 ----
async function openCollection(dir: string): Promise<void> {
  if (!dir) {
    try {
      dir = await api.pickDirectory()
    } catch {
      return
    }
  }
  if (!dir) {
    message.warning(t('welcome.pickFailed'))
    return
  }
  tabs.reset()
  try {
    await coll.open(dir)
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

onMounted(() => {
  if (lastDir) void openCollection(lastDir)
})

// ---- 新建请求 / 分组弹窗 ----
const showCreate = ref(false)
const createForm = ref({ name: '', folder: '', method: 'GET' })
const folderOptions = computed(() => {
  const out: { label: string; value: string }[] = [{ label: t('prompt.folder'), value: '' }]
  const walk = (nodes: typeof coll.tree): void => {
    for (const n of nodes) {
      if (n.type === 'folder') {
        out.push({ label: n.name, value: n.path })
        if (n.children) walk(n.children)
      }
    }
  }
  walk(coll.tree)
  return out
})

function openCreate(): void {
  createForm.value = { name: '', folder: '', method: 'GET' }
  showCreate.value = true
}

async function submitCreate(): Promise<void> {
  if (!createForm.value.name.trim()) return
  try {
    const r = await api.createRequest(createForm.value.folder, createForm.value.name.trim(), createForm.value.method)
    showCreate.value = false
    await coll.reload()
    tabs.openDoc(r)
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

const showFolder = ref(false)
const folderName = ref('')

async function submitFolder(): Promise<void> {
  if (!folderName.value.trim()) return
  try {
    await api.createFolder(folderName.value.trim())
    showFolder.value = false
    await coll.reload()
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

async function deleteRequest(uid: string): Promise<void> {
  try {
    await tabs.deleteRequest(uid)
    await coll.reload()
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

function envSaved(): void {
  tabs.refreshResolve()
}

function toggleLang(): void {
  locale.value = locale.value === 'zh-CN' ? 'en-US' : 'zh-CN'
  localStorage.setItem('client.lang', locale.value)
}

// 集合切换后刷新解析预览
watch(
  () => coll.uid,
  () => tabs.refreshResolve(),
)
</script>

<template>
  <n-config-provider :locale="naiveLocale">
    <div class="app">
      <n-spin :show="coll.loading">
        <template v-if="coll.ready && coll.info">
          <header class="toolbar">
            <button class="menu" :title="t('toolbar.toggleSide')" @click="sideOpen = !sideOpen">☰</button>
            <span class="brand">API-DOC</span>
            <n-tag size="small" :bordered="false" class="ws" :title="coll.dir">{{ coll.name }}</n-tag>
            <span class="sp" />
            <env-picker
              :envs="coll.info.envs"
              :model-value="coll.currentEnv"
              @update:model-value="coll.setEnv($event); tabs.refreshResolve()"
              @manage="showEnvManager = true"
            />
            <n-button size="small" @click="openCreate">{{ t('toolbar.newRequest') }}</n-button>
            <n-button size="small" @click="showFolder = true">{{ t('toolbar.newFolder') }}</n-button>
            <n-button size="small" quaternary :title="t('toolbar.reload')" @click="coll.reload()">⟳</n-button>
            <n-button size="small" quaternary :title="t('toolbar.openDir')" @click="openCollection('')">📂</n-button>
            <n-button size="small" quaternary :title="locale" @click="toggleLang">{{ locale === 'zh-CN' ? 'EN' : '中' }}</n-button>
            <n-button size="small" quaternary :title="t('history.title')" @click="showHistory = true">🕘</n-button>
            <n-button size="small" quaternary :title="t('settings.title')" @click="showSettings = true">⚙</n-button>
          </header>

          <div class="body">
            <aside v-show="sideOpen" class="side">
              <sidebar :tree="coll.tree" @open="tabs.openRequest($event)" @delete="deleteRequest" />
            </aside>
            <main class="main">
              <tab-bar
                :tabs="tabs.tabs"
                :active-key="tabs.activeKey"
                @select="tabs.setActive($event)"
                @close="tabs.close($event)"
                @new="openCreate"
              />
              <div v-if="tabs.active" class="work">
                <section class="editor-col">
                  <request-editor :tab="tabs.active" />
                </section>
                <section class="resp-col">
                  <response-panel :tab="tabs.active" />
                </section>
              </div>
              <div v-else class="placeholder muted">← {{ t('sidebar.empty') }}</div>
            </main>
          </div>
        </template>

        <welcome v-else :last-dir="lastDir" @open="openCollection" />
      </n-spin>

      <env-manager v-model:show="showEnvManager" @saved="envSaved" />
      <settings-dialog v-model:show="showSettings" />
      <history-dialog v-model:show="showHistory" @open="tabs.openRequest($event)" />

      <n-modal v-model:show="showCreate" preset="card" :title="t('prompt.newRequest')" style="width: 440px">
        <n-form label-placement="left" label-width="86">
          <n-form-item :label="t('prompt.reqName')">
            <n-input v-model:value="createForm.name" @keyup.enter="submitCreate" />
          </n-form-item>
          <n-form-item :label="t('prompt.method')">
            <n-select
              v-model:value="createForm.method"
              :options="['GET', 'POST', 'PUT', 'DELETE', 'PATCH'].map((m) => ({ label: m, value: m }))"
            />
          </n-form-item>
          <n-form-item :label="t('prompt.folder')">
            <n-select v-model:value="createForm.folder" :options="folderOptions" tag filterable />
          </n-form-item>
        </n-form>
        <template #footer>
          <div class="modal-ft">
            <n-button size="small" @click="showCreate = false">{{ t('common.cancel') }}</n-button>
            <n-button size="small" type="primary" @click="submitCreate">{{ t('common.create') }}</n-button>
          </div>
        </template>
      </n-modal>

      <n-modal v-model:show="showFolder" preset="card" :title="t('prompt.newFolder')" style="width: 380px">
        <n-input v-model:value="folderName" :placeholder="t('prompt.newFolder')" @keyup.enter="submitFolder" />
        <template #footer>
          <div class="modal-ft">
            <n-button size="small" @click="showFolder = false">{{ t('common.cancel') }}</n-button>
            <n-button size="small" type="primary" @click="submitFolder">{{ t('common.create') }}</n-button>
          </div>
        </template>
      </n-modal>
    </div>
  </n-config-provider>
</template>

<style scoped>
.app {
  height: 100%;
  display: flex;
  flex-direction: column;
}

/* n-spin 会自带一层容器，需显式撑满并沿用列布局，否则整页高度塌陷 */
.app :deep(.n-spin-container),
.app :deep(.n-spin-content) {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 12px;
  border-bottom: 1px solid var(--app-border);
  background: var(--app-panel);
}

.menu {
  border: none;
  background: none;
  font-size: 15px;
  color: var(--app-muted);
  cursor: pointer;
  padding: 2px 4px;
  border-radius: 4px;
}

.menu:hover {
  background: var(--app-row-hover);
  color: var(--app-text);
}

.brand {
  font-weight: 700;
  letter-spacing: 1px;
  font-size: 13px;
  color: var(--app-text);
}

.ws {
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
  background: var(--app-sidebar);
  color: var(--app-text);
}

.sp {
  flex: 1 1 auto;
}

.body {
  flex: 1 1 auto;
  display: flex;
  min-height: 0;
}

.side {
  width: 264px;
  flex: 0 0 auto;
  border-right: 1px solid var(--app-border);
  overflow: auto;
  background: var(--app-sidebar);
}

.main {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.work {
  flex: 1 1 auto;
  display: flex;
  min-height: 0;
}

.editor-col {
  flex: 1 1 auto;
  min-width: 420px;
  display: flex;
  flex-direction: column;
  overflow: auto;
  background: var(--app-panel);
}

.resp-col {
  flex: 0 0 42%;
  min-width: 340px;
  border-left: 1px solid var(--app-border);
  display: flex;
  flex-direction: column;
  min-height: 0;
  background: var(--app-panel);
}

.placeholder {
  padding: 40px;
  text-align: center;
}

.modal-ft {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>

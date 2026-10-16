<script setup lang="ts">
// 文档条目管理（B9 入口）：左侧列表（新建/删除）+ 右侧 DocEditor 编辑预览。
// 数据走 Go 门面 ListDocs/CreateDoc/SaveDoc/DeleteDoc（docs/*.md，frontmatter + 正文，删除进 .trash）。
import DocEditor from '@/components/DocEditor.vue'
import { api } from '@/lib/ipc'
import { message } from '@/lib/notice'
import type { DocEntry } from '@/types'
import { CloseOutline } from '@vicons/ionicons5'
import { NButton, NIcon, NInput, NModal, NPopconfirm } from 'naive-ui'
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const show = defineModel<boolean>('show', { required: true })

const { t } = useI18n()
const docs = ref<DocEntry[]>([])
const activeUid = ref('')
const creating = ref(false)
const newName = ref('')
const busy = ref(false)

async function refresh(): Promise<void> {
  try {
    docs.value = await api.listDocs()
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

watch(show, (v) => {
  if (!v) {
    creating.value = false
    newName.value = ''
    return
  }
  void refresh().then(() => {
    if (!activeUid.value && docs.value.length) activeUid.value = docs.value[0].uid
  })
})

async function create(): Promise<void> {
  const name = newName.value.trim()
  if (!name || busy.value) return
  busy.value = true
  try {
    const d = await api.createDoc(name)
    await refresh()
    activeUid.value = d.uid
    creating.value = false
    newName.value = ''
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  } finally {
    busy.value = false
  }
}

async function remove(d: DocEntry): Promise<void> {
  try {
    await api.deleteDoc(d.uid)
    if (activeUid.value === d.uid) activeUid.value = ''
    await refresh()
    message.success(t('docs.deleted'))
  } catch (e) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

function onSaved(): void {
  void refresh()
}
</script>

<template>
  <n-modal v-model:show="show" preset="card" :title="t('docs.title')" style="width: 860px; max-width: 94vw">
    <div class="wrap" data-testid="docs.dialog">
      <aside class="side">
        <div class="side-hd">
          <n-button size="tiny" type="primary" data-testid="docs.new" @click="creating = true">
            {{ t('docs.new') }}
          </n-button>
        </div>
        <div v-if="creating" class="new-row">
          <n-input v-model:value="newName" size="small" :placeholder="t('docs.namePh')" data-testid="docs.nameInput"
            @keyup.enter="create" />
          <n-button size="small" type="primary" :loading="busy" data-testid="docs.newConfirm" @click="create">
            {{ t('common.confirm') }}
          </n-button>
          <n-button size="small" quaternary @click="creating = false">{{ t('common.cancel') }}</n-button>
        </div>
        <div v-if="!docs.length && !creating" class="empty muted">{{ t('docs.empty') }}</div>
        <div v-for="d in docs" :key="d.uid" class="item" :class="{ on: d.uid === activeUid }" data-testid="docs.item"
          :data-uid="d.uid" @click="activeUid = d.uid">
          <span class="nm">{{ d.name || d.path }}</span>
          <n-popconfirm @positive-click="remove(d)">
            <template #trigger>
              <button class="del" type="button" :title="t('common.delete')" data-testid="docs.delete"
                @click.stop>
                <n-icon :component="CloseOutline" :size="13" />
              </button>
            </template>
            {{ t('docs.deleteConfirm') }}
          </n-popconfirm>
        </div>
      </aside>
      <main class="main">
        <DocEditor v-if="activeUid" :key="activeUid" :uid="activeUid" @saved="onSaved"
          @close="activeUid = ''" />
        <div v-else class="empty muted">{{ t('docs.pickHint') }}</div>
      </main>
    </div>
  </n-modal>
</template>

<style scoped>
.wrap {
  display: flex;
  gap: 0;
  height: 560px;
  max-height: 70vh;
  min-width: 0;
}

.side {
  flex: 0 0 200px;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--app-border);
  overflow-y: auto;
  padding-right: 6px;
}

.side-hd {
  display: flex;
  justify-content: flex-end;
  padding: 0 0 8px;
}

.new-row {
  display: flex;
  gap: 6px;
  align-items: center;
  padding: 0 0 8px;
}

.item {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 6px 8px;
  border-radius: 6px;
  cursor: pointer;
  font-size: 12.5px;
  color: var(--app-text);
}

.item:hover {
  background: var(--app-row-hover);
}

.item.on {
  background: var(--app-accent-tint);
  color: var(--app-accent-dark);
}

.nm {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.del {
  border: none;
  background: none;
  color: var(--app-placeholder);
  cursor: pointer;
  padding: 2px;
  display: inline-flex;
  border-radius: 4px;
}

.del:hover {
  color: var(--app-danger, #d03050);
}

.main {
  flex: 1;
  min-width: 0;
  display: flex;
}

.empty {
  padding: 18px 8px;
  text-align: center;
  font-size: 12.5px;
}
</style>

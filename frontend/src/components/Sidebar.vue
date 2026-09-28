<script setup lang="ts">
// 集合侧栏：分组（目录）+ 请求的扁平渲染（缩进表示层级），点击开 tab；支持名称搜索过滤。
import { NInput } from 'naive-ui'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import MethodTag from '@/components/MethodTag.vue'
import type { TreeNode } from '@/types'

const props = defineProps<{ tree: TreeNode[] }>()
const emit = defineEmits<{ open: [uid: string]; delete: [uid: string] }>()
const { t } = useI18n()

const query = ref('')

interface Row {
  node: TreeNode
  depth: number
}

// 搜索时保留命中的请求及其祖先分组。
function filterTree(nodes: TreeNode[], q: string): TreeNode[] {
  const out: TreeNode[] = []
  for (const n of nodes) {
    if (n.type === 'folder') {
      const kids = n.children ? filterTree(n.children, q) : []
      if (kids.length || n.name.toLowerCase().includes(q)) {
        out.push({ ...n, children: kids.length ? kids : n.children })
      }
    } else if (n.name.toLowerCase().includes(q)) {
      out.push(n)
    }
  }
  return out
}

const rows = computed<Row[]>(() => {
  const src = query.value.trim() ? filterTree(props.tree, query.value.trim().toLowerCase()) : props.tree
  const out: Row[] = []
  const walk = (nodes: TreeNode[], depth: number): void => {
    for (const n of nodes) {
      out.push({ node: n, depth })
      if (n.type === 'folder' && n.children?.length) walk(n.children, depth + 1)
    }
  }
  walk(src, 0)
  return out
})
</script>

<template>
  <div class="sidebar">
    <div class="head">
      <span class="title">{{ t('sidebar.collections') }}</span>
    </div>
    <n-input
      v-model:value="query"
      size="small"
      clearable
      class="search"
      :placeholder="t('sidebar.search')"
    />
    <div v-if="!rows.length" class="empty muted">{{ t('sidebar.empty') }}</div>
    <div
      v-for="row in rows"
      :key="row.node.type + row.node.path"
      class="row"
      :class="{ folder: row.node.type === 'folder' }"
      :style="{ paddingLeft: 10 + row.depth * 14 + 'px' }"
    >
      <template v-if="row.node.type === 'folder'">
        <span class="ficon">▾</span>
        <span class="fname">{{ row.node.name }}</span>
      </template>
      <template v-else>
        <method-tag :method="row.node.method ?? 'GET'" />
        <button class="rname" :title="row.node.name" @click="emit('open', row.node.uid)">
          {{ row.node.name }}
        </button>
        <span
          class="del"
          :title="t('sidebar.deleteHint')"
          @click.stop="emit('delete', row.node.uid)"
        >✕</span>
      </template>
    </div>
  </div>
</template>

<style scoped>
.sidebar {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: auto;
  padding-bottom: 10px;
}

.head {
  padding: 10px 12px 6px;
}

.title {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 1px;
  text-transform: uppercase;
  color: var(--app-muted);
}

.search {
  margin: 0 10px 8px;
  width: calc(100% - 20px);
}

.empty {
  padding: 16px 12px;
  font-size: 12px;
}

.row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 5px 10px;
  min-height: 28px;
  cursor: pointer;
}

.row:hover {
  background: var(--app-row-hover);
}

.row.folder {
  cursor: default;
}

.ficon {
  color: var(--app-muted);
  font-size: 10px;
  width: 12px;
}

.fname {
  font-weight: 600;
  font-size: 12.5px;
  color: var(--app-text);
}

.rname {
  border: none;
  background: none;
  padding: 0;
  font-size: 12.5px;
  color: #444;
  cursor: pointer;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-align: left;
  flex: 1 1 auto;
}

.rname:hover {
  color: var(--app-accent);
}

.del {
  visibility: hidden;
  color: var(--app-muted);
  padding: 0 3px;
  border-radius: 3px;
  cursor: pointer;
  flex: 0 0 auto;
}

.row:hover .del {
  visibility: visible;
}

.del:hover {
  color: #d03050;
  background: #fdecec;
}
</style>

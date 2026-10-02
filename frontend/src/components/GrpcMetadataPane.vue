<script setup lang="ts">
// gRPC Metadata 分段（设计文档 G6）：直接复用键值表（名称 / 值 / 启用 / 说明 / 批量编辑）。
// 变量由执行器逐行替换（`runner.resolveKV`），敏感值掩码沿用 VarInput 那套环境变量标记。
import KeyValueTable from '@/components/KeyValueTable.vue'
import { watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { grpcOf } from '@/lib/grpc'
import { useTabsStore } from '@/stores/tabs'
import type { Tab } from '@/stores/tabs'

const props = defineProps<{ tab: Tab }>()
const tabs = useTabsStore()
const { t } = useI18n()

/** metadata 在磁盘上是可省略段（Go omitempty）：渲染前补齐成数组，表格才能就地增删行。 */
function ensureRows(): void {
  const g = props.tab.request.grpc
  if (!g) return
  if (!Array.isArray(g.metadata)) g.metadata = []
}

ensureRows()
watch(() => props.tab.key, ensureRows)

function touch(): void {
  tabs.touch(props.tab.key)
}
</script>

<template>
  <div class="md-pane">
    <p class="hint">{{ t('grpc.metadataHint') }}</p>
    <key-value-table :rows="grpcOf(tab.request).metadata ?? []" :label="t('grpc.metadata')" @change="touch" />
  </div>
</template>

<style scoped>
.md-pane {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 10px 12px;
  overflow: auto;
}

.hint {
  margin: 0;
  font-size: 11.5px;
  color: var(--app-muted);
}
</style>

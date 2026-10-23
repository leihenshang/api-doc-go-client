<script setup lang="ts">
// 「响应字段」分段的字段表：字段路径 / 类型 / 值（均只读）/ 含义（唯一可编辑列）。
// 与请求体表格同构：字段由上方「响应数据」实时解析生成（表格只读，不加/删字段），
// 只有「含义」可写回 RespDoc（description），随文件落盘、随同步往返 Web 端、并对 MCP 可见。
import type { FieldRow } from '@/lib/responseFields'
import { NInput } from 'naive-ui'
import { useI18n } from 'vue-i18n'

defineProps<{ rows: FieldRow[] }>()
const emit = defineEmits<{
  meaning: [path: string, value: string]
}>()
const { t } = useI18n()
</script>

<template>
  <section v-if="rows.length" class="fields">
    <div class="fh">
      <span class="ft">{{ t('resp.fields') }}<span class="num">{{ rows.length }}</span></span>
      <span class="fd">{{ t('resp.fieldsHint') }}</span>
    </div>
    <div class="fields-scroll">
      <table class="ftable">
        <colgroup>
          <col class="col-path" />
          <col class="col-type" />
          <col class="col-value" />
          <col class="col-meaning" />
        </colgroup>
        <thead>
          <tr>
            <th>{{ t('resp.field') }}</th>
            <th>{{ t('resp.type') }}</th>
            <th>{{ t('resp.example') }}</th>
            <th>{{ t('resp.meaning') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="f in rows" :key="f.path" data-testid="resp.fields.row" :data-path="f.path">
            <td class="fp" data-testid="resp.fields.path" :title="f.path">{{ f.path }}</td>
            <td>
              <span v-if="f.type" class="ftype">{{ f.type }}</span>
              <span v-else class="empty-cell">—</span>
            </td>
            <td class="fval" data-testid="resp.fields.value" :title="f.value">
              {{ f.value || '—' }}
            </td>
            <td class="fmeaning">
              <n-input :value="f.meaning" size="small" data-testid="resp.fields.meaning"
                :placeholder="t('common.optional')" @update:value="emit('meaning', f.path, $event)" />
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>

  <p v-else class="fields-empty">{{ t('resp.fieldsEmpty') }}</p>
</template>

<style scoped>
.fields {
  display: flex;
  flex-direction: column;
  min-height: 0;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  overflow: hidden;
}

.fh {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 8px 12px;
  border-bottom: 1px solid var(--app-border);
}

.ft {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--app-text);
}

.ft .num {
  margin-left: 6px;
  padding: 0 6px;
  border-radius: 999px;
  background: var(--app-chip);
  font-size: 11px;
  font-weight: 500;
  color: var(--app-muted);
}

.fd {
  font-size: 11.5px;
  color: var(--app-muted);
}

.fields-scroll {
  overflow: auto;
}

.ftable {
  width: 100%;
  min-width: 680px;
  table-layout: fixed;
  border-collapse: collapse;
  font-size: 12px;
}

.col-path {
  width: 38%;
}

.col-type {
  width: 104px;
}

.col-value {
  width: 26%;
}

.col-meaning {
  width: 28%;
}

.ftable th,
.ftable td {
  padding: 6px 10px;
  border-bottom: 1px solid var(--app-border);
  text-align: left;
  vertical-align: middle;
}

.ftable th {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--app-surface-2);
  color: var(--app-muted);
  font-size: 11.5px;
  font-weight: 500;
}

.ftable tbody tr:nth-child(even) {
  background: var(--app-bg);
}

.ftable tbody tr:hover {
  background: var(--app-row-hover);
}

.ftable tbody tr:last-child td {
  border-bottom: none;
}

.fp {
  font-family: var(--app-mono);
  color: var(--app-code-key);
  word-break: break-all;
}

.ftype {
  display: inline-block;
  padding: 1px 6px;
  border-radius: 4px;
  background: var(--app-chip);
  color: var(--app-text-2);
  font-family: var(--app-mono);
  font-size: 10.5px;
  line-height: 16px;
}

.fval {
  color: var(--app-muted);
  white-space: pre-wrap;
  word-break: break-all;
}

.empty-cell {
  color: var(--app-placeholder);
}

.fmeaning :deep(.n-input) {
  width: 100%;
}

.fields-empty {
  margin: 12px 0 0;
  font-size: 12.5px;
  color: var(--app-placeholder);
}
</style>

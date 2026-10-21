<script setup lang="ts">
// 「响应字段」分段的字段表：字段路径 / 类型 / 值（均只读）/ 含义（唯一可编辑列）。
// 与请求体表格同构：字段由上方「响应数据」实时解析生成（表格只读，不加/删字段），
// 只有「含义」可写回 RespDoc（description），随文件落盘、随同步往返 Web 端、并对 MCP 可见。
import type { FieldRow } from '@/lib/responseFields';
import { NInput } from 'naive-ui';
import { useI18n } from 'vue-i18n';

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
    <div class="tr th">
      <span>{{ t('resp.field') }}</span>
      <span>{{ t('resp.type') }}</span>
      <span>{{ t('resp.example') }}</span>
      <span>{{ t('resp.meaning') }}</span>
    </div>
    <div v-for="f in rows" :key="f.path" class="tr" data-testid="resp.fields.row" :data-path="f.path">
      <span class="mono fp" data-testid="resp.fields.path" :title="f.path">{{ f.path }}</span>
      <span class="mono ftype">{{ f.type }}</span>
      <span class="mono fval" data-testid="resp.fields.value" :title="f.value">{{ f.value }}</span>
      <n-input :value="f.meaning" size="small" data-testid="resp.fields.meaning" :placeholder="t('common.optional')"
        @update:value="emit('meaning', f.path, $event)" />
    </div>
  </section>

  <p v-else class="fields-empty">{{ t('resp.fieldsEmpty') }}</p>
</template>

<style scoped>
.fields {
  border: 1px solid var(--app-border);
  border-radius: 6px;
}

.fh {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px 6px;
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

.tr {
  display: grid;
  grid-template-columns: minmax(0, 1.4fr) 80px minmax(0, 1fr) minmax(0, 1.4fr);
  align-items: center;
  gap: 8px;
  padding: 5px 8px;
  border-top: 1px solid var(--app-border);
  font-size: 12px;
}

.tr.th {
  background: var(--app-surface-2);
  color: var(--app-muted);
  font-size: 11.5px;
  border-top: none;
}

.fp {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--app-code-key);
}

.ftype,
.fval {
  color: var(--app-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fields-empty {
  margin: 12px 0 0;
  font-size: 12.5px;
  color: var(--app-placeholder);
}
</style>

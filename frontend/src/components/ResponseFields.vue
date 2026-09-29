<script setup lang="ts">
// 「响应字段」页签的表格：字段 / 类型 / 含义。
// 字段来自用户点击「更新响应字段」后的解析结果（由 ResponsePanel 持有并落 localStorage），
// 本组件只负责展示与编辑含义，不自行解析响应体。
import { NInput } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import type { FieldRow } from '@/lib/responseFields'

defineProps<{ rows: FieldRow[] }>()
const emit = defineEmits<{ meaning: [path: string, value: string] }>()
const { t } = useI18n()
</script>

<template>
  <section v-if="rows.length" class="fields">
    <div class="fh">
      <span class="ft">{{ t('resp.fields') }}</span>
      <span class="fd">{{ t('resp.fieldsHint') }}</span>
    </div>
    <div class="tr th">
      <span>{{ t('resp.field') }}</span>
      <span>{{ t('resp.type') }}</span>
      <span>{{ t('resp.meaning') }}</span>
    </div>
    <div v-for="f in rows" :key="f.path" class="tr" data-testid="resp.fields.row" :data-path="f.path">
      <span class="mono fp" data-testid="resp.fields.path" :title="f.path">{{ f.path }}</span>
      <span class="mono ftype">{{ f.type }}</span>
      <n-input
        :value="f.meaning"
        size="small"
        data-testid="resp.fields.meaning"
        :placeholder="t('common.optional')"
        @update:value="emit('meaning', f.path, $event)"
      />
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
  align-items: baseline;
  gap: 8px;
  padding: 8px 10px 6px;
}

.ft {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--app-text);
}

.fd {
  font-size: 11.5px;
  color: var(--app-muted);
}

.tr {
  display: grid;
  grid-template-columns: minmax(0, 1.2fr) 90px minmax(0, 1.6fr);
  align-items: center;
  gap: 10px;
  padding: 5px 8px;
  border-top: 1px solid var(--app-border);
  font-size: 12px;
}

.tr.th {
  background: var(--app-surface-2);
  color: var(--app-muted);
  font-size: 11.5px;
}

.fp {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--app-code-key);
}

.ftype {
  color: var(--app-muted);
}

.fields-empty {
  margin: 12px 0 0;
  font-size: 12.5px;
  color: var(--app-placeholder);
}
</style>

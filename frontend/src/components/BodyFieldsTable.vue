<script setup lang="ts">
// 「请求体表格」视图：JSON 体按字段路径展平为四列（路径 / 类型 / 值只读 / 备注可编辑）。
// 值列只读（值由 JSON 原文决定），本组件只负责展示与编辑备注；备注写回由父级落到 body.descs。
import type { BodyFieldRow } from '@/lib/bodyFields';
import { NInput } from 'naive-ui';
import { useI18n } from 'vue-i18n';

defineProps<{ rows: BodyFieldRow[] }>()
const emit = defineEmits<{ updateDesc: [path: string, value: string] }>()
const { t } = useI18n()
</script>

<template>
  <section v-if="rows.length" class="bfields">
    <div class="fh">
      <span class="ft">{{ t('body.title') }}<span class="num">{{ rows.length }}</span></span>
      <span class="fd">{{ t('body.hint') }}</span>
    </div>
    <div class="tr th">
      <span>{{ t('body.colPath') }}</span>
      <span>{{ t('body.colType') }}</span>
      <span>{{ t('body.colValue') }}</span>
      <span>{{ t('body.colDesc') }}</span>
    </div>
    <div v-for="f in rows" :key="f.path" class="tr" data-testid="body.fieldsRow" :data-path="f.path">
      <span class="mono fp" :title="f.path">{{ f.path }}</span>
      <span class="mono ftype">{{ f.type }}</span>
      <span class="mono fval" :title="f.value">{{ f.value }}</span>
      <n-input :value="f.desc" size="small" data-testid="body.fieldDesc" :placeholder="t('common.optional')"
        @update:value="emit('updateDesc', f.path, $event)" />
    </div>
  </section>

  <p v-else class="bfields-empty">{{ t('body.empty') }}</p>
</template>

<style scoped>
.bfields {
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
  grid-template-columns: minmax(0, 1.3fr) 68px minmax(0, 1fr) minmax(0, 1.4fr);
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

.bfields-empty {
  margin: 12px 0 0;
  font-size: 12.5px;
  color: var(--app-placeholder);
}
</style>

<script setup lang="ts">
// 「请求体字段」表格视图：5 列可编辑（字段名 / 类型 / 必填 / 示例值 / 说明）+ 新增行 / 删除行。
//
// 表格真源是 body.fields（由「生成数据表格」派生或用户手工增行），不再从 JSON 自动派生。
// 所有修改只往父级 emit 补丁，由父级写回 body.fields 并标脏 —— 本组件不持有状态。
import type { BodyField } from '@/types';
import { NInput, NSelect, NCheckbox, NButton, NIcon } from 'naive-ui';
import { useI18n } from 'vue-i18n';

defineProps<{ rows: BodyField[] }>()
const emit = defineEmits<{
  update: [index: number, patch: Partial<BodyField>]
  add: []
  remove: [index: number]
}>()
const { t } = useI18n()

/** 类型下拉选项：JSON 类型名（也允许手输，故 select 可过滤 + 可清空）。 */
const TYPE_OPTIONS = ['string', 'integer', 'number', 'boolean', 'array', 'object', 'null']
</script>

<template>
  <section class="bfields">
    <div class="fh">
      <span class="ft">{{ t('body.title') }}<span class="num">{{ rows.length }}</span></span>
      <span class="fd">{{ t('body.hint') }}</span>
    </div>

    <template v-if="rows.length">
      <div class="tr th">
        <span>{{ t('body.colName') }}</span>
        <span>{{ t('body.colType') }}</span>
        <span class="c">{{ t('body.colRequired') }}</span>
        <span>{{ t('body.colExample') }}</span>
        <span>{{ t('body.colDesc') }}</span>
        <span></span>
      </div>

      <div v-for="(f, i) in rows" :key="i" class="tr" data-testid="body.fieldsRow" :data-path="f.name">
        <n-input :value="f.name" size="small" data-testid="body.fieldName" :placeholder="t('body.namePh')"
          @update:value="emit('update', i, { name: $event })" />
        <n-select :value="f.type || null" size="small" filterable clearable tag
          :options="TYPE_OPTIONS.map((v) => ({ label: v, value: v }))" data-testid="body.fieldType"
          :placeholder="t('body.colType')" @update:value="emit('update', i, { type: $event ?? '' })" />
        <span class="c">
          <n-checkbox :checked="f.required" data-testid="body.fieldRequired"
            @update:checked="emit('update', i, { required: $event })" />
        </span>
        <n-input :value="f.example" size="small" data-testid="body.fieldExample" :placeholder="t('body.colExample')"
          @update:value="emit('update', i, { example: $event })" />
        <n-input :value="f.description" size="small" data-testid="body.fieldDesc" :placeholder="t('common.optional')"
          @update:value="emit('update', i, { description: $event })" />
        <n-button size="tiny" quaternary type="error" data-testid="body.fieldRemove" :title="t('body.removeRow')"
          @click="emit('remove', i)">
          <template #icon>
            <n-icon><svg viewBox="0 0 24 24" width="14" height="14"><path fill="currentColor"
              d="M9 3h6l1 2h4v2H4V5h4l1-2Zm-3 6h12l-1 12H7L6 9Z" /></svg></n-icon>
          </template>
        </n-button>
      </div>
    </template>

    <!-- 空态：表格不再从 JSON 自动派生，只有点「生成数据表格」或手工「新增行」才有内容 -->
    <p v-else class="bfields-empty">{{ t('body.empty') }}</p>

    <div class="tf">
      <n-button size="tiny" data-testid="body.fieldAdd" @click="emit('add')">+ {{ t('body.addRow') }}</n-button>
    </div>
  </section>
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
  grid-template-columns: minmax(0, 1.3fr) 108px 40px minmax(0, 1fr) minmax(0, 1.4fr) 32px;
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

.c {
  display: flex;
  justify-content: center;
}

.bfields-empty {
  margin: 0;
  padding: 8px 10px;
  border-top: 1px solid var(--app-border);
  font-size: 12px;
  color: var(--app-placeholder);
}

.tf {
  display: flex;
  justify-content: flex-start;
  padding: 6px 8px;
  border-top: 1px solid var(--app-border);
}
</style>

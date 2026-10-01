<script setup lang="ts">
// 「保存 ▾」下拉（response-panel design-spec §4）：
// 三个保存菜单项 + 分隔线后「作用域」chips（集合/环境/全局，单选）。
// 第 4 项「保存响应示例」为既有功能（E23）保留项，见 doc 的「有意差异」。
import { NIcon, NPopover } from 'naive-ui'
import { CaretDownOutline } from '@vicons/ionicons5'
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { VarScope } from '@/lib/saveVars'

const props = defineProps<{ scope: VarScope; canSaveExample: boolean }>()
const emit = defineEmits<{
  'update:scope': [v: VarScope]
  'save-file': []
  'save-all-vars': []
  'save-selected-var': []
  'save-example': []
}>()
const { t } = useI18n()

const open = ref(false)

/** 浮层规格对齐 response-panel design-spec §4：240 宽、圆角 8、边框 + 投影（颜色走主题令牌，暗色同样适配）。 */
const menuStyle = {
  width: '240px',
  padding: '8px 0',
  borderRadius: '8px',
  border: '1px solid var(--app-border)',
  boxShadow: 'var(--app-shadow-pop)',
}

const scopes: { key: VarScope; label: string }[] = [
  { key: 'collection', label: t('resp.scopeCollection') },
  { key: 'env', label: t('resp.scopeEnv') },
  { key: 'global', label: t('resp.scopeGlobal') },
]

function pickScope(v: VarScope): void {
  emit('update:scope', v)
}

function run(action: 'save-file' | 'save-all-vars' | 'save-selected-var' | 'save-example'): void {
  open.value = false
  if (action === 'save-file') emit('save-file')
  else if (action === 'save-all-vars') emit('save-all-vars')
  else if (action === 'save-selected-var') emit('save-selected-var')
  else emit('save-example')
}
</script>

<template>
  <!-- trigger 与 default 槽都必须恰好一个子节点：naive 的 getFirstSlotVNode 会抛错并中断渲染 -->
  <n-popover
    v-model:show="open"
    trigger="click"
    placement="bottom-end"
    :show-arrow="false"
    :content-style="menuStyle"
  >
    <template #trigger>
      <button class="save-btn" type="button" data-testid="resp.save">
        {{ t('common.save') }}
        <n-icon :component="CaretDownOutline" :size="12" />
      </button>
    </template>
    <div class="menu">
      <button class="mi" type="button" data-testid="resp.saveFile" @click="run('save-file')">
        {{ t('resp.saveFile') }}
      </button>
      <button class="mi" type="button" data-testid="resp.saveAllVars" @click="run('save-all-vars')">
        {{ t('resp.saveAllVars') }}
      </button>
      <button class="mi" type="button" data-testid="resp.saveSelectedVar" @click="run('save-selected-var')">
        {{ t('resp.saveSelectedVar') }}
      </button>
      <button v-if="props.canSaveExample" class="mi" type="button" data-testid="resp.saveExample" @click="run('save-example')">
        {{ t('resp.saveExample') }}
      </button>
      <div class="sep" />
      <div class="scope-row">
        <span class="scope-label">{{ t('resp.scope') }}</span>
        <button
          v-for="s in scopes"
          :key="s.key"
          class="chip"
          :class="{ on: props.scope === s.key }"
          type="button"
          data-testid="resp.scope"
          :data-scope="s.key"
          @click="pickScope(s.key)"
        >
          {{ s.label }}
        </button>
      </div>
    </div>
  </n-popover>
</template>

<style scoped>
/* ⑦ 保存▾：工具条上唯一的主按钮（实底白字圆角 6） */
.save-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border: none;
  background: var(--app-accent);
  color: var(--app-on-accent);
  border-radius: 6px;
  font-size: 12px;
  font-family: inherit;
  font-weight: 600;
  padding: 5px 10px;
  cursor: pointer;
  white-space: nowrap;
  flex: 0 0 auto;
}

.save-btn:hover {
  background: var(--app-send-hover);
}

.menu {
  display: flex;
  flex-direction: column;
}

.mi {
  display: flex;
  align-items: center;
  height: 34px;
  padding: 0 12px;
  border: none;
  background: none;
  font-family: inherit;
  font-size: 12px;
  color: var(--app-text);
  cursor: pointer;
  text-align: left;
  white-space: nowrap;
}

.mi:hover {
  background: var(--app-row-hover);
}

.sep {
  height: 1px;
  background: var(--app-border);
  margin: 8px 0;
}

.scope-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 12px;
}

.scope-label {
  font-size: 11px;
  color: var(--app-muted);
  flex: 0 0 auto;
}

.chip {
  border: none;
  background: var(--app-chip);
  border-radius: 4px;
  padding: 3px 10px;
  font-size: 11px;
  font-family: inherit;
  color: var(--app-text-2);
  cursor: pointer;
  flex: 0 0 auto;
}

.chip.on {
  background: var(--app-accent-tint);
  color: var(--app-accent-dark);
  font-weight: 600;
}

.chip:hover {
  background: var(--app-chip-hover);
}

.chip.on:hover {
  background: var(--app-accent-tint);
}
</style>

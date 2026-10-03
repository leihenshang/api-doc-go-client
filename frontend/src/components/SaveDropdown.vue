<script setup lang="ts">
// 「保存 ▾」下拉（response-panel design-spec §4）：只保留两个落盘动作 ——
// ① 保存响应示例（E23，集合内 examples/*.yml，可 diff 可入库）
// ② 保存响应体为文件（.json/.txt/.bin，落到用户选的位置）
// 「保存全部字段为变量 / 保存选中值为变量」与分隔线后的「作用域」chips 已下线（变量入口收敛到
// 字段表的「变量书签」，落点固定为当前环境），详见 doc 的「有意差异」。
import { NIcon, NPopover } from 'naive-ui'
import { BookmarkOutline, CaretDownOutline, DocumentOutline } from '@vicons/ionicons5'
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ canSaveExample: boolean }>()
const emit = defineEmits<{
  'save-file': []
  'save-example': []
}>()
const { t } = useI18n()

const open = ref(false)

/** 浮层宽度随内容（不再撑成 240px 的大盒子）；圆角/投影/底色由 App.vue 的 Popover 主题覆盖给到外层盒子，
 *  这里只管内容宽度 —— 否则会给 naive 的两层盒子都描边，看起来像"框中框"。 */
const menuStyle = {
  minWidth: '168px',
  maxWidth: '260px',
}

function run(action: 'save-file' | 'save-example'): void {
  open.value = false
  if (action === 'save-file') emit('save-file')
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
      <!-- 正在回看示例时不再提供「保存为示例」（避免快照套快照），此时菜单里只剩存文件一项 -->
      <button v-if="props.canSaveExample" class="mi" type="button" data-testid="resp.saveExample" @click="run('save-example')">
        <n-icon :component="BookmarkOutline" :size="14" />
        <span>{{ t('resp.saveExample') }}</span>
      </button>
      <button class="mi" type="button" data-testid="resp.saveFile" @click="run('save-file')">
        <n-icon :component="DocumentOutline" :size="14" />
        <span>{{ t('resp.saveFile') }}</span>
      </button>
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

/* 菜单项：图标 + 文字，hover 才是底色（常态无框，与工具条其它幽灵按钮同一量级） */
.mi {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 30px;
  padding: 0 10px;
  border: none;
  border-radius: 6px;
  background: none;
  font-family: inherit;
  font-size: 12px;
  color: var(--app-text-2);
  cursor: pointer;
  text-align: left;
  white-space: nowrap;
}

.mi:hover {
  background: var(--app-row-hover);
  color: var(--app-text);
}
</style>

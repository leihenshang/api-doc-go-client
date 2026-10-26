<script setup lang="ts">
// Sidebar 的三个确认/创建弹窗（关闭工作目录 / 删除非空分组 / 创建集合）。
// 状态与业务动作仍由 Sidebar 持有，本组件只负责渲染与把确认事件抛回去。
import { NCheckbox, NInput, NModal } from 'naive-ui'
import { useI18n } from 'vue-i18n'

defineProps<{
  closeTargetName: string
  deleteTargetName: string
  deleteChildCount: number
}>()

const closeShow = defineModel<boolean>('closeShow', { default: false })
const deleteShow = defineModel<boolean>('deleteShow', { default: false })
const deleteAgreed = defineModel<boolean>('deleteAgreed', { default: false })
const createShow = defineModel<boolean>('createShow', { default: false })
const createName = defineModel<string>('createName', { default: '' })

const emit = defineEmits<{
  'confirm-close': []
  'confirm-delete': []
  'confirm-create': []
}>()
const { t } = useI18n()
</script>

<template>
  <!-- 关闭工作目录：居中确认（item 11），确认后由 Sidebar 通知 App 做页签/存储清理 -->
  <n-modal v-model:show="closeShow" preset="dialog" type="warning" :title="t('tree.closeDir')"
    :content="closeTargetName ? t('tree.confirmCloseDir', { name: closeTargetName }) : ''"
    :positive-text="t('common.confirm')" :negative-text="t('common.cancel')" @positive-click="emit('confirm-close')"
    @negative-click="closeShow = false" @close="closeShow = false" @esc="closeShow = false" />

  <!-- 删除非空分组：必须勾选「删除所有子项」；整棵移入 .trash，可在回收站恢复 -->
  <n-modal v-model:show="deleteShow" preset="dialog" type="warning" :title="t('tree.delDir')"
    :positive-text="t('common.delete')" :negative-text="t('common.cancel')"
    :positive-button-props="{ disabled: !deleteAgreed }" @positive-click="emit('confirm-delete')"
    @negative-click="deleteShow = false" @close="deleteShow = false" @esc="deleteShow = false">
    <p class="del-text">{{ deleteTargetName ? t('tree.confirmDelFolder', { name: deleteTargetName }) : '' }}</p>
    <n-checkbox v-model:checked="deleteAgreed" data-testid="tree.deleteChildren">
      {{ t('tree.deleteChildrenConfirm', { n: deleteChildCount }) }}
    </n-checkbox>
  </n-modal>

  <!-- 创建集合：名称即目录名；确认后由 Sidebar 通知 App 走统一 openCollection 流程 -->
  <n-modal v-model:show="createShow" preset="dialog" type="info" :title="t('sidebar.createTitle')"
    :positive-text="t('common.create')" :negative-text="t('common.cancel')" @positive-click="emit('confirm-create')"
    @negative-click="createShow = false" @close="createShow = false" @esc="createShow = false">
    <div class="cform">
      <span class="clb">{{ t('sidebar.createName') }}</span>
      <n-input v-model:value="createName" size="small" :placeholder="t('sidebar.createNamePh')" autofocus
        @keyup.enter="emit('confirm-create')" />
    </div>
  </n-modal>
</template>

<style scoped>
/* 删除非空分组的危险确认文案 */
.del-text {
  margin: 0 0 10px;
  font-size: 12.5px;
  color: var(--app-text-2);
}

/* 创建集合弹窗：标签 + 名称输入一行排布 */
.cform {
  display: flex;
  align-items: center;
  gap: 10px;
}

.clb {
  flex: 0 0 auto;
  font-size: 12.5px;
  color: var(--app-text-2);
}
</style>

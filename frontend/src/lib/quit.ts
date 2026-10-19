// 退出前的未保存改动守卫：标题栏关闭按钮与 Alt+F4/系统关闭都会走这里。
// 手动保存模式下若还有未保存改动，先弹「保存并退出 / 退出不保存 / 取消」；
// 自动保存模式或无未保存改动直接退出。
import { i18n } from '@/i18n'
import { windowCtl } from '@/lib/ipc'
import { dialog } from '@/lib/notice'
import { useSettingsStore } from '@/stores/settings'
import { useTabsStore } from '@/stores/tabs'

export function requestQuit(): void {
  const settings = useSettingsStore()
  // 任务栏驻留：关闭窗口 = 隐藏到托盘，不走未保存确认（数据仍在，托盘可再唤出）
  if (settings.form.trayResident) {
    windowCtl.hideToTray()
    return
  }
  const quitNow = (): void => void windowCtl.quit()
  if (settings.autoSave) {
    quitNow()
    return
  }
  const tabs = useTabsStore()
  const savable = tabs.savableTabs
  if (!savable.length) {
    quitNow()
    return
  }
  const d = dialog.warning({
    title: i18n.global.t('confirm.quitTitle'),
    content: i18n.global.t('confirm.quitUnsaved', { n: savable.length }),
    positiveText: i18n.global.t('common.saveAndQuit'),
    negativeText: i18n.global.t('common.quitWithoutSaving'),
    onPositiveClick: () => {
      // 先落盘再退出；退出后对话框随之销毁，无需处理返回值
      void (async () => {
        await useTabsStore().flushAll()
        quitNow()
      })()
    },
    onNegativeClick: () => quitNow(),
  })
  void d
}
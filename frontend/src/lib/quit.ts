// 关闭应用前的两层守卫：标题栏关闭按钮与 Alt+F4 / 系统关闭都会走这里。
//
//   1) 驻留层 —— 开启「驻留托盘」后，按 trayCloseAction 决定关窗是隐藏还是退出：
//      ask（默认）= 弹框问「放入托盘 / 直接关闭」，勾「不再显示」会把选择写回设置；
//      tray = 直接隐藏；quit = 直接退出。未开启驻留时本层跳过。
//   2) 未保存层 —— 真要退出时，手动保存模式下若还有未保存改动，
//      先弹「保存并退出 / 退出不保存 / 取消」；自动保存模式或无改动直接退出。
import { i18n } from '@/i18n'
import { windowCtl } from '@/lib/ipc'
import { dialog } from '@/lib/notice'
import { useSettingsStore } from '@/stores/settings'
import { useTabsStore } from '@/stores/tabs'
import { NCheckbox } from 'naive-ui'
import { h, ref } from 'vue'

export function requestQuit(): void {
  const settings = useSettingsStore()
  // 关闭行为由单一「关闭窗口时」决定（是否常驻托盘由 Go 侧据此派生）：
  //   ask = 弹框问「放入托盘 / 直接退出」；tray = 直接隐藏到托盘；quit = 直接退出。
  const action = settings.form.trayCloseAction
  if (action === 'tray') {
    windowCtl.hideToTray()
    return
  }
  if (action === 'ask') {
    askTrayOrQuit()
    return
  }
  quitWithUnsavedGuard()
}

/**
 * 首次关闭的询问框：「放入托盘 / 直接关闭」+「不再显示」勾选。
 * 只有勾了才把选择写进 trayCloseAction；不勾则保持 ask，下次关闭继续问。
 */
function askTrayOrQuit(): void {
  const settings = useSettingsStore()
  const remember = ref(false)
  const persist = async (action: 'tray' | 'quit'): Promise<void> => {
    if (!remember.value) return
    // 选「放入托盘」→ action=tray（Go 据此常驻）；选「直接关闭」→ action=quit（不常驻）。
    // trayResident 同步写一份，与 SettingsDialog 口径一致（ask/tray 都常驻，仅 quit 不常驻；
    // Go 侧实际由 trayResidentOf(action) 派生，本字段仅保留兼容）。
    // 审核 H#3：必须等待落盘——紧随的 quit 不等它会写配置前就退出，勾选随机丢失。
    try {
      await settings.save({
        ...settings.form,
        trayCloseAction: action,
        trayResident: action !== 'quit',
      })
    } catch {
      // 落盘失败不阻断关闭流程；下次启动继续 ask
    }
  }
  dialog.warning({
    title: i18n.global.t('confirm.trayCloseTitle'),
    // 用渲染函数：对话框的 content 只接受字符串或 VNode，勾选框得自己拼。
    // 离散 API 渲染在组件树之外，scoped 样式够不着，因此布局写内联样式。
    content: () =>
      h('div', { style: 'display:flex;flex-direction:column;gap:12px' }, [
        h('span', i18n.global.t('confirm.trayCloseBody')),
        h(
          NCheckbox,
          {
            checked: remember.value,
            'onUpdate:checked': (v: boolean) => {
              remember.value = v
            },
          },
          { default: () => i18n.global.t('confirm.trayCloseRemember') },
        ),
      ]),
    positiveText: i18n.global.t('common.toTray'),
    negativeText: i18n.global.t('common.closeDirectly'),
    onPositiveClick: () => {
      void persist('tray').then(() => windowCtl.hideToTray())
    },
    onNegativeClick: () => {
      // 先等选择落盘再走退出守卫，避免「不再显示」随进程退出丢写。
      void persist('quit').then(() => quitWithUnsavedGuard())
    },
  })
}

/** 退出流程：手动保存模式下先处理未保存改动，自动保存或无改动直接退出。 */
function quitWithUnsavedGuard(): void {
  const settings = useSettingsStore()
  const quitNow = (): void => void windowCtl.quit()
  if (settings.autoSave) {
    quitNow()
    return
  }
  const savable = useTabsStore().savableTabs
  if (!savable.length) {
    quitNow()
    return
  }
  dialog.warning({
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
}

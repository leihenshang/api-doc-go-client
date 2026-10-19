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
  if (settings.form.trayResident) {
    const action = settings.form.trayCloseAction
    if (action === 'tray') {
      windowCtl.hideToTray()
      return
    }
    if (action === 'ask') {
      askTrayOrQuit()
      return
    }
    // 'quit'：不隐藏，落到下面的退出流程
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
  const persist = (action: 'tray' | 'quit'): void => {
    if (!remember.value) return
    // 异步落盘即可：紧接着的隐藏/退出都不依赖它完成
    void settings.save({ ...settings.form, trayCloseAction: action })
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
      persist('tray')
      windowCtl.hideToTray()
    },
    onNegativeClick: () => {
      persist('quit')
      quitWithUnsavedGuard()
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

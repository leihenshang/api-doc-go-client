// 页签关闭流程（从 App.vue 抽出）：草稿保存框、脏页签询问、批量关闭与 Ctrl+S 保存。
// 规则：
//   - 草稿（未落盘）关闭时弹保存框；Ctrl+S 只保存、不关页签；
//   - 已落盘脏页签关闭前依次询问「保存 / 不保存 / 取消」，取消即中止整批；
//   - 批量关闭时草稿统一累计，最后一次性确认「全部丢弃」。
import { dialog, message } from '@/lib/notice'
import type { Tab } from '@/stores/tabs'
import { useTabsStore } from '@/stores/tabs'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

/** CloseFlowCtx：App 侧提供的渲染过渡包装与根目录 sentinel。 */
export interface CloseFlowCtx {
  /** 关闭是「移除」：跳过详情区淡入淡出（App 的 withoutDetailTransition） */
  withoutDetailTransition: <T>(fn: () => T | Promise<T>) => Promise<T>
  /** n-tree-select 的根目录 sentinel（落盘前转回空串） */
  rootKey: string
}

type CloseChoice = 'save' | 'discard' | 'cancel'

export function useCloseFlow(ctx: CloseFlowCtx) {
  const tabs = useTabsStore()
  const { t } = useI18n()

  // ---- 草稿保存框：关闭标签时问「保存（名称 + 分组） / 不保存 / 取消」；Ctrl+S 只保存、不关标签 ----
  const draftTab = ref<Tab | null>(null)
  /** true = 由 Ctrl+S 打开：保存后留在原地，「不保存」按钮不出现 */
  const draftSaveOnly = ref(false)
  const draftForm = ref({ name: '', folder: '' })
  const savingDraft = ref(false)

  /** 保存对话框的名称预填：已有名字优先，gRPC 用「方法 / 服务」名，最后退回页签标题。 */
  function draftName(tab: Tab): string {
    const named = tab.request.name.trim()
    if (named) return named
    const g = tab.request.grpc
    if (g) return (g.method || g.service || '').trim() || tab.title
    return tab.title
  }

  /** 打开草稿保存框（saveOnly = 保存后不关页签）。 */
  function openDraftDialog(tab: Tab, saveOnly: boolean): void {
    draftSaveOnly.value = saveOnly
    draftTab.value = tab
    draftForm.value = { name: draftName(tab), folder: tab.draftFolder || ctx.rootKey }
  }

  function closeDraftDialog(): void {
    draftTab.value = null
    draftSaveOnly.value = false
  }

  // ---- 未保存询问框 ----
  const closeAsk = ref<{
    tab: Tab
    /** 批量关闭时的进度（用于提示「第 i / n 个」），单个关闭为 null */
    seq: { i: number; n: number } | null
    resolve: (c: CloseChoice) => void
  } | null>(null)
  const closeAskTab = computed(() => closeAsk.value?.tab ?? null)
  const closeAskSeq = computed(() => closeAsk.value?.seq ?? null)

  /** 弹询问框并等用户作答。 */
  function askClose(tab: Tab, seq: { i: number; n: number } | null = null): Promise<CloseChoice> {
    return new Promise<CloseChoice>((resolve) => {
      closeAsk.value = { tab, seq, resolve }
    })
  }

  /** 询问框作答（按钮 / Esc / 遮罩关闭都走这里，否则 await 会一直挂着）。 */
  function answerClose(choice: CloseChoice): void {
    const ask = closeAsk.value
    closeAsk.value = null
    ask?.resolve(choice)
  }

  /**
   * 关掉一个页签（草稿/脏页签都先问；其余直接关）。
   * 返回是否真的关掉了 —— 批量关闭据此判断「用户取消（中止整批）」还是「写盘被拒（保留页签）」。
   */
  async function closeOne(key: string, seq: { i: number; n: number } | null = null): Promise<boolean> {
    const tab = tabs.tabs.find((t) => t.key === key)
    if (!tab) return true // 已经不在了（批量关闭过程中被前面的操作带走）：算处理过
    if (tab.draft) {
      openDraftDialog(tab, false)
      return false // 保存框由对话框的保存/丢弃按钮控制关闭，这里不阻塞批量流程（草稿逐个处理）
    }
    if (!tab.dirty) {
      await ctx.withoutDetailTransition(() => tabs.close(key))
      return true
    }
    const choice = await askClose(tab, seq)
    if (choice === 'cancel') return false
    if (choice === 'discard') {
      await ctx.withoutDetailTransition(() => tabs.closeDiscard(key))
      return true
    }
    // 保存并关闭：写盘被拒（冲突 / 只读 / 磁盘错误）时页签留着，原因由 flush 自己提示
    await tabs.flush(key)
    if (tab.dirty || tab.conflict) return false
    await ctx.withoutDetailTransition(() => tabs.close(key))
    return true
  }

  /** 统一关闭入口：草稿弹保存框；已落盘但有未保存改动先问一句；其余直接关。 */
  function requestClose(key: string): void {
    void closeOne(key)
  }

  /**
   * 批量关闭（关闭左侧 / 右侧 / 全部）：
   *  - 已落盘脏页签：**依次**弹「保存/丢弃/取消」；
   *  - 草稿：统一累计，最后**一次性**弹「全部丢弃 / 取消」；
   *  - 用户点「取消」（任一环节）= 中止整批，剩下的一律保留。
   */
  async function closeManySequential(keys: string[]): Promise<void> {
    const dirty = keys.filter((k) => {
      const td = tabs.tabs.find((x) => x.key === k)
      return !!td && !td.draft && td.dirty
    }).length
    let closed = 0
    let asked = 0
    const remainingDrafts: string[] = []
    for (const key of keys) {
      const tab = tabs.tabs.find((t) => t.key === key)
      if (!tab) continue
      if (tab.draft) {
        remainingDrafts.push(key)
        continue
      }
      const seq = tab.dirty ? { i: ++asked, n: dirty } : null
      if (!(await closeOne(key, seq))) {
        message.info(t('tab.closeAborted', { n: closed }))
        return
      }
      closed++
    }
    if (remainingDrafts.length) {
      const discarding = await askBatchDiscardDrafts(remainingDrafts.length)
      if (discarding) {
        for (const k of remainingDrafts) {
          const tab = tabs.tabs.find((t) => t.key === k)
          if (!tab) continue
          await ctx.withoutDetailTransition(() => tabs.close(k)) // 草稿直接 close = 丢弃（没有磁盘副本）
          closed++
        }
      } else {
        if (closed > 0) message.info(t('tab.closeAborted', { n: closed }))
        return
      }
    }
    if (!closed) {
      message.info(t('tab.nothingToClose'))
      return
    }
    message.success(t('tab.closedSome', { n: closed }))
  }

  /** 批量丢弃草稿的确认框：一次性问「有 N 个新建请求未保存，全部丢弃？」。 */
  function askBatchDiscardDrafts(n: number): Promise<boolean> {
    return new Promise((resolve) => {
      const d = dialog.warning({
        title: t('prompt.draftBatchTitle', { n }),
        content: t('prompt.draftBatchContent', { n }),
        positiveText: t('prompt.draftBatchDiscard'),
        negativeText: t('common.cancel'),
        onPositiveClick: () => resolve(true),
        onNegativeClick: () => resolve(false),
        onClose: () => resolve(false),
        onMaskClick: () => resolve(false),
      })
      void d
    })
  }

  /** tab 栏右键菜单命令：关闭类走批量关闭（草稿自动跳过），复制新建开草稿，保存所有 = 立即 flush 全部改动。 */
  async function onTabCommand(cmd: string, key: string): Promise<void> {
    const i = tabs.tabs.findIndex((t) => t.key === key)
    if (cmd === 'save-all') {
      const n = tabs.tabs.filter((t) => t.dirty && !t.draft && t.uid).length
      if (!n) {
        message.info(t('tab.nothingToSave'))
        return
      }
      await tabs.flushAll()
      message.success(t('tab.savedAll'))
      return
    }
    if (i < 0) return
    if (cmd === 'close') {
      requestClose(key)
      return
    }
    if (cmd === 'duplicate') {
      tabs.duplicate(key)
      return
    }
    const keys =
      cmd === 'close-all'
        ? tabs.tabs.map((t) => t.key)
        : cmd === 'close-left'
          ? tabs.tabs.slice(0, i).map((t) => t.key)
          : tabs.tabs.slice(i + 1).map((t) => t.key)
    await closeManySequential(keys)
  }

  /** Ctrl+S：已落盘请求立即写盘；新建草稿开保存框，保存后留在原地。 */
  function saveActiveNow(): void {
    if (draftTab.value) return // 保存框已经开着时不再重复打开（否则会重置已填内容）
    const tab = tabs.active
    if (!tab) return
    if ((tab.kind ?? 'request') === 'doc') {
      void tabs.saveDoc(tab.key)
      return
    }
    if (!tab.draft) {
      void tabs.flush(tab.key)
      return
    }
    openDraftDialog(tab, true)
  }

  /** 保存草稿弹窗内回车确认：输入法组词、多行输入、下拉选择中的回车都不算确认。 */
  function onDraftEnter(e: KeyboardEvent): void {
    if (e.isComposing) return
    const el = e.target as HTMLElement | null
    if (el?.tagName === 'TEXTAREA') return
    if (el?.closest?.('.n-base-selection, .n-tree-select')) return
    if (draftForm.value.name.trim()) void confirmSaveDraft()
  }

  /** 保存草稿（落盘到所选分组）：关闭流程顺带关页签，Ctrl+S 流程保存后留在原地继续编辑。 */
  async function confirmSaveDraft(): Promise<void> {
    // 回车（keydown）与按钮点击可能在同一帧内各来一次：保存中直接忽略，避免重复建请求
    if (savingDraft.value) return
    const tab = draftTab.value
    const name = draftForm.value.name.trim()
    if (!tab || !name) return
    const folder = draftForm.value.folder === ctx.rootKey ? '' : draftForm.value.folder
    savingDraft.value = true
    try {
      await tabs.saveDraft(tab.key, folder, name)
      const keepOpen = draftSaveOnly.value
      closeDraftDialog()
      if (!keepOpen) await ctx.withoutDetailTransition(() => tabs.close(tab.key))
      message.success(t('prompt.draftSaved', { name }))
    } catch (e) {
      message.error(e instanceof Error ? e.message : String(e))
    } finally {
      savingDraft.value = false
    }
  }

  /** 不保存：丢弃草稿并关闭（只在关闭流程出现）。 */
  async function discardDraft(): Promise<void> {
    const tab = draftTab.value
    closeDraftDialog()
    if (tab) await ctx.withoutDetailTransition(() => tabs.discardDraft(tab.key))
  }

  return {
    draftTab,
    draftSaveOnly,
    draftForm,
    savingDraft,
    openDraftDialog,
    closeDraftDialog,
    confirmSaveDraft,
    discardDraft,
    onDraftEnter,
    closeAskTab,
    closeAskSeq,
    answerClose,
    requestClose,
    onTabCommand,
    saveActiveNow,
  }
}

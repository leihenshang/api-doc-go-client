// 交互辅助：把「hover 才出现、且宿主行可能被重渲染替换」这类控件的点击做成可重试动作。
import { expect, type Locator } from '@playwright/test'

/**
 * 点击「悬停才显示」的行内操作按钮（如侧栏分组行的 ＋/重命名/删除，testId 形如 `tree.row.plus`）。
 *
 * 为什么需要重试：集合目录变更后会重载集合树，行元素被整体替换；
 * Chromium 在指针不动时不会给新插入的元素重新计算 :hover，
 * 于是 `display: none` 的 `.actions` 一直不可见 → 普通 click 会一直等到超时。
 * 这里用 toPass 把「hover + click」作为一个整体重试，任一步失败就重来。
 */
export async function hoverAndClick(row: Locator, testId: string, timeout = 15_000): Promise<void> {
  await expect(async () => {
    await row.hover()
    await row.getByTestId(testId).click({ timeout: 1_500 })
  }).toPass({ timeout })
}

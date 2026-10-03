// 常用 UI 动作：只用 role / data-testid / 语言包文案定位，不碰 scoped 类名与 nth-child。
import type { Locator, Page } from '@playwright/test'
import { expect, type AppFixture } from './app'
import { t } from './i18n'
import { resetSettings } from './settings'

/**
 * 打开应用并进入集合。
 * 首次访问是欢迎页（点「打开集合目录…」）；浏览器里已存过 client.lastDir 时会自动打开，故两种都兼容。
 */
export async function openCollection(page: Page, app: AppFixture): Promise<void> {
  await resetSettings(app) // 先复位设置再加载页面，保证用例从默认状态开始
  await page.goto(app.url)
  await reopenApp(page)
}

/** 刷新/重进应用后回到工作台（自动打开过就不再点欢迎页按钮） */
export async function reopenApp(page: Page): Promise<void> {
  const welcome = page.getByRole('button', { name: t('welcome.open') })
  if (await welcome.isVisible().catch(() => false)) await welcome.click()
  await expect(page.getByTestId('toolbar.collection')).toBeVisible()
}

/** 侧栏：按显示名取分组行 / 请求行 */
export const treeRow = (page: Page, name: string): Locator =>
  page.getByTestId('tree.row').filter({ hasText: name })

/** 侧栏：按 uid 取行（重命名等行内编辑场景下，名称已不在文本里，只能按 uid 定位） */
export const treeRowByUid = (page: Page, uid: string): Locator =>
  page.locator(`[data-testid="tree.row"][data-uid="${uid}"]`)

/** 侧栏：打开某个请求（行内名称按钮） */
export async function openRequest(page: Page, name: string): Promise<void> {
  await page.getByTestId('tree.row').filter({ hasText: name }).getByTestId('tree.row.name').click()
}

/** 请求栏：切换方法下拉并选中（naive NSelect 的选项挂在 body 上） */
export async function pickMethod(page: Page, method: string): Promise<void> {
  await page.getByTestId('req.method').click()
  await page.locator('.n-base-select-option').filter({ hasText: method }).click()
}

/** 请求栏：点发送，并等到「状态码徽标」或「错误告警」出现（不靠固定 sleep） */
export async function send(page: Page): Promise<void> {
  await page.getByTestId('req.send').click()
  await expect(page.getByTestId('resp.status').or(page.locator('.resp .n-alert'))).toBeVisible({ timeout: 30_000 })
}

/** 设置弹窗根节点（按「主题」下拉定位，避免与其它 .n-modal 混淆） */
export const settingsModal = (page: Page): Locator =>
  page.locator('.n-modal').filter({ has: page.getByTestId('settings.theme') })

/** 工具栏 → 全局设置：打开设置弹窗 */
export async function openSettings(page: Page): Promise<void> {
  await page.getByTestId('toolbar.collection').click()
  await page.locator('.n-dropdown-option').filter({ hasText: t('settings.title') }).click()
  await expect(page.getByTestId('settings.theme')).toBeVisible()
}

/**
 * 设置弹窗：切到指定分区（左侧竖向菜单）。
 * 设置项按类型分了区（界面 / 网络与安全 / 本地数据 / MCP 服务），右侧只渲染当前分区，
 * 所以操作别的分区的字段前必须先切过去。
 */
export async function openSettingsSection(page: Page, key: 'appearance' | 'network' | 'local' | 'mcp'): Promise<void> {
  await page.getByTestId(`settings.nav.${key}`).click()
  await expect(page.getByTestId(`settings.pane.${key}`)).toBeVisible()
}

/** 设置弹窗：保存并等待关闭 */
export async function saveSettings(page: Page): Promise<void> {
  // 用「弹窗内的主按钮」定位：切到英文后文案是 Save，按中文文案找会失效；
  // 响应面板还有个「保存响应」按钮，限定在设置弹窗内才不会歧义
  await settingsModal(page).locator('.n-button--primary-type').click()
  await expect(page.getByTestId('settings.theme')).toBeHidden()
}

/** 响应区：切到某个页签（body / headers / fields） */
export async function openRespTab(page: Page, seg: 'body' | 'headers' | 'fields'): Promise<void> {
  await page.locator(`[data-testid="resp.tab"][data-seg="${seg}"]`).click()
}

/**
 * 切到「原始」视图并返回响应体 <pre>。
 * 断言 JSON 内容时用它而不是响应体页签的文本：页签默认是 JSON 树（`tls:true` 无引号无空格），
 * 原始视图才是可精确断言的源文本。
 */
export async function rawBody(page: Page): Promise<Locator> {
  await page.getByTestId('resp.raw').click()
  return page.getByTestId('resp.rawBody')
}

/** 读原始响应体并解析为 JSON：断言字段本身，避免依赖 JSON 的空格/缩进风格 */
export async function rawJSON<T = Record<string, unknown>>(page: Page): Promise<T> {
  const pre = await rawBody(page)
  return JSON.parse((await pre.textContent()) ?? 'null') as T
}

/** 请求区：切到某个页签（params / body / headers / auth / docs） */
export async function openReqTab(page: Page, seg: string): Promise<void> {
  await page.locator(`[data-testid="req.tab"][data-seg="${seg}"]`).click()
}

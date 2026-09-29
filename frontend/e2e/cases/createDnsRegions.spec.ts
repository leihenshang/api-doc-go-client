// 演示用例（打真实外部 https 接口，需可达）：集合内新建分组 → 分组内新建请求 → 设置 URL → 真实发送 →
// 严格证书模式失败 → 勾选「忽略 SSL 证书校验」后成功。
//
// 地址不写进仓库，用环境变量传入：
//   E2E_DEMO_URL     必填，例如 https://内网主机/dnsRegions（自签证书的 https 接口）；未设置则整条用例 skip
//   E2E_DEMO_STATUS  选填，忽略证书后期望的状态码（如 401）；不设则只断言「拿到了响应」
// 用例标 @demo，默认门禁（--smoke / --full）都以 --grep-invert @demo 排除。
import path from 'node:path'
import type { Locator, Page } from '@playwright/test'
import { expect, test } from '../helpers/app'
import { hoverAndClick } from '../helpers/dom'
import { listCollectionFiles, readCollectionFile } from '../helpers/fs'
import { t } from '../helpers/i18n'
import { openCollection, openSettings, saveSettings, send } from '../helpers/ui'

const DEMO_URL = process.env.E2E_DEMO_URL ?? ''
const DEMO_STATUS = process.env.E2E_DEMO_STATUS ?? ''

/** 从地址取请求名（路径最后一段），取不到就用 demo */
function requestNameOf(url: string): string {
  try {
    const last = new URL(url).pathname.split('/').filter(Boolean).pop()
    if (last) return last
  } catch {
    // 非法 URL 交给用例断言阶段暴露
  }
  return 'demo'
}

const FOLDER = 'dns'
const REQ_NAME = requestNameOf(DEMO_URL)

const folderRow = (page: Page): Locator => page.getByTestId('tree.row').filter({ hasText: FOLDER })
const dropdownOption = (page: Page, label: string): Locator =>
  page.locator('.n-dropdown-option').filter({ hasText: label })

test.skip(!DEMO_URL, '未设置 E2E_DEMO_URL（需外部可达的 https 接口），跳过 @demo 用例')

test(`[B2/B3/E1] 集合内新建分组 → 分组内新建请求 ${REQ_NAME || 'demo'} → 发送 @demo`, async ({ page, app }) => {
  const collectionDir = await app.newCollection('basic')

  await test.step('1. 打开集合目录（Welcome → 打开集合目录…）', async () => {
    await openCollection(page, app)
  })

  await test.step(`2. 新建分组「${FOLDER}」`, async () => {
    await page.getByTitle(t('tree.newFolder')).click()
    const input = page.getByPlaceholder(t('tree.folderName'))
    await input.fill(FOLDER)
    await input.press('Enter')
    await expect(folderRow(page)).toBeVisible()
  })

  await test.step(`3. 在分组内新建请求「${REQ_NAME}」`, async () => {
    await hoverAndClick(folderRow(page), 'tree.row.plus') // 分组行内「新建」下拉（悬停才显示）
    await expect(dropdownOption(page, t('tree.newRequest'))).toBeVisible()
    await dropdownOption(page, t('tree.newRequest')).click()

    const modal = page.locator('.n-modal')
    await expect(modal).toBeVisible()
    await modal.locator('input').first().fill(REQ_NAME) // 请求名称（所属分组已由侧栏预选）
    await modal.getByRole('button', { name: t('common.create') }).click()

    await expect(modal).toBeHidden()
    await expect(page.getByTestId('tree.row').filter({ hasText: REQ_NAME })).toBeVisible()
  })

  await test.step('4. 设置 URL 并确认已落盘（自动保存）', async () => {
    const urlInput = page.getByTestId('req.url')
    await expect(urlInput).toBeVisible()
    await urlInput.fill(DEMO_URL)
    await urlInput.blur()
    await expect(urlInput).toHaveValue(DEMO_URL)

    const reqFile = `${FOLDER}/${REQ_NAME}.yml`
    await expect.poll(() => readCollectionFile(collectionDir, reqFile), { timeout: 10_000 }).toContain(DEMO_URL)
    console.log(`\n  ▶ 新建请求文件内容：\n${readCollectionFile(collectionDir, reqFile)}`)
  })

  await test.step('5. 发送：严格模式应因证书校验失败', async () => {
    await send(page)
    await expect(page.locator('.resp .n-alert')).toContainText(/x509|certificate|证书|tls/i)
  })

  await test.step('6. 勾选「忽略 SSL 证书校验」后重发 → 拿到响应', async () => {
    await openSettings(page)
    const ignoreSsl = page.getByTestId('settings.insecureSsl')
    await ignoreSsl.click()
    await expect(ignoreSsl).toHaveClass(/n-checkbox--checked/)
    await saveSettings(page)

    await send(page)
    const status = page.getByTestId('resp.status')
    await expect(status).toBeVisible({ timeout: 20_000 }) // 运输层成功：出现状态徽标而不是证书错误
    if (DEMO_STATUS) await expect(status).toContainText(DEMO_STATUS)
  })

  await test.step('7. 留档：集合文件清单 + 结束态截图', async () => {
    console.log(`  ▶ 集合文件：\n    ${listCollectionFiles(collectionDir).join('\n    ')}`)
    await page.screenshot({ path: path.resolve(collectionDir, '..', 'final-state.png') })
  })
})

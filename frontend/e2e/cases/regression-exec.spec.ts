// 对应 doc《客户端功能规划与完成情况》§5 回归要点 1、2 与请求执行组（E）：
// 证书策略 / 重定向三态 / 超时 / 状态码 / 大响应 / 二进制 / 压缩 / 非 UTF-8 / Cookie / 认证 / multipart。
// 全部打本地 fixtures 与 devserver 的自签 TLS 端点，不依赖外网。
import { expect, test } from '../helpers/app'
import { patchRequest } from '../helpers/api'
import { t } from '../helpers/i18n'
import { openCollection, openRequest, openSettings, rawJSON, saveSettings, send } from '../helpers/ui'

const PING_UID = '33333333-3333-4333-8333-333333333333' // basic 种子里的 ping 请求
const BINARY_HINT = t('resp.binary')

async function sendUrl(page: import('@playwright/test').Page, app: import('../helpers/app').AppFixture, url: string) {
  await patchRequest(app, PING_UID, { url })
  await page.getByTestId('req.url').fill(url) // UI 与磁盘保持一致（发送用的是编辑器里的草稿）
  await page.getByTestId('req.url').blur()
  await send(page)
}

test.describe('请求执行：网络策略与响应形态', () => {
  test('[E1] 证书策略：严格失败 / 忽略 SSL 后成功 @smoke', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    await sendUrl(page, app, '{{tlsHost}}/echo')
    await expect(page.locator('.resp .n-alert')).toContainText(/x509|certificate|证书|tls/i)

    await openSettings(page)
    await page.getByTestId('settings.insecureSsl').click()
    await saveSettings(page)

    await send(page)
    await expect(page.getByTestId('resp.status')).toContainText('200')
    expect((await rawJSON<{ tls: boolean }>(page)).tls).toBe(true)
  })

  test('[E2] 重定向三态：跟随 / 不跟随 / 超上限', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    // 默认跟随：跳完 2 跳后拿到最终 JSON
    await sendUrl(page, app, '{{host}}/redirect/2')
    await expect(page.getByTestId('resp.status')).toContainText('200')
    expect((await rawJSON<{ hops: number }>(page)).hops).toBe(0)

    // 关闭跟随：拿到 302 本身
    await openSettings(page)
    await page.getByTestId('settings.followRedirects').click()
    await saveSettings(page)
    await sendUrl(page, app, '{{host}}/redirect/2')
    await expect(page.getByTestId('resp.status')).toContainText('302')

    // 打开跟随但上限 1：超过上限应报错
    await openSettings(page)
    await page.getByTestId('settings.followRedirects').click()
    await page.getByTestId('settings.maxRedirects').locator('input').fill('1')
    await saveSettings(page)
    await sendUrl(page, app, '{{host}}/redirect/3')
    await expect(page.locator('.resp .n-alert')).toContainText(/redirect|重定向/i)
  })

  test('[E3] 超时：timeoutSec=1 打 3s 延迟应失败', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    await openSettings(page)
    await page.getByTestId('settings.timeout').locator('input').fill('1')
    await saveSettings(page)

    await sendUrl(page, app, '{{host}}/delay?ms=3000')
    await expect(page.locator('.resp .n-alert')).toContainText(/timeout|deadline|超时/i)
  })

  test('[E4] 状态码：404 带体 / 204 无体', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    await sendUrl(page, app, '{{host}}/status/404')
    await expect(page.getByTestId('resp.status')).toContainText('404')
    expect((await rawJSON<{ status: number }>(page)).status).toBe(404)

    await sendUrl(page, app, '{{host}}/status/204')
    await expect(page.getByTestId('resp.status')).toContainText('204')
    await expect(page.getByTestId('resp.rawBody')).toHaveText('')
  })

  test('[E5] 响应形态：大 JSON / 二进制 / gzip / 非 UTF-8', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    await sendUrl(page, app, '{{host}}/json/big?rows=500')
    await expect(page.getByTestId('resp.status')).toContainText('200')
    await expect(page.locator('.resp .pane')).toContainText('row-0')

    await sendUrl(page, app, '{{host}}/binary?kb=2')
    await expect(page.locator('.resp .binhint')).toHaveText(BINARY_HINT)

    await sendUrl(page, app, '{{host}}/gzip')
    await expect(page.getByTestId('resp.status')).toContainText('200')
    expect((await rawJSON<{ gzipped: boolean }>(page)).gzipped).toBe(true)

    // 中性 Content-Type + 非法 UTF-8 → 落到「按 UTF-8 合法性判断」分支，判定为二进制
    await sendUrl(page, app, '{{host}}/charset/latin1')
    await expect(page.locator('.resp .binhint')).toHaveText(BINARY_HINT)

    // 同样字节但声明 text/* → 信任 Content-Type，按文本展示，不判二进制
    await sendUrl(page, app, '{{host}}/text/latin1')
    await expect(page.getByTestId('resp.status')).toContainText('200')
    await expect(page.locator('.resp .binhint')).toHaveCount(0)
  })

  test('[E6] Cookie：拦截端点写入的 Cookie 会被后续请求带上', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await openRequest(page, 'ping')

    await sendUrl(page, app, '{{host}}/cookies/set?name=e2eCookie&value=kept')
    await expect(page.getByTestId('resp.status')).toContainText('200')

    await sendUrl(page, app, '{{host}}/cookies/echo')
    await expect(page.locator('.resp .pane')).toContainText('e2eCookie=kept')
  })

  test('[E7] 认证注入：basic 认证头真实落到请求上', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    await patchRequest(app, PING_UID, { auth: { type: 'basic', username: 'e2e', password: 'secret' } })
    await openRequest(page, 'ping')

    await sendUrl(page, app, '{{host}}/auth/require')
    await expect(page.getByTestId('resp.status')).toContainText('200')
    await expect(page.locator('.resp .pane')).toContainText('Basic ')
  })

  test('[E8] multipart：表单字段按 multipart 组装', async ({ page, app }) => {
    await app.newCollection('basic')
    await openCollection(page, app)
    // 种子请求自带 Content-Type: application/json，而 runner 只在该头为空时才注入
    // body 推导出的 Content-Type（multipart 需要带 boundary 的那个），故这里先清空头
    await patchRequest(app, PING_UID, {
      method: 'POST',
      headers: [],
      body: { type: 'multipart', form: [{ name: 'field1', value: 'v1', enabled: true }] },
    })
    await openRequest(page, 'ping')

    await sendUrl(page, app, '{{host}}/multipart')
    await expect(page.getByTestId('resp.status')).toContainText('200')
    const echo = await rawJSON<{ fields: Record<string, string>; contentType: string }>(page)
    expect(echo.fields).toMatchObject({ field1: 'v1' })
    expect(echo.contentType).toBe('multipart/form-data')
  })
})

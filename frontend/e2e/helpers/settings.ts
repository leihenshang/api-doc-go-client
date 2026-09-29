// 全局设置复位：设置是「进程级 + 落盘」的，用例会改它（缩放/布局/证书/重定向/超时…），
// 因此每个用例开跑前把它拨回默认，保证用例之间与执行顺序无关。
import type { AppFixture } from './app'

export async function resetSettings(app: AppFixture): Promise<void> {
  const cur = await app.ipc<Record<string, unknown>>('GetSettings')
  await app.ipc('SaveSettings', {
    ...cur,
    uiScale: 1,
    responseLayout: 'right',
    responseSize: 44,
    theme: 'light',
    insecureSsl: false,
    followRedirects: true,
    maxRedirects: 5,
    timeoutSec: 30,
    persistCookies: true,
  })
}

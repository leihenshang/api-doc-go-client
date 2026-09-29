import { defineConfig } from '@playwright/test'

// 默认有头（便于旁观操作），E2E_HEADLESS=1 切无头（CI）；
// E2E_SLOWMO 控制每步放慢的毫秒数（0 = 全速）。
export default defineConfig({
  testDir: './e2e',
  timeout: 120_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  reporter: [['list']],
  // 每次运行独立产物目录：Playwright 会清空 outputDir，固定目录会触发环境的批量删除保护
  outputDir: `../.tmp/e2e-artifacts/run-${Date.now()}`,
  use: {
    headless: process.env.E2E_HEADLESS === '1',
    viewport: { width: 1440, height: 900 },
    locale: 'zh-CN',
    timezoneId: 'Asia/Shanghai',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    launchOptions: { slowMo: Number(process.env.E2E_SLOWMO ?? 300) },
  },
})

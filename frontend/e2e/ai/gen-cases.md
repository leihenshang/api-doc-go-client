# AI 规程 A：生成/补齐 e2e 用例

用途：新增功能点、改动交互后，由 AI 起草用例。**生成物必须人审 + 首跑通过才合并**；合并后即为确定性用例，此后不再依赖 AI。

## 输入包（按顺序读）

1. `doc/客户端功能规划与完成情况.md` §3（功能点编号）与 §5（回归要点）
2. 本目录 `README.md`（写用例的硬性规矩、夹具端点表）
3. 相关组件源码（`frontend/src/components/*.vue`）与所需 IPC（`internal/app/app.go` 导出方法）
4. 真实 DOM 结构（有 devserver 时）：
   ```bash
   go build -o .tmp/devserver ./cmd/devserver
   XDG_CONFIG_HOME=.tmp/ai/config .tmp/devserver -dir .tmp/ai/collection -addr 127.0.0.1:8199 -web frontend/dist &
   agent-browser open http://127.0.0.1:8199 && agent-browser snapshot -i
   ```

## 步骤

1. **先补句柄**：目标元素若没有稳定定位，在组件上加 `data-testid="模块.子域.元素"`（`resp.status`、`tree.row.plus`、`settings.theme`…），并用 `npm run build` 重建。
2. **写用例**：放进对应 `cases/regression-*.spec.ts`（或新建同族文件）；标题前缀功能点编号，外部网络依赖的标 `@demo`。
3. **前置数据用 IPC**（`helpers/api.ts` 的 `patchRequest`），**断言用 UI + 文件**；能读盘验证的不要只看界面。
4. **首跑**：`npm run test:e2e:headless -- --grep "关键字"`，失败按 `triage.md` 分类。
5. **登记**：在 `doc/客户端功能规划与完成情况.md` §3 对应功能点行写清用例文件名/标题；需要时更新本目录 README 的覆盖清单。

## 用例骨架

```ts
test('[E23] 一句话描述用户可见行为 @smoke', async ({ page, app }) => {
  const dir = await app.newCollection('basic')      // 种子：basic / bruno-sample
  await openCollection(page, app)                    // 复位设置 + 打开应用
  await openRequest(page, 'ping')

  await send(page)                                   // 交互
  await expect(page.getByTestId('resp.status')).toContainText('200')

  await expect.poll(() => readCollectionFile(dir, 'api/ping.yml')).toContain('…')   // 落盘断言
})
```

## 禁止

- 硬编码中文/英文文案（用 `t()/tEn()`）；用 scoped 类名或 nth-child 定位
- 用 `waitForTimeout` 掩盖竞态；把断言放宽到「只要不报错」
- 一条用例断言多个不相关功能点（失败后无法定位）
- 依赖其它用例留下的状态（集合、设置、localStorage）

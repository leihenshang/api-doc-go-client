# AI 规程 C：失败归因

用途：CI / 本地回归失败时，先分类再动手。**只有「产品回归」才值得改产品；其余先修用例或记录环境问题。**

## 输入包

- 失败用例标题（含功能点编号）、Playwright 输出片段
- `.tmp/e2e-artifacts/run-*/<用例>/`：`trace.zip`、`test-failed-1.png`、`error-context.md`（含页面快照）
- `.tmp/e2e/run-*/collection/`（集合快照）与 `config/config.json`（设置快照）
- `.tmp/e2e/run-*/` 日志（devserver / fixtures 输出）
- 同一用例最近一次通过的证据（如果手头有）

## 分类与线索

| 判定 | 线索 | 处置 |
|---|---|---|
| **product-regression** | 断言读到的值确实是错的（文件少字段、状态码不符、元素缺失且 DOM 快照里也没有）；`error-context.md` 与产品文档/设计不符 | 修产品；若暴露的是新场景，补一条更精准的用例 |
| **case-brittle** | 元素存在但定位不到（选择器/文案变了、strict 冲突）；断言依赖格式（JSON 空格、toast 条数） | 改用 `data-testid` / `t()` / `rawJSON` / `.last()`；修用例不改产品 |
| **flaky** | 同一提交重跑通过；竞态（自动保存防抖、文件监听、动画） | 用条件等待替掉隐式等待；必要时重试 1 次并记录 flake 率 |
| **env** | 端口占用、编译失败、外部主机不可达（`@demo` 用例）、无桌面/显示器 | 修环境或标记 `@demo`；不要改断言 |

## 输出（贴到 CI 摘要或 issue）

```json
{
  "case": "regression-exec.spec.ts › [E2] 重定向三态",
  "verdict": "product-regression|case-brittle|flaky|env",
  "confidence": 0.0,
  "featurePoint": "E2",
  "rootCauseHypothesis": "一句话",
  "evidence": ["具体读到的值 vs 期望值", "文件/diff/快照路径"],
  "suggestedFix": "一句话",
  "notify": true
}
```

规则：`notify=true` 只给 `product-regression`；`case-brittle` 进用例维护队列；`flaky`/`env` 记录后重跑一次再说结论。**禁止**在未给出证据（值对比 / 快照 / diff 路径）的情况下下结论。

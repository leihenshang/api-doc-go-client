# clientctl collection runner（RM-02）

`clientctl run` 把本地集合按树顺序跑一遍：执行 `vars.pre-request`、`script.pre-request`、真实发送、`script.post-response` 与 `assert`，输出 text / JSON / JUnit 报告。集合格式与桌面端完全一致（文件即真相源）。

> 本仓库的 `.github/workflows/collection-tests.yml` **只保留 `workflow_dispatch`**，推送代码不会自动运行；需要手动触发或复制到实际项目仓库后再接 push/PR。

## 本地用法

```bash
# 文本报告（默认）
go run ./cmd/clientctl run ./ci/fixtures/demo --env dev

# JSON / JUnit
go run ./cmd/clientctl run ./ci/fixtures/demo --env dev --reporter json --output report.json
go run ./cmd/clientctl run ./ci/fixtures/demo --env dev --reporter junit --output report.xml

# 只看部分请求；首个失败即停；单请求 10s 超时
go run ./cmd/clientctl run ./ci/fixtures/demo --env dev --filter health --bail --timeout 10
```

参数：

| 参数 | 说明 |
|---|---|
| `--env <name>` | 环境名；空 = 只用内置变量（`{{$uuid}}` 等） |
| `--filter <substr>` | 按请求名称/路径做不区分大小写的子串过滤 |
| `--bail` | 第一个失败即停止 |
| `--timeout <sec>` | 覆盖全局单请求超时；0 = 用全局/请求级设置 |
| `--reporter text\|json\|junit` | 报告格式，默认 text |
| `--output <file>` | 报告写入文件；空 = stdout |

退出码：`0` 全部通过；`1` 有用例失败/加载失败；`2` 参数错误（flag 解析）。

## 支持范围

- HTTP 与 gRPC（unary + 服务器流；客户端流/双工仍由 runner 明确拒绝）。
- 集合级公共 Header/Auth（`opencollection.yml` 的 `defaults` 段）会按桌面端同一规则合并。
- 前置/后置脚本与断言：与桌面端共用 `internal/script`（goja，单段 5s 硬中断）。
- 变量链式传递：上一条请求 post-response 里 `bru.setVar('token', res.body.token)` 写入的变量，会自动传给后续请求。
- 报告不回显渲染后的 URL 与脚本变量表，避免 secret/token 进入 CI 日志。

## GitHub Actions（复制到目标仓库）

```yaml
on:
  push:
    branches: [main]
  pull_request:
  workflow_dispatch:

jobs:
  api-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - run: |
          go build -o clientctl ./cmd/clientctl
          ./clientctl run ./path/to/collection --env ci --reporter junit --output report.xml
      - if: always()
        uses: actions/upload-artifact@v4
        with:
          name: api-test-report
          path: report.xml
```

## GitLab CI

```yaml
api-tests:
  image: golang:1.26
  script:
    - go build -o clientctl ./cmd/clientctl
    - ./clientctl run ./path/to/collection --env ci --reporter junit --output report.xml
  artifacts:
    when: always
    reports:
      junit: report.xml
```

## 密钥与环境

- `environments/<env>.secrets.yml` 已在集合 `.gitignore` 中，**不要提交**；CI 里应由 Secret 注入或由部署步骤生成。
- 目前不提供 `--var` 覆盖；如果 CI 环境变量需要动态注入，建议在步骤里生成对应的 `*.secrets.yml`（或改用不带 secret 的环境）。
- 报告 JSON 不含请求头/请求体/渲染后的 URL；失败详情只含断言名/表达式与错误文本。

## 示例夹具

`ci/fixtures/demo` 是一个最小集合：请求 `{{host}}/health.json`，断言状态 200 与 `res.body.ok === true`。`ci/fixtures/http/health.json` 由 `python3 -m http.server 18080 --directory ci/fixtures/http` 提供，供本仓库手动 workflow 与本地自测使用。

package mcp

// get_doc_template 的结构契约：与前端 lib/genDocs.ts#buildApiDoc 的输出一一对应
// （小节顺序、表列、掩码规则均以那份实现为准，那边变更时此处必须同步）。
// docs 字段本身是自由文本，不做强校验；但按本模板写，客户端「文档」一节与
// Markdown/HTML 导出都能一致渲染。

const docFence = "```"

// DocTemplate 返回自动生成接口文档的分节结构契约（静态文案，无需项目上下文）。
func (s *Service) DocTemplate() string { return docTemplateText }

const docTemplateText = `接口文档模板（对齐客户端「生成接口文档」；小节顺序固定，无内容的小节整体省略）

## 简要描述
- <接口名>

## 请求URI
- {{host}}/path/to/api（只留路径：去掉查询串——查询参数交由「请求参数」表；协议+主机替换为 {{host}} 占位）

## 请求方式
- POST

## 请求头
- **Content-Type**: application/json（每条启用且有名字的头部一行，格式 "- **名称**: 值"）

## 请求参数
| 参数名 | 必选 | 类型 | 说明 |
| --- | --- | --- | --- |
| page | 是 | string | 页码 |

## 请求体
- json（无请求体时整节省略；form/multipart 同理列出类型与字段表）

| 参数名 | 必选 | 类型 | 示例值 | 说明 |
| --- | --- | --- | --- | --- |
| uid | 是 | string | u-42 | 用户ID |

（请求体字段表来自请求的 body.fields —— 客户端「生成数据表格」手工生成、可编辑；表格为空时只输出请求体原文，不出表）

## 请求示例

` + docFence + `shell
curl -X POST '{{host}}/path/to/api' …（客户端按当前环境生成的 curl 命令，其中主机同样替换为 {{host}}）
` + docFence + `

` + docFence + `json
{ "uid": "u-42" }（JSON 请求体时另附一段缩进美化的请求数据示例；无 JSON 体则省略此块）
` + docFence + `

## 响应字段
| 参数名 | 必选 | 类型 | 示例值 | 说明 |
| --- | --- | --- | --- | --- |
| data[].id | 是 | number | 42 | 主键；嵌套字段路径用 a.b / a[].c 表达 |

## 响应示例

` + docFence + `json
{ "code": 0 }（取已保存响应示例里最新一个 JSON 体；没有则省略本节）
` + docFence + `

## 备注
（留空，留给手写）

书写规则（与生成逻辑一致）：
- 请求参数表固定 4 列：参数名 | 必选 | 类型 | 说明；必选列当前恒为「是」，类型缺省 string。
- 请求体字段表固定 5 列：参数名 | 必选 | 类型 | 示例值 | 说明；数据来自请求的 body.fields（手工生成，可为空）。
  表格为空时不输出该表，只输出请求体原文；必选列按 body.fields[].required 写「是」/「否」，类型缺省 string。
- 响应字段表固定 5 列：参数名 | 必选 | 类型 | 示例值 | 说明；数据来自请求的 respDoc.params（手工生成，可为空）。
  表格为空时不输出该表（响应示例照常输出）；必选列按 respDoc.params[].required 写「是」/「否」（默认「是」），
  类型缺省 string，示例值取自 respDoc.params[].example，说明取自 respDoc.params[].description。
- 单元格里的竖线要转义（\|），换行压成空格，否则会破坏表格。
- 「响应字段」一节与请求的 respDoc（响应字段文档）同源：两边都写时内容必须一致，
  客户端「生成接口文档」会直接取 respDoc 覆盖本节（含示例值列）。
- 变量引用统一写 {{host}}，不要落真实域名（环境可切换）。
`

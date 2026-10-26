# testserver · 增删改查测试服务

仅依赖 Go 标准库的本地测试服务，提供 5 个用户接口，用于客户端在线调试、同步联调与集合运行器的目标端。
数据存内存，**重启即清空**；统一响应格式 `{code, msg, data}`，HTTP 恒 200（`code=0` 成功，`code=1` 业务失败）。

## 启动

```bash
go run ./testserver              # 默认端口 :9091
go run ./testserver -port 9999   # 指定端口
```

## 接口清单

| # | 方法 | URL | 参数 | 说明 |
|---|---|---|---|---|
| 1 | POST | `/api/user/create` | body `{"name","email","age"}` | 新增（`name` 必填），返回完整实体 |
| 2 | POST | `/api/user/delete` | body `{"id":1}` | 删除，id 不存在返回 code 1 |
| 3 | POST | `/api/user/update` | body `{"id":1, "name"?, "email"?, "age"?}` | 局部修改，只覆盖传入字段 |
| 4 | GET | `/api/user/list` | query `page=1&size=10` | 分页列表，按 id 升序 |
| 5 | GET | `/api/user/detail` | query `id=1` | 详情 |

## curl 示例

```bash
# 1) 新增
curl -s -X POST localhost:9091/api/user/create \
  -H 'Content-Type: application/json' \
  -d '{"name":"张三","email":"zhangsan@example.com","age":28}'
# => {"code":0,"msg":"ok","data":{"id":1,"name":"张三","email":"zhangsan@example.com","age":28,"createdAt":"...","updatedAt":"..."}}

# 2) 修改（只改 name）
curl -s -X POST localhost:9091/api/user/update \
  -H 'Content-Type: application/json' \
  -d '{"id":1,"name":"张三丰"}'

# 3) 列表（第 1 页，每页 10 条）
curl -s 'localhost:9091/api/user/list?page=1&size=10'
# => {"code":0,"msg":"ok","data":{"total":1,"page":1,"size":10,"items":[...]}}

# 4) 详情
curl -s 'localhost:9091/api/user/detail?id=1'

# 5) 删除
curl -s -X POST localhost:9091/api/user/delete \
  -H 'Content-Type: application/json' \
  -d '{"id":1}'

# 失败示例：id 不存在
curl -s 'localhost:9091/api/user/detail?id=999'
# => {"code":1,"msg":"用户 999 不存在","data":null}
```

## 说明

- 服务未做鉴权与持久化，仅作联调靶子；不要暴露到公网。
- 新增接口请同步更新本 README 的接口清单表。

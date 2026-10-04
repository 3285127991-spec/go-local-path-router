# pathrouter

一个零依赖的 Go 本地路径路由器库：按 HTTP 方法 + 路径模式注册路由，并对实际请求路径做匹配。
只提供后端库、命令行演示和自动化测试——不包含 HTTP 服务、网络监听、数据库或任何第三方依赖。

## 路径模式语法

| 模式 | 类型 | 说明 |
| --- | --- | --- |
| `/users/new` | 静态片段 | 逐段精确匹配 |
| `/users/:id` | 单段参数 | 匹配任意一个非空片段，提取为 `id` |
| `/files/*path` | 末尾通配符 | 匹配剩余零到多个片段，以 `/` 连接后提取为 `path` |

匹配优先级**始终**为：静态片段 > 参数片段 > 通配符，与注册顺序无关。
例如同时注册 `/users/new`、`/users/:id`、`/users/*rest` 时，
`/users/new` 命中静态路由，`/users/42` 命中参数路由，`/users/42/posts` 命中通配符路由。

## 快速开始

```go
package main

import (
	"fmt"

	"pathrouter"
)

func main() {
	r := pathrouter.New()

	// 注册：方法可以是任意非空字符串，同一模式可按方法分别注册。
	_ = r.Add("GET", "/users/new", "user-new-form")
	_ = r.Add("GET", "/users/:id", "user-show")
	_ = r.Add("POST", "/users/:id", "user-update")
	_ = r.Add("GET", "/files/*path", "file-serve")

	handler, params, ok := r.Match("GET", "/users/42")
	// handler = "user-show", params = {"id": "42"}, ok = true
	fmt.Println(handler, params, ok)

	r.Remove("POST", "/users/:id") // true；删除不存在的路由返回 false

	for _, rt := range r.Routes() { // 按 (方法, 模式) 排序的稳定列表
		fmt.Println(rt.Method, rt.Pattern, rt.Handler)
	}
}
```

## 命令行演示

```sh
go run ./cmd/demo                 # 运行内置演示表并打印路由列表
go run ./cmd/demo GET /users/42   # 查询单个 方法+路径
```

## 测试

可重复运行的验证命令（在仓库根目录执行）：

```sh
go test ./...                # 全部单元测试
go test -race ./... -count=1 # race detector 验证并发安全
go vet ./...                 # 静态检查
```

测试覆盖：静态匹配、参数提取、通配符、匹配优先级（对抗性注册顺序）、方法隔离、
重复/冲突注册、非法模式、Remove、Routes 稳定顺序、未匹配、尾部斜杠行为以及并发读写。

## 行为约定

- **注册校验**：拒绝空方法、空路径、非 `/` 开头的路径、重复的“方法+模式”、
  非末尾通配符、空参数名（`:` 或 `*`）、同一模式中重复的参数名；
  错误为可 `errors.Is` 判断的哨兵错误（见 `router.go` 顶部）。
- **形状冲突**：`/users/:id` 与 `/users/:name` 结构相同，后注册者返回
  `ErrConflictRoute`；Remove 只按精确的“方法+模式”字符串删除。
- **尾部斜杠**：不做归一化，`/users` 与 `/users/` 是不同路径；
  参数不会捕获空片段，因此 `/users/` 不匹配 `/users/:id`。
- **通配符**：可匹配零个片段，`/files` 命中 `/files/*path` 且 `path` 为 `""`。
- **方法**：区分大小写、不做归一化，未知方法一律不匹配。
- **并发**：`Match`/`Routes` 走读锁，`Add`/`Remove` 走写锁；
  `Match` 每次返回新建的参数 map，多 goroutine 并发安全。

## 项目结构

```
router.go          # Router 类型与 Add/Remove/Match/Routes（每方法一棵前缀 trie，带回溯匹配）
pattern.go         # 模式解析与校验
router_test.go     # 自动化测试
cmd/demo/main.go   # 命令行演示
```

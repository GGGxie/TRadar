# 浏览器访问令牌闭环：agent-skills 过程记录

- 记录日期：2026-10-20（Asia/Shanghai）
- 任务来源：GitHub Issue #29
- 状态：已完成，PR #30 于 2026-10-21 合入，随 #31 进入 main
- 对应提交：见 Issue #29 关联的 PR

## 1. 背景与边界

设置 `TRADAR_ACCESS_TOKEN` 后，`guard` 对静态页也要求 Bearer，而前端 `request()` 不带令牌，页面无法加载
（见 [Cloudflare Tunnel 临时运行](2026-10-20-cloudflare-tunnel-launch.md) 第 5 节）。

采用 Issue #29 的方案 A：静态外壳免令牌，API 仍强制令牌，前端用 sessionStorage 保存并附带令牌。

不做：Cookie 会话、登录端点、改动 `/healthz` 的现有保护、改动 Host/Origin/CSRF 规则。

## 2. 采用的技能

`using-agent-skills`、`spec-driven-development`、`test-driven-development`（读取自 addyosmani/agent-skills 的
`SKILL.md`）。本记录未使用其他技能。

## 3. 规格到实现切片

| 切片 | 验收 | 证据 |
|---|---|---|
| 后端放行静态外壳 | 带令牌实例上 GET/HEAD 的前端文件 200；`/api/*`、`/healthz`、写方法无令牌仍 401；点号文件、未知路径 404 | `internal/httpapi/access_test.go` |
| 前端携带令牌 | 有令牌才加 `Authorization`；无令牌时请求参数不变；401 `UNAUTHORIZED` 清除旧令牌并发出事件 | `web/src/access.test.tsx` |
| 解锁界面 | 收到事件显示表单；空令牌给出提示；输入后保存并重载 | `web/src/access.test.tsx` |

放行判断与 `static` 使用同一套路径清理（`filepath.Clean("/" + path)`），避免 `//api/...` 绕过。

## 4. TDD 记录

- RED：后端 `go test ./internal/httpapi -run TestStatic` 因静态页返回 401 失败；前端因 `AccessGate` 不存在而加载失败。
- GREEN：新增 `isStaticShellRequest` 与 `AccessGate`、`api.ts` 令牌函数后全部通过。
- REFACTOR：无。

## 5. 问题、根因与修正

- 首次运行新增测试时，Go 与 Vitest 都曾报告"没有测试"，原因是文件刚写入后工具链尚未看到；重跑后正常。不是代码问题。

## 6. 验证证据

- `go test -race -count=1 ./...` 全部通过；`go vet ./...` 无输出。
- `npx vitest --run`：24 个测试通过（原 16 + 新增 8）；`npm run build` 通过。
- 真实进程（回环、带令牌、临时库）用 curl 验证：`/radar` 与静态资源 200；无令牌 `/api/v1/projects` 与 `/healthz` 401；
  错误令牌 401；正确令牌 200；无令牌 `POST /radar` 401；`//api/v1/projects` 401；`/.git` 404；页面正文不含令牌。
- 无头 Chrome 访问未带令牌的 `/radar`，显示令牌输入框。
- 一次性 Playwright 脚本（未入库，对真实二进制）：未带令牌显示表单 → 错误令牌后再次要求输入且旧令牌被清除 →
  正确令牌后表单消失并显示主导航；令牌只在 sessionStorage，不在 localStorage。
  脚本里 `getByLabel("访问令牌")` 曾同时命中表单与输入框（表单的 `aria-labelledby` 名称为"需要访问令牌"），
  是脚本定位问题，改用 `getByRole("textbox")`；界面本身没有改动。

## 7. 产物与剩余限制

- 浏览器流程只做了一次性验证，没有写入 `web/e2e`，也没有重跑现有 Chromium 套件。
  原因：现有 Playwright 配置只启动一个无令牌实例，加入带令牌实例需要第二个 webServer，留作后续。
- 令牌存 sessionStorage，仍受同源 XSS 影响；仅靠现有 CSP 缓解。

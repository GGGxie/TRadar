# 实施验证记录

更新时间：2026-10-15（Asia/Shanghai）。这里只记录实际执行过的检查；未列模块仍按
[`tasks/todo.md`](../tasks/todo.md) 视为未完成。

## 已验证范围

| 范围 | 证据 |
|---|---|
| SQLite 初始化、迁移、事务和重启 | 临时真实数据库测试覆盖迁移、回滚、新版拒绝及关闭后重开；HTTP 重启测试验证项目仍可查询 |
| 项目身份 | URL 保守归一化、追踪参数移除、同名不同 URL 隔离、并发去重、稳定分页和中文搜索 |
| 项目 API | JSON 形状/大小、Host/Origin、同源写入、幂等重放、payload 不匹配、ETag 与旧版本冲突 |
| 项目界面 | 空状态、创建、列表、详情、编辑、刷新、搜索以及 360px 手机布局 |
| 证据 | 私密默认、URL 归一化、不可变修订、历史截止读取、公开过滤、跨项目引用拒绝、人工指标伪造拒绝 |
| 证据界面 | 新增、失败后草稿与重试 key 保留、修订保留旧版、原始来源外链及无障碍扫描 |
| 来源身份 | 稳定 `(source, externalId)` 去重、仓库改名保留项目 ID、新旧 URL 别名、冲突别名事务回滚及关联审计 |
| 观测 | 同 run 重试、不同 run 同值、累计/日桶隔离、桶内最新读取、非负安全整数、90 天窗口及失败事务无部分写入 |
| 来源设置 | 三个远程来源默认配置、按来源字段校验、ETag 冲突、凭据仅以存在状态显示、PH 默认禁用 |
| 采集任务 | 同键并发只建一个任务、完成重放原 202/Location、同源非终态唯一、来源独立完成、真实数据库重开后中断恢复 |
| 事务 checkpoint | catalog 数据、任务计数与 checkpoint 同事务提交；后一页失败保留前页；同 run/page 重放不重复写入 |
| GitHub 适配器 | 无 Token 搜索双桶、重复候选去重、稳定数字 ID、改名、最久未观测跟踪、认证停止、截断及空结果 |
| GitHub Star 历史 | 单自然周七个日桶、周总数校验、`UNSPECIFIED` 时区；历史 422 保留累计 Stars/Forks 并呈现 PARTIAL |
| Hacker News 适配器 | Top/New/Show 去重、讨论身份、共享外链归并、缺指标不填零、dead/deleted/null/非 story 跳过原因及空榜 |
| 请求预算 | GitHub 滚动小时与搜索分钟预算在请求前原子领取；并发 80 次只允许 60 次，关闭重开数据库不重置；平台响应额度只能收紧有效预算，匿名配置始终不超过 60/h |
| 重试与期限 | 网络错误、429、5xx 和明确的 GitHub 限流 403 最多三次并按 Retry-After/reset 退避；普通 403、401、解析失败不重试；单请求取消、五分钟任务取消及 `nextRetryAt` 持久化已覆盖 |
| 来源网络边界 | 生产客户端只接受三个内置 HTTPS 主机；解析结果含私网、回环、链路本地或保留地址即拒绝，连接固定到已校验 IP；同域最多一次重定向且删除 Authorization；解压后响应上限 2 MiB |
| 定时调度 | 持久化到期时间、停机多周期只补一次、同一到期时刻并发去重、来源忙时合并触发、平台退避前不运行及来源独立意图 |
| Product Hunt 条件适配 | 固定官方 GraphQL 端点、Token 请求、窗口分页、complexity 配额、部分 GraphQL 错误保留有效数据；缺凭据不发请求 |
| 来源设置界面 | 三来源配置、凭据/预算/恢复时间、未保存状态、立即采集与最近任务；组件和 Chromium 浏览器验证，axe 违规为零 |
| 真实来源冒烟 | 2026-10-15 匿名 GitHub 2 次请求导入 2 项并写 4 个观测；HN 2 次请求导入 1 项并写 2 个观测；临时 SQLite 关闭重开后项目详情、来源和观测仍可读取 |
| 手动限额反馈 | GitHub 本地搜索预算耗尽时，创建任务前返回 429 `SOURCE_RATE_LIMITED` 与 `Retry-After`，不留下排队任务 |
| 趋势增长计算 | 固定 asOf 的 24h/7d 容差窗口、真实跨度日均速度、下降、零基数、三点加速度、未来样本排除、累计/来源日桶隔离及至少四个完整日窗的持续性均由表驱动测试覆盖 |
| 趋势排序与雷达 | `trend-v1` 主信号评分、同平台去重、跨源加分、缺持续性部分结果、稳定并列排序和三点异常提示均有纯函数测试；SQLite 查询先评分后筛选分页，24h/7d 与来源筛选经 Chromium 验证 |

## 最近一次完整检查

以下命令在当前分支实际成功执行：

```sh
GOCACHE=/tmp/tradar-go-cache go test -race ./...
GOCACHE=/tmp/tradar-go-cache go vet ./...
npm --prefix web run test -- --run
npm --prefix web run build
npm --prefix web run test:e2e
npm --prefix web audit --json
```

结果：Go 全部包通过 race 测试及 vet；Vitest 为 3 个文件、6 个测试通过；Vite 生产构建成功；
Playwright Chromium 为 3 条流程通过。浏览器流程覆盖项目创建、证据新增和修订、项目编辑、刷新、
搜索、移动端布局、来源配置和手动采集，页面错误为零，所检查页面 axe 违规为零。npm 完整依赖
审计为 0 条已知漏洞。真实冒烟通过显式 `TRADAR_LIVE_SMOKE=1` 单独执行，不属于默认测试联网行为。

浏览器审阅截图：

- [`catalog-desktop.png`](screenshots/catalog-desktop.png)
- [`catalog-mobile.png`](screenshots/catalog-mobile.png)
- [`evidence-desktop.png`](screenshots/evidence-desktop.png)

## 当前限制

Product Hunt 真实接口尚未使用用户账号 Token 联调，GitHub 日历史也未在真实冒烟中覆盖。
分析、研究记录、日报、演示模式和可选模型尚未实现。当前运行不会访问人工录入的 URL，
也不会静默生成演示指标或商业结论。
公网发布、付费服务与外部通知不在当前授权范围内。

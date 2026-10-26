# UX-1 当前关注度与来源展示：agent-skills 过程记录

- 记录日期：2026-10-23（Asia/Shanghai）
- 任务来源：[GitHub Issue #32](https://github.com/GGGxie/TRadar/issues/32)
- 状态：已完成，已合并到 main（PR #38，2026-10-26）
- 对应提交：`9ebd85f`（后端）及本记录所在提交（前端与文档）

## 1. 背景与边界

2026-10-23 以用户身份在临时库上完整使用 TRadar：GitHub/HN 真实采集成功，但雷达 100 个项目全部显示
「待积累」，17 万星仓库与 0 星仓库外观相同；详情页不读取 `/trend`、`/sources`，并提示「项目介绍来自你的录入」。
规格 TREND-01 已要求单快照时「累计值仍可见」，属于实现缺口。

本次交付：TrendResult 增加按平台分列的当前关注度；待积累组按关注度辅助排序；列表与详情展示来源和最新累计值。
不改变增长公式与 score，不跨平台相加，不把累计量显示为增长，不补零。冷启动引导、采集门槛、分析与日报接入
分别在 #33–#35 处理。

## 2. 采用的技能

| 技能 | 读取文件 | 应用 |
|---|---|---|
| `incremental-implementation` | `engineering/agent-skills/skills/incremental-implementation/SKILL.md` | 后端契约（纯函数 + 排序 + HTTP）先成一片并提交，再做界面片 |
| `test-driven-development` | `engineering/agent-skills/skills/test-driven-development/SKILL.md` | 每片先写失败测试；HTTP 测试通过 stash 实现确认能捕获回归 |
| `frontend-ui-engineering` | `engineering/agent-skills/skills/frontend-ui-engineering/SKILL.md` | 状态文本与数值同时呈现；手机视口无横向溢出 |
| `api-and-interface-design` | `engineering/agent-skills/skills/api-and-interface-design/SKILL.md` | 只做加法字段；旧冻结快照缺字段时前端类型标为可选 |

## 3. 规格到实现切片

| 验收 | 切片 | 证据 |
|---|---|---|
| attention 取 asOf 前最新累计值、多平台分列、无观测为空 | `internal/trends/attention.go`、`ranking.go` | `TestComputeProjectKeepsLatestAttentionPerPlatformWithoutScoring`、`TestComputeProjectWithoutObservationsHasNoAttention` |
| 待积累组按关注度排序 | `SortProjectTrends` | `TestSortProjectTrendsOrdersAccumulatingGroupByAttention`，原有稳定性测试仍通过 |
| /radar 返回 attention 并按规则排序 | HTTP 无代码改动 | `TestRadarHTTPOrdersAccumulatingProjectsByCurrentAttention` |
| 列表显示关注度 | `ProjectList.tsx` | `ProjectList.test.tsx` |
| 详情显示来源、最新值与趋势解释 | 新增 `SignalPanel.tsx` | `SignalPanel.test.tsx`（两例） |
| 规格同步 | `SPEC-trend-ranking.md` 排序第 5/6 条、契约、TREND-08/11 | 本提交 |

## 4. TDD 记录

- RED（后端）：新增 3 个单测后 `go test ./internal/trends/` 编译失败，`TrendResult` 无 `Attention`。
- GREEN：实现 `computeAttention` 与排序分支后通过。
- HTTP 测试写在领域实现之后，直接通过；随后 `git stash` 掉 trends 改动重跑，失败为
  `order[0]=quiet-newest ... want=popular-oldest`，恢复后通过，证明测试能捕获回归。
- RED（前端）：`ProjectList.test.tsx` 找不到「GitHub ★ 17.4 万」；`SignalPanel` 尚不存在。
- GREEN：实现后 12 个测试文件 27 个测试通过。
- REFACTOR：`tsc` 发现旧日报快照夹具没有 `attention`、`explanations`；把 `attention` 标为可选（历史快照确实没有），
  为夹具补 `explanations`，`SignalPanel` 改用 `AttentionMetric[]` 类型。

## 5. 问题、根因与修正

- 关注度指数最初在测试中约束 `≤ 1`。截断会让 1 万星与 17 万星并列，退化为 firstSeenAt 排序；
  改为不截断，规格明确「不截断上限」。
- 指数取各平台 `ln(1+主指标)/ln(1+参考规模)` 的最大值，而不是求和，满足规格「不把不同平台原始值相加」。
  参考规模 GitHub 10000、HN 500、PH 500 是工程默认值，可后续调整。
- 未提升 `algorithmVersion`：score 公式与输入未变，只有待积累组的并列顺序和新增展示字段变化。

## 6. 验证证据

| 命令 / 检查 | 结果 |
|---|---|
| `go test -race ./...`、`go vet ./...`、`gofmt -l` | 全部通过，无格式问题 |
| `npx vitest --run` | 12 文件 27 测试通过 |
| `npm run build`（含 `tsc --noEmit`） | 通过 |
| `make e2e` | Chromium 7/7 通过 |
| 真实数据浏览器检查 | 用 10-23 验收临时库（113 个真实采集项目）启动新二进制：列表首位为 ponytail「GitHub ★ 17.4 万 / 增速待积累」，GitHub 与 HN 项目按关注度交错；详情页显示来源链接、Stars 173,618、Forks 9,447、采集/发布时间与未评分原因；375px 视口无横向溢出 |

## 7. 产物与剩余限制

- 新增 `internal/trends/attention.go`、`web/src/SignalPanel.tsx` 及对应测试。
- 详情页暂不展示观测历史曲线，只展示各指标最新值；需要时另建 Issue。
- 关注度只在待积累组生效；有评分项目的排序不受影响。
- 冷启动时仍需等待 24 小时以上的观测才能出现增速，见 #33。

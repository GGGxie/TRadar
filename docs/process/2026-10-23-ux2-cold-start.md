# UX-2 冷启动可用：agent-skills 过程记录

- 记录日期：2026-10-23（Asia/Shanghai）
- 任务来源：[GitHub Issue #33](https://github.com/GGGxie/TRadar/issues/33)
- 状态：已完成，PR 待审阅（基于 #32 分支）
- 对应提交：`1dc34b6`（后端）及本记录所在提交（前端与文档）

## 1. 背景与边界

用户视角验收发现：空库只引导「记录第一个项目」；GitHub 日级 Star 历史只对 `trackedSourceRecordIds` 读取，
界面没有设置入口；已有 SOURCE_DAILY 也从未展示。结果是首日用户看不到任何与增长相关的信息。

本次交付：日级历史空闲名额给本轮最受关注的新候选；`attention.sourceWeeks` 返回最新来源自然周；
列表显示「来源本周 +N」，详情显示 7 个日桶；详情页可跟踪/取消跟踪 GitHub 仓库；雷达空状态和全部未评分时给出采集指引。

保持不变：每轮最多 5 个历史仓库、每个只读最近一个来源周（规格 L40）；SOURCE_DAILY 不进入净增、加速度或 score；
不猜平台时区；不默认开启定时采集。

## 2. 采用的技能

| 技能 | 读取文件 | 应用 |
|---|---|---|
| `spec-driven-development` | `engineering/agent-skills/skills/spec-driven-development/SKILL.md` | 实现前发现历史数据只入库、不展示，先在 #33 写清细化范围，再同步两份规格 |
| `incremental-implementation` | 同 #32 | 后端（名额分配 + sourceWeeks）一片提交，界面一片提交 |
| `test-driven-development` | 同 #32 | 每片先写失败测试；有意改变的旧测试期望单独说明 |
| `security-and-hardening` | `engineering/agent-skills/skills/security-and-hardening/SKILL.md` | 日桶柱高不用内联 style，保持 CSP `style-src 'self'` 不放宽 |

## 3. 规格到实现切片

| 验收 | 实现 | 证据 |
|---|---|---|
| 名额：跟踪优先、候选按 Stars、已有历史跳过、≤5 | `allocateHistorySlots`、`catalog.SourceIDsWithBasis` | `TestGitHubExecutorGivesHistorySlotsToTrackedThenTopNewCandidates` |
| sourceWeeks 取最新周、同桶不翻倍、未来排除、不评分 | `latestSourceWeek` | `TestComputeProjectReturnsLatestSourceWeekWithoutDoubleCounting` |
| 列表本周新增、全部未评分提示、空状态引导 | `ProjectList.tsx` | `ProjectList.test.tsx` 新增 2 例；e2e 空状态断言更新 |
| 详情日桶与跟踪切换（If-Match） | `SignalPanel.tsx`、`api.getSource` | `SignalPanel.test.tsx` 新增 2 例 |
| 规格 | SPEC-source-ingestion L40；SPEC-trend-ranking 第 6 条、契约、TREND-12 | 本次提交 |

## 4. TDD 记录

- RED（名额）：新测试首轮期望 5 个历史请求，实际 `[]`。
- GREEN：实现后通过；旧测试 `...KeepsCumulativeWhenHistoryFails` 失败于「unexpected request
  /repos/acme/radar/stargazers/history」。这是有意的行为变化：首轮新候选现在会占用空闲名额。
  更新夹具在第 1 轮接受该路径，首轮期望由 6 请求/2 观测改为 7 请求/9 观测，其余阶段不变。
- RED（sourceWeeks）：编译失败，`AttentionResult` 无 `SourceWeeks`；实现后通过。
- RED（界面）：两例 SignalPanel 新测试失败；实现后 12 文件 31 测试通过。

## 5. 问题、根因与修正

- 实测 `stargazers/history` 第一页为最新周，当周未来日返回 0，且日界不是 UTC（周四数值在 UTC 周五仍在增长）。
  因此界面写「平台自然周（10/18 起），时区未说明；不计入趋势分」，不换算成滚动 7 天。
- 日桶柱高最初用内联 `style`。CSP 为 `style-src 'self'`，改为 0–10 档位 class，不放宽安全头。
- 真实页面上日桶出现多余横线：`.signal-sources li` 误匹配嵌套日桶，改为子选择器 `> li`。

## 6. 验证证据

| 命令 / 检查 | 结果 |
|---|---|
| `go test -race ./...`、`go vet ./...`、`gofmt -l` | 通过 |
| `npx vitest --run`、`npm run build` | 12 文件 31 测试通过；构建通过 |
| `make e2e` | Chromium 7/7 通过 |
| 真实 GitHub 采集（验收临时库） | 1 轮 13 个请求、写入 95 项观测、无错误；Stars 前 5 的新候选各写入 7 个日桶 |
| 浏览器检查 | 列表首行「GitHub ★ 17.4 万 / 来源本周 +7473」；全部未评分提示指向 `/settings`；详情显示 7 个日桶；跟踪后刷新仍为「取消跟踪」，测试后已恢复；375px 无横向溢出、无控制台错误 |

## 7. 产物与剩余限制

- 冷启动仍需约 24 小时的两次自有观测才会出现 score；本次只让首日已有可信的来源口径信息。
- 来源本周新增未用于排序；是否把它作为待积累组的次级信号，需要单独评估口径后再定。
- 跟踪入口只在详情页；设置页仍无跟踪列表管理。

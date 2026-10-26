# UX-4 采集来源进入分析与日报：agent-skills 过程记录

- 记录日期：2026-10-23（Asia/Shanghai）
- 任务来源：[GitHub Issue #35](https://github.com/GGGxie/TRadar/issues/35)（方案见该 Issue 评论）
- 状态：已完成，已合并到 main（PR #41，2026-10-26）
- 对应提交：`a95a6da`（后端主体）及本记录所在提交（界面、日报细节与文档）

## 1. 背景与边界

用户视角验收：规则初筛对采集项目提示「当前没有证据」，GitHub 描述和星数要人工再录；日报在全部未评分时
按 firstSeenAt + ID 选取，选中 0 星仓库而漏掉已收藏并做过实践的项目，条目只有标题，事实一律「尚无可引用事实证据」。

关键约束：来源记录会被采集**原地更新**，不能作为冻结引用；SPEC-project-catalog 规定 `METRIC_OBSERVATION`
只能由内部派生接口创建；SPEC-project-analysis 要求只用**明确选中**的证据，且不从星数推断需求或收入。

交付：
1. `POST /projects/{id}/evidence/from-sources` 把当前来源冻结为公开证据（描述/标题 + 最新累计指标，引用真实 observation ID），内容相同不重复。
2. 规则模板 `rules-v2`：来源描述 → WHAT/PRODUCT，指标 → ATTENTION/GROWTH（并追加「是否有真实使用或付费」待验证问题），其余位置不变。
3. 日报 `brief-selection-v2`：有评分 → 个人关注（已收藏或研究变化）→ 当前关注度 → firstSeenAt；条目增加简介；无证据时事实行写冻结的来源观测。
4. 界面：详情页「保存为证据」，证据列表随之刷新；日报显示简介，个人判断显示中文标签（界面与 Markdown）。

## 2. 采用的技能

| 技能 | 读取文件 | 应用 |
|---|---|---|
| `spec-driven-development` | `engineering/agent-skills/skills/spec-driven-development/SKILL.md` | 先把「来源可变、证据不可变」的取舍写进 Issue 评论，再同步三份规格 |
| `api-and-interface-design` | `engineering/agent-skills/skills/api-and-interface-design/SKILL.md` | 新接口幂等（Idempotency-Key）、新建 201 / 无新增 200；BriefItem 只加字段；规则与选择版本号递增，旧版本不重算 |
| `test-driven-development` | `engineering/agent-skills/skills/test-driven-development/SKILL.md` | 每片先写失败测试；部分在实现后写的测试用 stash 补取 RED |
| `incremental-implementation` | `engineering/agent-skills/skills/incremental-implementation/SKILL.md` | catalog → HTTP → analysis → brief → 界面逐片推进 |

## 3. 规格到实现切片

| 验收 | 实现 | 证据 |
|---|---|---|
| 派生证据：描述/标题、只取最新指标、公开、带 sourceRecordId、去重 | `catalog/source_evidence.go` | `TestDeriveSourceEvidenceFreezesDescriptionsAndLatestMetricsOnce`、`...WithoutSourcesCreatesNothing` |
| 派生接口幂等与状态码 | `httpapi/evidence.go` | `TestEvidenceFromSourcesHTTPIsIdempotentAndPublic` |
| rules-v2 分节、PROBLEM/FIT 仍未知、追加待验证问题 | `analysis/repository.go` | `TestRulesRunPlacesSourceDerivedEvidenceAsCitedFacts`；原 v1 测试不变仍通过 |
| 日报排序 v2 | `brief/selection.go`；`trends.LessProjectTrend` 抽出供复用 | `TestSelectItemsPrefersScoredThenPersonalThenAttention` |
| 简介、来源观测事实、不与简介重复 | `brief/selection.go` | `TestBriefItemCarriesSummaryAndFrozenSourceFactsWithoutEvidence` |
| Markdown 简介与判断标签 | `brief/markdown.go` | `TestMarkdownUsesOnlyFrozenDataAndEscapesUntrustedStructure` 扩充 |
| 界面 | `SignalPanel`、`EvidencePanel`（refreshKey）、`BriefPage`、`AnalysisPanel` 提示 | `SignalPanel.test.tsx`、`BriefPage.test.tsx` |

## 4. TDD 记录

- RED：catalog 测试编译失败（无 `DeriveSourceEvidence`）；HTTP 测试 404（路由不存在）；rules 测试显示所有证据堆在 ATTENTION、WHAT 仍是自述推测；
  brief 测试编译失败（无 `Summary`）；界面「保存为证据」按钮不存在；Markdown 缺简介行（stash `markdown.go` 后确认）。
- 真实页面发现两处问题后各自先补失败测试：日报事实与简介重复（`repeated summary fact=...`）；个人判断显示 `UNDECIDED`（Markdown 测试 `missing 个人判断：继续观察`）。
- GREEN：Go 全包 race 通过；Vitest 12 文件 33 测试通过。

## 5. 问题、根因与修正

- **日报首条与预期不符**：在已有当日日报的库上重新生成，首条是从未进过日报的项目，已收藏项目不在其中。
  根因不是排序：已收藏项目在前一版日报中出现过且没有新变化，按规格不再入选（去重正确）。换用清空日报历史的数据副本后，已收藏项目排在第一。
- **事实与简介重复**：派生的 GitHub 描述证据正文等于项目简介，日报两行相同。改为选择事实时跳过正文与简介相同的证据（证据仍完整冻结）。
- **判断显示枚举值**：日报界面与 Markdown 直接输出 `UNDECIDED`，改用与研究面板一致的中文标签。
- **排序实现**：最初用两元素切片调用 `SortProjectTrends` 间接比较，可读性差；改为在 trends 中导出 `LessProjectTrend`，雷达与日报共用同一组并列规则。
- **工具问题**：并行调用使测试或服务早于编辑/构建执行，多次出现「测试未运行」「端口占用」等假象；之后改为按顺序执行并核对二进制时间。

## 6. 验证证据

| 命令 / 检查 | 结果 |
|---|---|
| `go test -race ./...`、`go vet ./...`、`gofmt -l` | 通过 |
| `npx vitest --run`、`npm run build` | 12 文件 33 测试通过；构建通过 |
| `make e2e` | Chromium 7/7 通过 |
| 真实数据（新空库采集的 GitHub/HN） | 详情页保存为证据：新增 2 条（描述、Stars 58,348 / Forks 3,377）；规则初筛 WHAT 与 ATTENTION 为带引用事实，PROBLEM/FIT 仍未知，并出现「是否有真实使用或付费」问题；收藏后生成首日日报，该项目排第 1，后续按关注度排列，每条有简介与来源观测；Markdown 一致 |

## 7. 产物与剩余限制

- 派生证据需要用户点击「保存为证据」，不在采集时自动生成，避免每轮采集为全部项目新增证据并让日报不断出现「新增证据」。
- 指标证据每次累计值变化都会形成新证据；暂不自动标记旧指标证据为被替代。
- 规则模板仍不回答 PROBLEM / FIT，这两项需要人工或模型基于一手资料补充。

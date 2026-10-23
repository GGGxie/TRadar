# 第二轮试用修复（UX-7 至 UX-12）：agent-skills 过程记录

- 记录日期：2026-10-23（Asia/Shanghai）
- 任务来源：GitHub Issues [#44](https://github.com/GGGxie/TRadar/issues/44)–[#49](https://github.com/GGGxie/TRadar/issues/49)
- 状态：已完成，PR 待审阅
- 采用技能：`test-driven-development`、`incremental-implementation`、`debugging-and-error-recovery`（读取对应 `SKILL.md`）

第二轮在 #43 栈顶、全新空库上以用户身份试用，发现的问题逐个建 Issue，按严重程度依次修复。每节记录根因、测试与验证。

## #44 GitHub 搜索与普通接口限流额度混用

- **现象**：新库首次采集「部分成功」，`RATE_LIMITED：GitHub 历史请求停止`，日级历史 0 条；同时 GitHub `/rate_limit` 显示 core 55/60。
- **根因**：`recordProviderBudgetHeaders` 把每个响应的剩余额度写入同一个来源级字段，不看 `X-RateLimit-Resource`。
  搜索池（10/分钟）的 `remaining=0` 随后让日级历史等 core 池请求被判为无额度。第一轮能取到历史只是当时搜索池恰好还剩几次。
- **修正**：迁移 011 新增 `provider_budget_states(source, resource)`，旧的单一字段清空（无法判断归属，且一小时内过期）；
  请求按类别对应资源池（GitHub search/core，其余 default），响应头带 resource 时以响应为准；search 池被限后停止剩余搜索，
  但继续刷新跟踪仓库和日级历史；设置页展示 core 池。
- **TDD**：`TestGitHubSearchQuotaDoesNotBlockCoreRequests` 先失败（PARTIAL，历史未请求），实现后通过。
  第一版实现后它仍失败：搜索被限时执行器直接 `return`，连历史都不跑；改为跳出搜索循环。原有 provider 预算测试改用新签名。
- **真实验证**：新空库采集 GitHub：search 池剩余 0、core 池 49，任务「采集成功」，11 个请求，5 个仓库写入日级历史。

## #45 令牌模式下 Markdown 下载失败

- **现象**：开启 `TRADAR_ACCESS_TOKEN` 后点「下载 Markdown」，浏览器保存 `markdown.json`，内容为 401 错误。
- **根因**：下载是普通 `<a download>` 链接，浏览器导航不会附带 sessionStorage 中的 Bearer 令牌。
- **修正**：点击时用带令牌的请求取回文件，按 `Content-Disposition` 文件名以 Blob 保存；401 时清除令牌并回到解锁界面；保留 href 作为无脚本回退。
- **TDD**：`BriefPage.test.tsx`「downloads Markdown with the access token…」先失败，实现后 12 文件 36 测试通过。
- **真实验证**：令牌模式实例解锁后下载得到 `tradar-brief-2026-10-23-v6.md`，正文为 Markdown。

## #46 证据修订后新旧版本无法区分

- **现象**：「补充修订」后新旧两条证据外观相同、都能再修订；分析与实践的证据选择列出两版；日报事实可能引用旧版。
- **修正**：
  - `EvidenceFilter.CurrentOnly` 排除截止时刻前已被替代的版本；`GET /evidence?current=true` 暴露该过滤；读取结果附 `supersededBy`。
  - 已有新版本的证据不能再被修订（422 `supersedesId`），版本保持一条直线。
  - 日报冻结只取当前版本；分析、实践的证据选择请求 `current=true`。
  - 证据列表标「修订版」「已被新版本替代」，旧版不显示「补充修订」，正文改用次要文字色（不使用透明度，保持对比度）。
- **TDD**：catalog、brief、HTTP、组件四个新测试均先失败后通过。全量运行时 #44 的测试在 `-race` 下偶发失败：
  设置页预算视图读取真实时钟，而测试用固定时间，重置时间已过即为空；改为基于当前时间构造。
- **真实验证**：对真实来源证据做一次修订后，列表标出旧版、只剩一个修订入口；规则初筛选择从 3 项变为 2 项；axe 无违规。
- **工具问题**：本节多次出现「编辑或写文件延迟数秒才落盘」「服务进程早于二进制写完启动」，导致测试或页面看到旧代码；
  之后每次先核对文件内容与二进制时间再判断结果。

## #47 过期退避提示、24 小时窗口提示与文案

- 设置页「来源退避至」只在时间未到时显示（组件测试：过去与未来各一条，只显示一条）。
- 雷达提示改为「相隔 24–26 小时」，并说明手动采集需在明天同一时间后两小时内，或开启每小时一次的定时采集。
  依据：24h 窗口取不晚于 `t1−24h`、容差 2 小时的样本（SPEC-trend-ranking）；第二轮用模拟样本确认相隔 23:59:57 不评分。
- 趋势解释使用「GitHub Stars」等显示名（`ranking_test` 断言，stash 实现后失败）。
- 列表评分显示「趋势分 50.0」，净增标注所选窗口（组件测试）。
- 验证：Go 全包 race；Vitest 13 文件 39 测试；构建通过。

## #48 研究节奏建议与下一步时间

- **问题**：研究偏好（每日收藏 3 / 每周深研 2 / 每月实践 1）保存后无处显示；认知库卡片不显示下一步时间，到期无提示。
- **口径**（先写在 Issue 评论中）：规格禁止把阶段编辑次数当研究统计，因此只统计有时间戳的事实——
  今日收藏用新增的 `starred_at`（未收藏→收藏时写入，取消收藏清空；旧收藏为 NULL 不计）；本周深研统计人工或模型 DEEP 版本；
  本月实践统计新建的实践记录。真实浏览器复核时发现一次规则深研也计入了「本周深研」，补测试后排除 `origin=RULES`。
- **接口**：`GET /research-rhythm?timeZone=`，非法时区与 `Local` 返回 422。
- **界面**：认知库顶部显示「进度 / 建议」，建议为 0 的项隐藏，超过建议提示「只是参考」；读取失败不影响认知库。
  卡片显示「计划时间」或「已到期」。
- **TDD**：`TestRhythmCountsOnlyTimestampedResearchFacts`（stash 实现后编译失败；加入规则深研用例后再次失败）、
  `TestResearchRhythmHTTPValidatesTimeZone`、`LibraryRhythm.test.tsx` 均先失败后通过。
- **真实验证**：第二轮库收藏一个新项目后「今日收藏」0/3 → 1/3；10/20 的下一步显示「已到期 · 10/20 09:00」；axe 无违规。
## #49 日报重新生成的比较基准与证据全选
- **问题**：同日重新生成的版本 2 往往只有一两项甚至为空，看起来像「日报丢了内容」；分析证据选择只能逐项勾选。
- **范围决策**：同日新版本以上一版本为比较起点是 DB-03 / 选择规则 2 的既定行为，不改选择逻辑，只在界面说明基准。
- **界面**：每个日报版本显示比较基准——无 `previousBriefId` 为「首份日报：收录截止前 24 小时内…」；
  同日为「本版只列出版本 N 之后的新变化；版本 N 中的项目仍保留在下方」；跨日写明上一份日报日期与版本。
  证据选择区在可选项多于 1 条时提供「全选」「清空」；模型模式下全选只选公开证据。
- **TDD**：`BriefPage.test.tsx`「explains which version each regenerated brief is compared against」、
  `AnalysisPanel.test.tsx`「selects or clears all selectable evidence at once」均先失败后通过。
- **真实验证**：第二轮库 6 个同日版本依次显示「版本 N−1 之后」，版本 1 显示首份说明；
  有 2 条当前证据的项目「清空」0/2、「全选」2/2；两个页面 axe 无违规。
- **剩余限制**：同日重新生成仍只列新变化，这是规格行为；如需「完整重排」需要单独提需求。

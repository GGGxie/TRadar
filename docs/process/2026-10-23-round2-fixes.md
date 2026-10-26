# 第二轮试用修复（UX-7 至 UX-12）：agent-skills 过程记录

- 记录日期：2026-10-23（Asia/Shanghai）
- 任务来源：GitHub Issues [#44](https://github.com/GGGxie/TRadar/issues/44)–[#49](https://github.com/GGGxie/TRadar/issues/49)
- 状态：进行中
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

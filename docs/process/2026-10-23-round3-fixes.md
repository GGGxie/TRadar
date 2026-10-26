# 第三轮试用修复（UX-13 至 UX-17）：agent-skills 过程记录
- 记录日期：2026-10-23（Asia/Shanghai）
- 任务来源：GitHub Issues [#56](https://github.com/GGGxie/TRadar/issues/56)–[#60](https://github.com/GGGxie/TRadar/issues/60)
- 状态：已完成，PR 待审阅
- 采用技能：test-driven-development、incremental-implementation、frontend-ui-engineering
- 试用环境：第二轮库的副本 `/tmp/tradar-uat/round3.db`，端口 18086；不使用 `data/radar.db`。

## 试用发现
- 空白人工深研可直接保存并计入「本周深研」（#56）。
- 只有下一步时间、没有行动文字时，同时显示「尚未安排下一步」与「计划时间 / 已到期」（#57）。
- 日报截止与生成时间显示为 UTC 原始时间戳（#58）。
- 日报按最多项目截断候选时没有说明（#59）。
- 小问题合集：版本全部展开、FACT 错误不指明位置、过期的入队提示、来源链接换行、链接搜索 0 条（#60）。
- 顺带修正：#49 新增的比较基准说明没有卡片内边距，已在 PR #55 追加提交 `1901065`，桌面和手机与标题左对齐。

## #56 空白人工分析版本
- **范围决策**：规格允许保存不完整资料，因此只拒绝「完全空白」：全部声明为 UNKNOWN 且正文等于表单预置的
  `尚未形成判断。`，且没有待验证问题与下一步检查。用户自己写的 UNKNOWN 说明（如「还没找到定价信息」）算内容。
  只对 MANUAL 生效，规则 / 模型生成不受影响；已有空版本不删除。
- **实现**：`analysis.UnknownPlaceholder` 与 `hasAuthoredContent`，`createVersion` 对 MANUAL 返回 422 `content`；
  前端用同一判定在发请求前提示，草稿保留。
- **TDD**：`TestManualAnalysisRequiresAuthoredContent` 先因常量未定义编译失败，再把判定临时置为 false 确认行为失败
  （`empty manual version error=<nil>`），恢复后通过；组件测试「blocks a blank manual version…」先失败
  （空表单发出了 POST）后通过。
- **真实验证**：空白表单保存显示提示、版本数不变；只写「商业模式」一条后保存成功（0 → 1）；接口直接提交空白内容返回 422。
- **工具问题**：服务进程又一次比二进制早 2 秒启动，第一次接口验证跑的是旧程序并写入了一个空版本（仅临时库）；
  重启后核对启动时间再验证。

## #57 只有下一步时间时的文案
- **范围决策**：规格允许 nextAction 为空、nextActionAt 有值，因此不加校验，只修正显示并在表单中提示。
- **实现**：`nextActionText()` 供详情页与认知库共用；有时间无文字显示「已定时间，尚未写具体行动。」。
  表单提示放在 label 外并通过 `aria-describedby` 关联——第一次放在 label 内导致输入框的可访问名称变成
  「下一步行动 已设置…」，组件测试按标签找不到输入框，据此调整。
- **TDD**：`ResearchPanel.test.tsx`「describes a scheduled next step…」与 `LibraryRhythm.test.tsx` 新断言先失败后通过。
- **真实验证**：认知库两张卡片分别显示「已到期 · 10/20 09:00」「计划时间 · 10/24 10:00」并配新文案；
  详情页同样；编辑时出现提示，输入行动后提示消失；axe 无违规。

## #58 日报时间按时区显示
- **实现**：卡片「截止 10/23 17:17（Asia/Shanghai）」按版本自己的 timeZone 换算，原值放在 `<time dateTime>`；
  Markdown 的数据截止、生成时间、来源最近采集按时区显示到分钟，数据截止附 `UTC 2026-10-23T09:17:26Z`。
  时区无法加载或时间无法解析时原样输出。接口字段与存储不变。
- **TDD**：`TestMarkdownShowsTimesInBriefTimeZone`、`BriefPage.test.tsx`「shows the cutoff in each brief's own time zone」
  （同页 Asia/Shanghai 与 America/Los_Angeles 两个版本）先失败后通过。
- **真实验证**：卡片显示「截止 10/23 17:17（Asia/Shanghai）」；下载的 Markdown 为
  「2026-10-23 17:17（Asia/Shanghai；UTC 2026-10-23T09:17:26Z）」。
- **工具问题**：构建与启动服务放在同一批调用，服务再次早于二进制 1 秒启动，第一次 Markdown 仍是旧格式；
  分开两轮后结果正确。

## #59 日报截断说明（2026-10-26 继续）
- **实现**：`SelectItemsWithOmitted` 与原规则共用排序和截断，另返回未收录数；`SelectItems` 保持原签名。
  迁移 013 为 `daily_briefs` 增加 `omitted_item_count`（默认 0，旧版本不回填），`DailyBrief.omittedItemCount` 随版本冻结。
  截断时收录数恰等于当时上限，因此说明中的上限 M 用收录数表示，无需另存设置。
  站内在项目列表前显示「另有 N 个符合条件的项目因上限 M 未收录…」，Markdown 以引用行输出。
- **TDD**：`TestSelectionCountsCandidatesOmittedByMaxItems`、`TestBriefRecordsCandidatesOmittedByMaxItems`
  （含 Markdown 断言）先因函数 / 字段未定义编译失败后通过；`BriefPage.test.tsx`「says how many qualifying projects…」先失败后通过。
- **真实验证**：临时库升级到 schema 13，原 7 个版本未收录数为 0 且不显示说明；上限设为 1 生成 2026-10-26 版本 1：
  收录 1 项，显示「另有 25 个…因上限 1 未收录」，Markdown 同样；axe 无违规；验证后上限改回 2。

## #60 小问题合集
- **版本折叠**：每页第一个版本展开，其余折叠为标题行与比较基准，按钮「展开本版（N 个项目）」带
  `aria-expanded` / `aria-controls`；折叠内容用 `hidden` 保留在 DOM 中，e2e 依旧能从旧版本卡片取下载链接。
  第三轮库 8 个版本，页面高度 6607 → 3048px。
- **FACT 定位**：保存前检查，提示「「增长信号」声明 1 标为事实，需要至少引用一条证据；也可以改为推测。」，不发请求；后端校验不变。
- **入队提示**：来源卡片的 activeRun 消失后清除「采集任务已进入队列」（组件测试让轮询返回 SUCCEEDED）。
- **来源链接**：`.brief-item-heading > a` 不换行、不收缩，桌面高度 16px（一行）。
- **链接搜索**：`searchLink` 对带点的搜索词补 https 后用 `NormalizeURL` 规范化，匹配 canonical_url 或 project_aliases，
  同时保留名称 / 简介的文字匹配；只做本地字符串处理，不访问链接。
- **TDD**：`TestListProjectsMatchesPastedLinks`、`SettingsPage.test.tsx` 新断言、`AnalysisPanel.test.tsx`「names the section…」、
  `BriefPage.test.tsx`「expands only the newest version…」均先失败后通过。
- **真实验证**：上述各项在第三轮库逐一复核，axe 无违规；`make e2e` 7/7。
- **工具问题**：本节中途前台 shell 工具连续无响应约 10 分钟，改为把命令写进脚本、由后台进程执行并把结果写入文件读取；
  恢复后改回前台执行。

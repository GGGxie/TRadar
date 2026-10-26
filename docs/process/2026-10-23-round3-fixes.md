# 第三轮试用修复（UX-13 至 UX-17）：agent-skills 过程记录
- 记录日期：2026-10-23（Asia/Shanghai）
- 任务来源：GitHub Issues [#56](https://github.com/GGGxie/TRadar/issues/56)–[#60](https://github.com/GGGxie/TRadar/issues/60)
- 状态：进行中
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

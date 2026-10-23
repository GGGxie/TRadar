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

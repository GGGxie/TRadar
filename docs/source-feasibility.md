# 信息源接入调研

核实日期：2026-10-13。以下是官方文档调研结果及工程建议；尚未完成运行时接口联调。
平台字段、配额和访问条件可能变化，实现时需要用实际响应验证。

## GitHub

官方 REST 可搜索仓库并读取累计 Stars 等指标。匿名常规配额通常为每小时 60 次，
个人 Token 通常为每小时 5,000 次；搜索独立限额，匿名每分钟 10 次、认证每分钟 30 次。
单次查询最多覆盖 1,000 个搜索结果，需关注不完整结果标记和实际限流响应头。
依据：[搜索接口](https://docs.github.com/en/rest/search/search#search-repositories)、
[速率限制](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api)。

官方已提供 `GET /repos/{owner}/{repo}/stargazers/history`：按自然周分页，
包含逐日新增 Stars，公开资源可匿名请求。日/周边界不保证与 UTC 对齐，
因此不能当成滚动 24 小时净增。建议用于日级冷启动；持续采样用于自己的时间窗口。
依据：[Star 历史接口](https://docs.github.com/en/rest/activity/starring#get-repository-star-history)。

本次未发现公开官方 Trending API。首版采用 REST 候选发现，不称为“官方 Trending”。
2026 年官方公布 Star 用户列表访问限制，故不依赖遍历任意仓库的用户列表重建历史。
依据：[Trending 网页](https://github.com/trending)、
[官方变更公告](https://github.blog/changelog/2026-06-30-upcoming-access-restrictions-to-public-api-endpoints-and-ui-views/)。

## Hacker News

官方 Firebase API 可读取 `topstories`、`newstories`、`showstories` 和单个 item，
无需用户 Token；文档当前称无限流，仍建议限制并发、缓存与退避重试。
Top/New 最多 500 项，Show 最多 200 项。观察分数、评论数和榜单位置，
同时处理 deleted、dead、null 项及文章无站外 URL 的情况。
依据：[官方 API 文档](https://github.com/HackerNews/API)。

Algolia 提供 HN 文本与时间搜索，适合补充项目历史讨论；公开限额为每 IP 每小时 10,000 次。
它是搜索索引，可能存在同步延迟，来源应单独标明。
依据：[Algolia HN Search API](https://hn.algolia.com/api)。

## Product Hunt

官方 GraphQL 需要 access token，可用账户绑定的 developer token 或相应 OAuth 流程。
可观测发布、投票、评论等表现；GraphQL 文档配额为每 15 分钟 6,250 complexity points，
实现应以响应头为准。官方文档要求商业用途另行联系授权。
首版做条件启用的适配器；缺少凭据时显示未启用，不用演示数据替代失败的抓取。
依据：[认证和用途条件](https://api.producthunt.com/v2/docs)、
[限额](https://api.producthunt.com/v2/docs/rate_limits/headers)、
[Post 字段](https://api-v2-docs.producthunt.com/object/post/)。

## Indie Hackers 与访谈材料

本次未核实到有当前官方文档支持的社区项目 API 或 RSS；这不等于断言其不存在。
旧有功能讨论和第三方采集器不足以支撑稳定接入承诺。
建议先人工录入故事链接、摘录、日期和收入声明类型，再用于商业分析。
官网：[Indie Hackers](https://www.indiehackers.com/)。

## 对系统边界的影响

这是基于上述来源的工程判断：

- GitHub REST 与 HN Firebase 作为无需购买服务的核心接入；PH 依赖凭据与使用条件。
- 区分累计关注度、来源日级新增、本系统观测区间净变化，不混合不同统计口径。
- 证据不足显示未知；缺失或失败不填零。异常增长只代表值得研究，不证明商业成功。
- 跨源归并依靠可确认的 URL / 仓库身份；名字相似只能作为候选关系。
- 收入与付费证据保留声明主体、时间和来源等级，不能由 Stars 或投票量推算为事实。

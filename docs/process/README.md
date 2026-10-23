# agent-skills 工程过程记录

这个目录记录 TRadar 在实际研发中如何使用
[addyosmani/agent-skills](https://github.com/addyosmani/agent-skills)。它面向想学习工程方法的读者，
补充规格、GitHub Issues 和验证结果之间通常看不到的执行过程。

## 如何阅读

每篇记录对应一个任务或一个可独立验证的实现切片，按下面的顺序组织：

1. **背景与边界**：本次要交付什么，明确不扩大哪些范围。
2. **技能选择**：使用的技能、选择原因和实际读取的 `SKILL.md`。
3. **规格到切片**：验收条目如何拆成小步以及每一步的完成证据。
4. **RED / GREEN / REFACTOR**：先失败的测试、最小实现和后续整理；纯文档工作注明不适用。
5. **问题与修正**：可复现现象、根因、修复与防回归手段。
6. **验证与产物**：实际运行的命令、结果、提交和仍存在的限制。

过程记录只写可观察的行动、决策依据和证据。它不会保存模型私有思维过程，也不会收录
访问令牌、个人敏感信息或来源页面中的大段不可信原文。

## 记录索引

| 任务 | 记录 | 状态 |
|---|---|---|
| [T16 日报选择与版本](https://github.com/GGGxie/TRadar/issues/20) | [`2026-10-19-t16-daily-brief-core.md`](2026-10-19-t16-daily-brief-core.md) | 已完成，补记 |
| [T17 日报交付](https://github.com/GGGxie/TRadar/issues/21) | [`2026-10-19-t17-daily-brief-delivery.md`](2026-10-19-t17-daily-brief-delivery.md) | 已完成 |
| [T18 可选模型](https://github.com/GGGxie/TRadar/issues/22) | [`2026-10-19-t18-optional-model.md`](2026-10-19-t18-optional-model.md) | 已完成 |
| [T19 集成与安全](https://github.com/GGGxie/TRadar/issues/23) | [`2026-10-19-t19-integration-security.md`](2026-10-19-t19-integration-security.md) | 已完成 |
| [T20 可运行交付](https://github.com/GGGxie/TRadar/issues/24) | [`2026-10-19-t20-delivery.md`](2026-10-19-t20-delivery.md) | 已完成 |
| [检查点 D](https://github.com/GGGxie/TRadar/issues/25) | [`2026-10-19-checkpoint-d.md`](2026-10-19-checkpoint-d.md) | 已完成 |
| [GitHub Issues 任务看板迁移](https://github.com/GGGxie/TRadar/issues/27) | [`2026-10-19-github-issues-migration.md`](2026-10-19-github-issues-migration.md) | 已完成 |
| [Cloudflare Tunnel 临时运行](https://github.com/GGGxie/TRadar/issues/28) | [`2026-10-20-cloudflare-tunnel-launch.md`](2026-10-20-cloudflare-tunnel-launch.md) | 已完成，临时服务运行中 |
| [浏览器访问令牌闭环](https://github.com/GGGxie/TRadar/issues/29) | [`2026-10-20-browser-access-token.md`](2026-10-20-browser-access-token.md) | 进行中，PR 待审阅 |
| [UX-1 当前关注度与来源展示](https://github.com/GGGxie/TRadar/issues/32) | [`2026-10-23-ux1-current-attention.md`](2026-10-23-ux1-current-attention.md) | 已完成，PR 待审阅 |
| [UX-2 冷启动可用](https://github.com/GGGxie/TRadar/issues/33) | [`2026-10-23-ux2-cold-start.md`](2026-10-23-ux2-cold-start.md) | 已完成，PR 待审阅 |

## 后续记录模板

```markdown
# Txx 标题：agent-skills 过程记录

- 记录日期：YYYY-MM-DD（Asia/Shanghai）
- 任务来源：GitHub Issue #n
- 状态：进行中 / 已完成
- 对应提交：进行中 / `<commit>`

## 1. 背景与边界
## 2. 采用的技能
## 3. 规格到实现切片
## 4. TDD 记录
## 5. 问题、根因与修正
## 6. 验证证据
## 7. 产物与剩余限制
```

新任务第一次采用 agent-skills 时创建记录；发生重要失败、返工、范围决策或验证后及时追加，
不要等到任务结束才凭记忆补写。

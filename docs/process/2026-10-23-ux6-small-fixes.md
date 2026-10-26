# UX-6 体验小问题合集：agent-skills 过程记录

- 记录日期：2026-10-23（Asia/Shanghai）
- 任务来源：[GitHub Issue #37](https://github.com/GGGxie/TRadar/issues/37)
- 状态：已完成，已合并到 main（PR #43，2026-10-26）
- 对应提交：`6157a94`（后端）及本记录所在提交（界面与文档）

## 1. 背景与边界

用户视角验收列出的 8 个小问题，外加 #34 记录的「采集任务不显示跳过数」。逐项修复并验证；
其中「无研究记录时 GET research 返回 404」是 SPEC-research-workspace 明确的接口约定（尚无记录 404，UI 显示未保存状态），本次不改。

## 2. 采用的技能

`test-driven-development`、`frontend-ui-engineering`（读取文件同前几篇记录）：每项先写失败测试或先截图复现，修复后再截图确认。

## 3. 逐项处理

| 问题 | 处理 | 证据 |
|---|---|---|
| 空库启动即生成空日报 v1 | 调度器在目录为空时不生成、不占用当天；有数据后的下一次 tick 生成。手动生成仍得到 NO_DATA | `TestSchedulerWaitsForCatalogDataBeforeClaimingTheDay`；旧调度测试改为先建项目 |
| 重复 URL 录入无提示 | POST 返回 200 时 `saveProject` 标记 `existing`；跳转详情页后显示「链接已在目录中…没有保存」，并清掉地址参数 | `ProjectForm.test.tsx` 新增用例；浏览器复核 |
| GitHub `/tree/<sha>` 未归一化 | `/owner/repo/tree|blob/<ref>/…` 归一到仓库根；issues 等子页面不变 | `url_test.go` 新增 5 例（stash 取得 RED） |
| HN 标题「Show HN:」前缀 | 新建项目名去掉 Show/Launch HN 前缀；来源记录保留原标题；Ask/Tell HN 不处理 | `TestHackerNewsProjectNameDropsLaunchPrefixes` |
| 雷达筛选刷新丢失 | 搜索、方向、来源、窗口、页码写入地址栏并在加载时恢复 | `ProjectList.test.tsx` 新增用例；刷新后仍为 AI Agent + agent |
| research 404 控制台错误 | 规格约定，保留 | — |
| 手机端标题词中断行 | 标题在「/」后插入 `<wbr>`，优先在此换行 | 375px 截图：`DietrichGebert/` + `ponytail` |
| HN 榜单复选框无间距 | `.feed-field .toggle-field` 补 `display:flex`，`gap` 生效（8px） | 截图 |
| 采集任务不显示跳过数 | 任务行显示「· 跳过 N 条」 | 浏览器：HN「28 条 · 56 项观测 · 跳过 32 条」 |

## 4. 问题与修正

- 改 `onSaved` 签名后旧的 ProjectForm 测试因多了第二个参数失败，按新约定更新期望。
- `saveProject` 两个分支返回类型不同导致 `tsc` 报错，显式声明返回 `Resource<Project> & { existing?: boolean }`。
- 新增 `skippedItems` 字段后设置页测试夹具缺字段，`tsc` 报错后补齐。

## 5. 验证证据

| 命令 / 检查 | 结果 |
|---|---|
| `go test -race ./...`、`go vet ./...`、`gofmt -l` | 通过 |
| `npx vitest --run`、`npm run build` | 12 文件 35 测试通过；构建通过 |
| `make e2e` | Chromium 7/7 通过 |
| 浏览器复核（真实采集库） | 上表各项；375px 无横向溢出 |

## 6. 剩余限制

- 已存在的历史项目名称和 URL 不回填（例如旧库里已有的「Show HN: …」项目名）。
- research 404 的控制台日志仍会出现，属于接口约定。

# GitHub Issues 任务看板迁移：agent-skills 过程记录

- 记录日期：2026-10-19—2026-10-20（Asia/Shanghai）
- 任务来源：[GitHub Issue #27](https://github.com/GGGxie/TRadar/issues/27)
- 状态：已完成
- 对应提交：本记录所在提交

## 1. 背景与边界

首版开发在原任务清单留下 T00–T20 和检查点 A–D，共 25 个已完成事项；
原实施计划记录了范围、实现切片、风险与任务跟踪决策。本次迁移的目标是：

1. 将这 25 个事项逐项建立为 GitHub Issue，并以 `completed` 原因关闭；
2. 将首版计划建立为 1 个总计划 Issue，并关联历史事项；
3. 以 GitHub Issues 作为后续唯一任务看板；
4. 远端记录验证完成后删除本地任务目录，同步仓库入口、工程约定和过程记录中的引用。

迁移不改产品行为，不重新解释历史完成状态，也不把尚未发生的公网发布或付费服务调用写成已完成。
为了避免迁移中断造成记录丢失，在远端 Issues 创建成功前不删除原任务文件。

## 2. 采用的技能

### `git-workflow-and-versioning`

- 读取文件：`engineering/agent-skills/skills/git-workflow-and-versioning/SKILL.md`
- 采用原因：本次会删除旧跟踪文件、修改多处仓库入口并形成一个可回滚提交。
- 实际应用：先检查分支和工作区；将远端迁移、引用更新、验证和提交作为一个逻辑变更；
  提交前检查差异、敏感信息和失效链接。

### `documentation-and-adrs`

- 读取文件：`engineering/agent-skills/skills/documentation-and-adrs/SKILL.md`
- 采用原因：任务真源从仓库内 Markdown 改为外部 Issues，会影响后续人员和代理理解工作流。
- 实际应用：记录选择 GitHub Issues 的原因、迁移顺序、失败恢复方式和后续工程约定；
  保留历史语境，避免只删除文件却不解释决策。

本次没有新建 ADR。项目已有 `docs/process/` 约定，而任务跟踪工具切换可以在工程约定和本过程记录中完整表达，
单独引入新的决策文档体系会造成重复。

## 3. 从历史清单到迁移切片

迁移按以下可验证切片执行：

1. **盘点**：读取原任务清单和计划，确认 21 个 T 任务、4 个检查点和 1 份实施计划。
2. **查重**：查询 `GGGxie/TRadar` 的 Issues；迁移前为 0 条，未发现重复项。
3. **准备**：生成包含 26 个 Issue 标题和正文的临时清单；历史任务正文保留目标、验证和文件边界。
4. **远端写入**：先创建并关闭 25 个历史事项，再创建带实际链接的总计划 Issue 并关闭。
5. **本地切换**：远端验证后删除原任务目录，把 README、能力图、规格入口、验证文档、
   `AGENTS.md` 和过程记录索引改为 GitHub Issues。
6. **收尾验证**：用 Issue #27 跟踪迁移本身；确认 Issues 数量与关闭原因、检查仓库内无失效的旧任务路径引用，
   检查 Git 差异后提交，再回填提交证据并以 `completed` 关闭 #27。

这个顺序把不可逆的数据丢失风险放在远端验证之后。创建过程中如果中断，恢复时先按标题搜索，
只补缺失项，避免重复 Issue。

## 4. TDD / 验证策略

本次不改变业务逻辑，RED/GREEN 单元测试循环不适用。采用状态迁移验证：

- **初始断言**：GitHub 搜索返回 0 个 Issue；仓库 `has_issues=true`。
- **写入探测**：尝试创建 `[T00] 规格协调`，GitHub API 返回 403
  `Resource not accessible by integration`，因此没有创建任何 Issue。
- **完成断言**：26 个历史 Issue 全部关闭且关闭原因为 `completed`；迁移任务 #27 在提交证据回填后同样以
  `completed` 关闭；仓库内不再保留第二份任务状态清单。

文档变更完成后执行 `git diff --check` 和全仓引用搜索。产品代码未改变，因此不重复运行与本次风险无关的
Go、前端或浏览器测试。

## 5. 问题、根因与恢复

### GitHub Issues 写入被拒绝

- 现象：读取仓库、权限和 Issue 搜索正常；创建 Issue 返回 403。
- 排除项：仓库未归档、未禁用并已启用 Issues；当前 GitHub 身份是 `GGGxie`，仓库权限显示 `admin=true`。
- 根因：当前 GitHub 连接没有可用的 App installation，连接器令牌缺少该仓库的 Issues 写权限。
- 本机回退检查：没有 `gh`，没有 Git credential helper、`GH_TOKEN`、`GITHUB_TOKEN`、gh 配置或 `.netrc`。
- 恢复方式：为 ChatGPT/Codex 的 GitHub 连接重新授权 `GGGxie/TRadar`，授予 **Issues: Read and write**；
  然后从“查重”步骤恢复迁移。

### Codex CLI 插件连接诊断

- 当前 Codex CLI 为 `0.163.0`，登录状态是 `Logged in using ChatGPT`。
- 已安装并启用官方 `github@openai-curated` 插件，安装版本为 `11c74d6b`。
- 新会话可以调用 GitHub 读取工具并确认仓库仍为 0 个 Issue，但连接器仍返回 0 个 installation、
  0 个已连接账号；公开读取不代表已获得写权限。
- 初次检查时 `codex mcp list` 没有用户级 MCP server，说明插件的 App Connector 没有在 CLI 中完成账号连接。
- 曾按 GitHub 官方 Codex 安装方案临时添加用户级 `github` MCP，认证只引用
  `GITHUB_PAT_TOKEN` 环境变量。找到官方 GitHub App 安装入口后已移除该临时配置，避免要求用户维护 PAT。

2026-10-19 再次实测：插件能返回 `GGGxie` 用户资料、`GGGxie/TRadar` 仓库以及仓库的
`admin/push/triage/pull` 权限，说明读取链路正常；但当前 Codex 进程中 `GITHUB_PAT_TOKEN` 为未设置，
App Connector 仍为 0 个 installation 和 0 个账号连接。以真实迁移项 `[T00] 规格协调` 作为写入探测时，
创建操作仍返回 403 `Resource not accessible by integration`，远端没有产生 Issue。

进一步尝试 `codex mcp login github --no-browser` 生成 OAuth 链接时，GitHub 托管 MCP 返回
`Dynamic client registration not supported`。随后确认身份授权与 GitHub App 仓库安装是两个独立步骤；
本仓库缺少后者。改用官方 ChatGPT Codex Connector 安装入口
`https://github.com/apps/chatgpt-codex-connector/installations/new`，由用户在 GitHub 页面选择
`GGGxie/TRadar`。这条路径无需在聊天中传递 PAT。

- 再次验证时，插件能识别 `GGGxie` 并读取 `GGGxie/TRadar`，仓库权限响应包含
  `admin/push/triage/pull`；Issue 搜索仍为 0 条。创建计划内的 `[T00]` Issue 仍返回 403，
  没有留下远端记录。当前 Codex 进程检查结果为 `GITHUB_PAT_TOKEN=unset`。

### 安装恢复与批量迁移

- 用户通过官方链接安装 ChatGPT Codex Connector 后，连接器返回 installation `172959256`、
  账号 `GGGxie` 和 Issues 事件订阅，写入 `[T00] 规格协调` 成功。
- 首次成功响应使用 `issue_number` 字段而不是预期的 `number`，所以创建后关闭没有在同一脚本中触发；
  修正字段兼容后单独关闭 #1，随后所有条目均按“创建→以 `completed` 关闭”顺序处理。
- [Issues #1–#25](https://github.com/GGGxie/TRadar/issues?q=is%3Aissue+is%3Aclosed) 保存 T00–T20
  和检查点 A–D；[Issue #26](https://github.com/GGGxie/TRadar/issues/26) 保存首版实施计划并链接全部子 Issue。
- 历史迁移完成时搜索得到 26 个关闭 Issue、0 个开放 Issue；随后创建 #27 跟踪本次迁移的本地收尾，
  远端历史记录验证结束后才删除原任务文件并切换本地引用。

没有通过降低校验、伪造链接或先删除本地清单来绕过该失败。

## 6. 当前证据

| 检查 | 结果 |
|---|---|
| 当前分支 | `feature/tradar-core` |
| 迁移前 Issues | 0 |
| 仓库 Issues 功能 | 已启用 |
| 当前用户仓库权限 | admin / maintain / push / triage / pull |
| 初次创建探测 | 403，未产生远端记录 |
| 读取探测 | 正常，可识别用户、仓库及仓库权限 |
| 本地替代凭据 | 未发现 |
| Codex GitHub 插件 | 已安装并启用；App Connector 已连接 `GGGxie` |
| GitHub App installation | `172959256`，Issues 事件已启用 |
| 用户级 GitHub MCP | 临时 PAT 配置已移除，改用 GitHub App 安装 |
| 历史迁移结果 | [#1–#26](https://github.com/GGGxie/TRadar/issues?q=is%3Aissue+is%3Aclosed) 全部关闭 |
| 迁移任务 | [#27](https://github.com/GGGxie/TRadar/issues/27)，提交证据回填后以 `completed` 关闭 |
| 临时迁移清单 | 仅存在于临时目录，不提交且不含凭据 |

## 7. 完成条件与剩余限制

26 个历史 Issue 和迁移任务 #27 均可从 GitHub 读取；#27 完成后仓库共有 27 个已关闭 Issue、
0 个开放 Issue。旧任务路径引用已清理，本地任务目录已删除。后续任务只在
GitHub Issues 建立和更新；规格、验收矩阵、验证记录及本目录继续保存行为基线和工程证据。

安装状态显示 `repository_selection=all`，范围大于本次只操作 `TRadar` 的需要。用户可在
[GitHub App 设置](https://github.com/settings/installations/172959256) 将仓库访问范围收紧为仅 `TRadar`；
这不影响已完成的迁移记录。

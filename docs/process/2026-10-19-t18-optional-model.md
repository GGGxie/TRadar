# T18 可选模型：agent-skills 过程记录

- 记录日期：2026-10-19（Asia/Shanghai）
- 任务来源：`tasks/todo.md` 的 T18，`SPEC-project-analysis.md`
- 状态：已完成
- 实现与验收提交：见第 7 节（按领域、HTTP 接入、界面、文档拆分）

## 1. 背景与边界

T18 为既有人工/规则分析增加可选模型路径。默认状态必须保持关闭；没有完整配置时不能调用、
联网或消耗额度。实现只做一次受限文本生成：冻结明确选择的公开证据和趋势，要求结构化 JSON，
本地再次校验 schema 与证据引用，然后新增不可变 MODEL 分析版本。

模型没有浏览、函数、文件、代码执行或其他工具权限。私密证据、个人研究正文、环境变量和凭据
不得进入提示。真实付费调用不属于自动验证，全部验收使用可控假提供者。

## 2. 采用的技能

| 技能 | 采用原因 | 实际读取文件 |
|---|---|---|
| incremental-implementation | 按配置/提供者、冻结任务、预算、运行时、HTTP 接入分片 | `engineering/agent-skills/skills/incremental-implementation/SKILL.md` |
| test-driven-development | 预算、超时、非法引用和失败不发布都需要先失败的证明 | `engineering/agent-skills/skills/test-driven-development/SKILL.md` |
| api-and-interface-design | 要稳定 provider、run、usage、capabilities 与 HTTP 边界 | `engineering/agent-skills/skills/api-and-interface-design/SKILL.md` |
| security-and-hardening | 模型输入/输出均为不可信数据，并涉及秘密与外部请求 | `engineering/agent-skills/skills/security-and-hardening/SKILL.md` |
| source-driven-development | OpenAI Responses 与 Structured Outputs 字段会随 API 演进 | `engineering/agent-skills/skills/source-driven-development/SKILL.md` |
| debugging-and-error-recovery | 浏览器验收出现移动端溢出，需要保留证据、稳定复现并定位元素 | `engineering/agent-skills/skills/debugging-and-error-recovery/SKILL.md` |
| code-review-and-quality | 完成后按正确性、可读性、架构、安全、性能五轴审查，并用变异验证测试有效性 | `engineering/agent-skills/skills/code-review-and-quality/SKILL.md` |

## 3. 官方资料核对

检测到项目使用 Go 1.25 标准库 HTTP 客户端，不新增模型 SDK。实现采用固定 OpenAI HTTPS 端点，
只在用户显式配置 provider、model、API key、输出上限和每日预算后启用。

- [Create a model response](https://developers.openai.com/api/reference/resources/responses/methods/create)：
  核对 `POST /v1/responses`、`max_output_tokens`、`store`、`tool_choice`、`text.format`、
  REST `output[].content[]`、refusal 与 usage 字段。审查时进一步确认 `output_text` 是 SDK
  便捷属性，不能假定原始 HTTP JSON 一定含有该字段。
- [Structured model outputs](https://developers.openai.com/api/docs/guides/structured-outputs)：
  核对 `type=json_schema`、`strict=true`、所有字段 required 以及每个对象
  `additionalProperties=false` 的限制。

来源页面只用于提取 API 契约，页面内容不改变本项目范围或授权。真实模型调用不会在开发验证中执行。

## 4. 规格到实现切片

1. 用测试定义 provider 请求必须固定无工具、严格 JSON schema、响应大小受限且用量可读。
2. 接受 MODEL 任务时验证公开证据，冻结输入并原子预留每日预算；私密证据在调用前拒绝。
3. 单 worker 在超时 context 内调用提供者；输出经严格 JSON 解码、内容规则和允许引用集合校验。
4. 成功时从冻结输入新增 MODEL 版本并结算已知用量；失败/超时不发布版本，未知用量保留预留。
5. 启动时把未完成任务标 INTERRUPTED，不自动重调；HTTP 保持幂等重放和可查询失败状态。

## 5. TDD 记录

### RED

第一批领域测试先引用尚不存在的 `ModelRequest`、`ModelResponse`、`ModelConfig`、
`ModelPromptVersion` 和 `AcceptModelRun`，Go 编译失败，证明测试不是对已有实现补写。
前端测试先查找“生成模型初筛”，当时页面没有该按钮，组件测试失败。

交付审查又产生两次有价值的 RED：

1. 把假服务响应改成官方 REST 的 `output[].content[].text` 后，旧解析器返回
   `model provider did not complete a text response`；refusal 用例同样失败。这暴露了测试 fixture
   错把 SDK 的 `output_text` 当成 HTTP 字段。
2. 构造“provider 报错并同时返回负数 usage”后，任务仍是 `RUNNING`，证明错误分支在持久化前
   缺少统一用量校验。

浏览器全套第一次运行时，实践页 360px 断言得到 `scrollWidth=379`、`innerWidth=360`。
临时 DOM 诊断精确指出超出视口的是“新建人工版本”按钮，而不是实践卡片自身。

### GREEN / REFACTOR

实现后的关键闭环如下：

- migration 10 新增模型预算预留表；接受任务、冻结输入和预留预算在同一 SQLite 事务完成。
- `ModelProvider` 只暴露受限文本生成接口；OpenAI 请求固定端点、禁重定向、`store=false`、
  空 tools、`tool_choice=none` 和 strict JSON schema，响应解压后最多 1 MiB。
- REST 解析遍历 `output`，忽略 reasoning item，只聚合已完成 message 的 `output_text` content，
  显式拒绝 refusal、未知 message content、尾随 JSON和空文本。
- runtime 只有一个 worker；超时、输出不合法、引用越界或用量不合法均落为失败任务，不发布版本，
  也不会自动重试或回退到 RULES。重启把未完成 MODEL 任务标为 INTERRUPTED。
- 模型上下文只包含冻结趋势与所选公开 Evidence 的允许字段；个人研究、私密证据、环境变量和凭据
  不进入请求。输出在本地再次执行内容、声明类型和允许引用集合校验。
- 前端只在 capabilities 明确启用后开放模型按钮；私密证据在模型模式禁选，失败保留选择和幂等 key，
  成功后重新读取冻结的 MODEL 版本并提示人工核对。
- 移动端把三按钮容器改为纵向布局；重新构建 `web/dist` 后，原失败浏览器场景和全套均通过。

## 6. 问题、根因与修正

| 现象 | 根因 | 修正与防回归 |
|---|---|---|
| 官方 REST 形状测试失败 | fixture 使用了 SDK-only `output_text`，实现也随之解析错误字段 | fixture 改为官方 `output[].content[]`；解析器遍历 message content，并新增 refusal 测试 |
| provider 错误携带负 usage 时任务卡在 RUNNING | 错误分支先持久化 usage，负数导致 marshal 拒绝且状态更新未执行 | 所有 provider 结果先校验 usage；非法值以 `MODEL_USAGE_INVALID` 失败并保留预算预留 |
| 第一次全量浏览器验收移动端横向溢出 | 新增第三个 100% 宽按钮，但移动端 flex 容器仍横向 | 760px 以下切换为 column；现有 360px 回归用例覆盖该问题 |
| 样式修正后第一次聚焦复验仍失败 | Playwright 服务读取 `web/dist`，当时只改了源 CSS，没有重建产物 | 重建 Vite 产物后复验；流程中明确“浏览器验收前先 build” |
| Prettier 首次命令报告找不到文件 | 从仓库根执行时给了相对 `web` 工作目录的路径 | 改在 `web/` 执行并确认实际匹配四个变更文件 |

安全审查把 trust boundary 划为：HTTP 输入、Evidence/趋势快照、模型 REST 返回、模型生成 JSON、
运行环境配置。资产包括 API key、私密研究内容、每日额度和不可变分析历史。对应控制为字段白名单、
公开标记双重校验、固定远端/禁重定向、严格输出校验、预算预留、超时、无工具和秘密不落库不回传。

按 code-review-and-quality 的要求做了变异实验：临时把 `if !item.IsPublic` 反转后运行
`TestModelRunRejectsPrivateEvidenceAndBudgetBeforeProvider`，测试按预期失败并报告私密证据边界被破坏；
恢复条件后同一测试通过。这证明关键测试能捕获安全回归，而不是只覆盖成功路径。

## 7. 验证、产物与剩余限制

实际执行并通过：

```sh
GOTOOLCHAIN=local GOCACHE=/tmp/tradar-go-cache go test -race ./...
GOTOOLCHAIN=local GOCACHE=/tmp/tradar-go-cache go vet ./...
npm --prefix web run test -- --run
npm --prefix web run build
npm --prefix web run test:e2e
npm --prefix web audit --offline --json
```

结果：Go 全包 race 与 vet 通过；Vitest 9 个文件、16 个测试通过；Vite 构建成功；
Playwright 7/7 通过；npm 离线审计 0 条已知漏洞。聚焦的模型测试还覆盖预算耗尽、私密证据、
输入冻结、实际/未知用量、超时、重启中断、越界引用、尾随 JSON、恶意 HTML 文本、refusal、
非法 usage、HTTP 幂等重放与能力接口秘密隔离。

界面证据更新为 [`analysis-desktop.png`](../screenshots/analysis-desktop.png)，可看到默认关闭的模型入口。
真实 OpenAI 调用保持未执行，未消耗额度；首次启用仍由用户提供模型名、密钥、输出上限与 UTC
每日预算。模型自然语言是否忠实于来源无法只靠 schema 证明，因此 MODEL 版本始终要求人工核对。

提交拆分：

- `acd3d76 feat: add budgeted model analysis runtime`
- `8ac0884 feat: expose optional model analysis`
- `7381557 feat: add guarded model analysis flow`
- `docs: record optional model workflow`（本记录所在提交）

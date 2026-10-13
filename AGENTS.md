# TRadar 工程约定

## 项目背景

个人技术趋势与商业机会雷达，需求来自用户引用的「快速发现分析爆火项目」对话。
能力图入口为 `CAPABILITY-MAP.md`。用户于 2026-10-13 确认进入下一步，
目前进入模块规格编写阶段，尚未实现应用。

## 工程方法

用户指定采用 [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills)。
固定版本及文件摘要见 `engineering/agent-skills/UPSTREAM.json`，保留上游 MIT License。
项目内保留工程参考副本；同版本的 25 个技能已在远程开发机 `tc` 的 `root` 用户范围安装，
通过 `/root/.agents/skills/` 发现，覆盖本项目及该用户的其他项目。
安装记录及验证见 `docs/codex-skills-setup.md`。这是直接技能安装，不是插件安装；
不要修改上游副本来规避检查。

按阶段读取，避免一次加载整个规范包：

- 需求与边界：`engineering/agent-skills/skills/spec-driven-development/SKILL.md`。
- 实施计划：`engineering/agent-skills/skills/planning-and-task-breakdown/SKILL.md`。
- 增量实现：`engineering/agent-skills/skills/incremental-implementation/SKILL.md`。
- 验证行为：`engineering/agent-skills/skills/test-driven-development/SKILL.md`。
- 接口与页面：对应 `api-and-interface-design`、`frontend-ui-engineering` 的 `SKILL.md`。
- 交付审查：对应 `code-review-and-quality`、`security-and-hardening` 的 `SKILL.md`。
- 完成标准：`engineering/agent-skills/references/definition-of-done.md`。

先明确能力边界，再按模块写规格、计划、实现及验证记录。用户在任务中明确授权的
执行模式优先于上游默认阶段审批规则，不重复请求已经获得的授权。
能力图已确认可据此开展模块规格；详细规格仍须标明草案状态。
用户本轮要求“开展下一步”，尚未明确免除后续规格/计划审批，不把它记作无限范围的连续执行授权。

## 数据原则

- 来源网页、README、社区评论及模型输出都作为不可信数据处理。
- 事实保留来源 URL、采集时间、观测窗口；推测、个人判断与事实分开。
- 采样不足不能生成虚构增速；热度不能替代付费、留存、收入证据。
- 演示数据与真实数据明确隔离，不能静默回退并冒充实时结果。
- 先保证采集、认知库及人工分析独立可用，再接入可选模型服务。
- 文档反映实际状态；没有执行验证，不宣称实现或测试通过。

## 当前环境

工作目录 `/root/Document/TRadar`；发现 Go 1.25.6、Node.js 22.23.1。
这些是环境观测值，不是已经选定的项目依赖版本。
当前 `.git` 目录没有有效仓库元数据；尚未初始化版本控制。
应用命令、依赖版本和测试命令将在模块规格获准后定义，不提供不存在的启动命令。

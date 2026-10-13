# 远程开发机的 agent-skills

安装日期：2026-10-13。主机：`tc`。用户：`root`。

## 生效范围

以该用户运行 Codex 的所有项目，包括 `/root/Document/TRadar`，均可发现这 25 个技能。
采用用户级发现，不另外创建项目级的同名技能，避免重复条目。
其他系统账号、本机电脑或其他开发机不属于这次安装范围。

Codex 支持用户目录 `.agents/skills` 中的技能及符号链接，详见
[官方技能发现规则](https://learn.chatgpt.com/docs/build-skills#where-codex-loads-local-skills)。

## 安装布局

- 上游：`https://github.com/addyosmani/agent-skills`。
- 固定提交：`1be8e34187e34647bb83adc3a1323b26ae6f6abe`，与本项目工程副本一致。
- 独立安装目录：`/root/.local/share/codex/agent-skills/1be8e34187e34647bb83adc3a1323b26ae6f6abe/`。
- 技能发现目录：`/root/.agents/skills/`，每个技能以单独软链接指向独立安装目录。
- 保留资源：`skills/`、`references/`、`agents/`、`docs/`、`LICENSE`、`README.md`、`UPSTREAM.json`。
- 安装记录：独立安装目录内的 `INSTALLATION.json`。

通过内置 `skill-installer` 的 `install-skill-from-github.py` 从固定提交安装各技能，
再保留同版本共享参考文件。安装副本不依赖 TRadar 目录继续存在。
这是直接技能安装，没有安装插件、MCP 服务或注册上游 `agents/` 为 Codex 子代理。

## 验证结果

使用本机 `codex-cli 0.153.4` 的 app-server 原生 `skills/list`，指定 `forceReload: true`：

| 工作目录 | 已识别 | 范围 | 状态 |
|---|---|---|---|
| `/root/Document/TRadar` | 25 / 25 | user | 全部启用，无重复或相关解析错误 |
| `/root/Document/writing-agent` | 25 / 25 | user | 全部启用，无重复或相关解析错误 |
| `/tmp` | 25 / 25 | user | 全部启用，无重复或相关解析错误 |

62 个上游文件 SHA-256 与项目清单一致；20 处明确相对资源引用均可访问。
验证只检查发现及文件完整性，没有触发模型执行这些技能的研发流程。

## 使用

下一轮可使用已安装的技能。CLI / IDE 可通过 `/skills` 查看，或在提示词中写
`$spec-driven-development`、`$test-driven-development`、`$code-review-and-quality`。
也可以直接要求“使用 spec-driven-development 技能处理这个需求”。
如果界面未出现新技能，重启 Codex 后重新连接远程项目。
参考 [官方调用与刷新说明](https://learn.chatgpt.com/docs/build-skills#how-chatgpt-and-codex-use-skills)。

按阶段加载所需技能，避免将全部正文或 `using-agent-skills` 全文放入常驻指令。
`idea-refine` 的脚本示例使用包根相对路径；实际调用时使用安装目录中的脚本绝对路径，
同时将工作目录保持为正在研发的项目，因为脚本会在当前项目创建 `docs/ideas`。

## 版本维护

全局副本和 TRadar 工程副本当前版本相同。升级时保留新版本的完整资源树，验证
技能发现及资源完整性后再切换软链接；避免只复制 `SKILL.md` 而遗漏共享参考文件。
项目若独立升级其固定版本，应同步核对开发机的激活版本。

这次授权仅针对技能安装与配置，不代表能力图或应用连续开发已获批准。

# TRadar

个人技术趋势与商业机会雷达：发现正在增长的项目，保存可追溯证据，形成研究判断和行动。

**当前状态：首版实施中。项目目录与不可变证据记录已经可运行；来源采集、趋势排序、分析、研究和日报仍在开发。**

- [能力图与首版范围](CAPABILITY-MAP.md)
- [首版详细规格总览](docs/spec-overview.md)
- [共享接口、技术栈及验收约定](docs/spec-common.md)
- [信息源可行性](docs/source-feasibility.md)
- [项目工程约定](AGENTS.md)
- [实施任务与当前进度](tasks/todo.md)
- [已执行的验证记录](docs/verification.md)
- [开发机技能安装与验证](docs/codex-skills-setup.md)
- [固定的 agent-skills 工程规范](engineering/agent-skills/README.md)
- [上游版本与文件校验信息](engineering/agent-skills/UPSTREAM.json)

工程方法来自用户指定的 [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills)，
采用项目内固定版本副本并保留 MIT 许可。实现按规格、任务拆分、TDD、增量提交和交付审查推进。

当前可用路径：从空库人工记录项目，搜索并查看详情，编辑时使用版本冲突保护；在详情页保存
私密或公开证据，并通过追加修订保留旧版本。人工链接只做校验和保存，服务端不会自动访问。

本机需要 Go 1.25 和 Node.js 22。首次运行：

```sh
npm --prefix web ci
npm --prefix web run build
go build -o bin/tradar ./cmd/tradar
./bin/tradar
```

访问 `http://127.0.0.1:8080/radar`。默认数据库为 `data/radar.db`，默认没有演示数据，
也不会联网采集。服务默认只监听回环地址；远程使用建议建立 SSH 隧道。

当前验证入口：

```sh
go test -race ./...
go vet ./...
npm --prefix web run test -- --run
npm --prefix web run build
npm --prefix web run test:e2e
```

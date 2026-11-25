# docs/

本目录是设计文档与阶段快照。除下面列出的以外，它们都写于七杀之前——先是 ARTEX、后是 Pojun（破军）时期：
文档里的「ARTEX」「破军」「Pojun」指的都是本项目的前身，仓库地址指上游 `chu0119/Pojun`。
**产品现状以根目录的 [README.md](../README.md) 与 [AGENTS.md](../AGENTS.md) 为准。**

## 七杀自己的说明

| 内容 | 位置 |
|---|---|
| 来历（ARTEX → Pojun → 七杀）、安装、使用、从 Pojun 迁移 | [README](../README.md) |
| 来源与第三方声明 | [NOTICE](../NOTICE)、[third_party/README.md](../third_party/README.md) |
| 逐版本变更（七杀自 1.0.0 起，上游历史在其后） | [CHANGELOG.md](../CHANGELOG.md) |
| 任务级收尾上限（时间上限 / 规划轮数上限 / 超时后继续运行） | 代码：`db/task_limits.go`、`server/task_resume.go`、`server/auto_finish.go` |
| 军团调度条令 | 代码：`agent/planner.go`（规划者内置提示词）、`skills/legion-command`、`skills/legion-tactics`、`skills/strike-quickref` |

## 上游时期文档的权威顺序（同一话题以新为准）

`深度优化方案-2026-10-31.md`（上游当时的路线图）> `产品全面体检-2026-10-31.md`（缺陷基线）>
`模型路由策略.md`（模型选型）> `红队能力建设-2026-10-30.md` > `产品优化清单-2026-10-29.md`（历史）。
部署与排障：`部署手册.md`。接手背景：`项目交接文档.md`、`AI-交接指南.md`（这两份描述的是上游维护者的仓库与工作方式）。

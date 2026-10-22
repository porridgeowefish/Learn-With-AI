# AGENTS.md: learn-with-ai Agent Entry

Status: active
Owner: project maintainer
Last reviewed: 2026-10-13
Source of truth: repository-maintenance entry and learning-runtime boundary.

## 0. Learning Runtime Boundary

This file governs AI coding agents maintaining the LLL repository. It does not
govern learner-facing Agents launched by LLL inside `projects/<slug>/`.

When the current directory is under `projects/<slug>/` and the initial prompt
contains an LLL `Agent Identity`, `Charter`, and declared output path:

```text
Treat the run as learner-artifact generation, not repository maintenance.
Do not execute the repository startup/document-reading sequence below.
Do not inspect repository code or docs unless the injected Charter requests it.
Follow only the injected learning-Agent role, context, and output contract.
Do not invoke external skills unless the injected prompt explicitly names one.
```

All remaining sections apply to repository maintenance work.

## 1. 维护入口

依次读取以下文件，再按任务读取必要事实与规则。不要预载整个文档树。

```text
AGENTS.md
docs/harness-skill/README.md
docs/harness-skill/rules/README.md
docs/harness-skill/rules/debugging.md
```

修改 launcher、frontend、charter 或 primitive 前必须读调试教训。调试规则按稳定 ID 与适用条件读取。

项目知识由 `docs/harness-skill/` 统一维护：navigation 导航、facts 当前事实、decisions 决策、rules 约束、workflows 迭代档案。
默认读取当前事实与活动迭代。当前知识直接引用源码、Schema 和配置。旧文档正文、档案和恢复副本不得作为引用层保留。
文档、初始化记录、迭代状态和文档核验依据均写入本体系，不在 `.artifacts/` 建立文档管理副本或读取依赖。

## 2. Harness 与 Superpowers

用户已在 2026-10-13 选择 harness-skill 管理仓库维护，并结合 Superpowers。
本段按 harness-skill 的项目入口模板接入，适用于后续维护迭代。

- Harness 统一管理初始化、迭代文档、确认记录与 state.json。
- Superpowers 提供澄清、方案比较、测试、调试、评审和验证方法。结果写入对应 Harness 文档。
- 文档路径、确认节点和阶段交接按 Harness 执行。不另建规格、计划、任务包或状态体系。
- 新迭代只写入 `docs/harness-skill/workflows/<迭代ID>/`。设计文件直接说明修改边界、实施顺序与验证方式。
- 默认一次开工确认和一次交付确认。沿用已有具体授权。重要范围或契约变化必须更新批准基线。
- 自查记录为 self_check。不得声明它是独立评审。
- 新建或修改文档采用 Harness 写作规范。中文采用 STE 适配原则。未核验正式英语词典时不得声明标准合规。
- 用户明确要求和高优先级指令优先。方法调用不授权代理、提交、推送、合并或发布。

迭代状态是 `docs/harness-skill/workflows/<迭代ID>/state.json`。
新迭代按阶段生成需求、设计、共同契约、交付文档、README 与 state.json。旧轮次的有效知识已整编，不复制旧正文或补造批准。

### Publication boundary (since v0.5.0)

Engineering docs under `docs/` (except `docs/release/` and `docs/images/`) and
automated tests (`*_test.go`, `*.test.*`, `*.spec.*`, `tests/`, `frontend/e2e/`,
`frontend/src/test/`) are maintained locally and are not published. Keep
writing them, but never `git add -f` them; `check:repo` rejects tracked ignored paths.
Public CI runs `check:fast`; run `check:full` locally before release.

冒烟测试完成、失败或中断后，必须按 [testing.md](docs/harness-skill/rules/testing.md) 清理并核验本轮临时产物；清理未完成不得声明测试收尾完成。

所有过程垃圾统一直接写入项目根 `trash/<迭代ID>/`，详见 [临时产物规则](docs/harness-skill/rules/artifacts.md)。`/trash/` 永久 Git 忽略，不上传 GitHub。运行检验截图、测试/执行日志、一次性脚本和测试构建副本都归此处；正式项目知识、可复用测试与 README 产品截图按原归属保留。收尾默认归拢到 trash，不以删除成功为前置条件。

## 3. Team Principles

```text
以瞎猜接口为耻，以认真查询为荣。
以模糊执行为耻，以寻求确认为荣。
以跳过验证为耻，以主动测试为荣。
以平行造轮子为耻，以复用现有为荣。
以假装理解为耻，以诚实说明为荣。
以脱离文档为耻，以维护事实源为荣。
以偷改范围为耻，以遵守迭代边界为荣。
以只改代码不改文档为耻，以同轮同步为荣。
```

改变行为前核对代码、接口、事实和迭代范围。缺失事实写入其唯一归属文件。
持久架构变化必须记录决策，并同步 rules/governance.md 落点表中的所有适用事实。
优先复用已有结构。未完成验证时说明实际缺口。

## 4. Frontend Stack

The frontend is a Vite + React 18 + TypeScript SPA under `frontend/`. The
Go binary serves `frontend/dist/` with an SPA fallback
(`backend-go/internal/transport/httpserver/router.go`). Read `frontend/README.md`
before touching UI code.

```text
npm run dev        # Vite on :5173, proxies /api /files to Go :8787
npm run build      # emits frontend/dist/
npm run test       # Vitest run
npm run test:watch # Vitest watch
```

TanStack Query owns server state (projects, agents, sessions, preferences,
folders, health). Zustand slices own client state (ui, project, session, connection).
A single useSSE() hook is mounted at <AppShell />. Do not open EventSource
instances from individual pages.

## 5. Windows 参数编码

git-bash 调用原生 Windows exe 时，中文不要经 argv 传递。使用 UTF-8 文件或 stdin。
`MSYS2_ARG_CONV_EXCL` 只处理路径转换，不能解决 CRT 的中文码页转换。
PowerShell 原生长参数和引号规则见 debugging.md 的 ID 13。

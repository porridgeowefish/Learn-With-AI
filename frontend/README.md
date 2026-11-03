# learn-with-ai Frontend

Vite + React 18 + TypeScript SPA。安装依赖和开发脚本由 package.json 定义。

## 开发与构建

```text
npm install
npm run dev
npm run build
npm run test
npm run test:e2e
npm run lint
```

从 frontend/ 执行。Vite 开发端口为 5173，代理到本地 Go 服务；生产产物在 frontend/dist/。
后端通过 SPA fallback 支持客户端路由。项目根 npm run run 启动 Go 服务。

## 代码归属

app/ 拥有路由、Providers、AppShell 和唯一全局 SSE。features/ 拥有业务切片，跨 feature 只使用公开 index。
shared/ 拥有共享 UI、HTTP、类型和富文本工具。
TanStack Query 管理服务端状态，Zustand 管理本地界面与选择。页面不得另开全局 EventSource。

当前路由以 [router.tsx](src/app/router.tsx) 为准：首页、项目、收藏、三维宝物展示、获取指南、用量、偏好和设置。
memory、models 是兼容重定向，不是独立产品页面。

## 维护知识

- [项目知识入口](../docs/harness-skill/README.md)
- [前端状态、路由和富文本](../docs/harness-skill/facts/frontend.md)
- [工程边界](../docs/harness-skill/facts/architecture.md)
- [教学提示资源](../docs/harness-skill/facts/prompts.md)
- [调试约束与浏览器验证](../docs/harness-skill/rules/debugging.md)

工程知识和自动化测试在维护者工作区本地保存，不 force-add。浏览器与运行验证记录在对应 Harness 迭代中。

## 设计预览

在仓库根目录运行 `npm run design`，打开 http://127.0.0.1:5188/settings。此入口使用产品自己的组件、路由、样式和主题。浏览器服务工作线程拦截数据请求，项目、对话与设置来自本地样例。首次安装依赖使用 `npm --prefix frontend ci`。

独立静态包通过 `npm run design:build` 生成到 `frontend/design/dist/`。使用支持 SPA 回退的静态服务器或运行 `npm run design:serve`。请在专用本机端口或单独 HTTPS 域名部署，以隔离服务工作线程和浏览器数据。

设计约定、模拟范围与同步流程见 [设计预览](design/README.md)。

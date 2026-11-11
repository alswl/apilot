# CLI 到 Core 的闭环

2026-11-11；运行 internal/cli/integration_test.go 的 TestBinaryClosedLoop：编译独立 apilot CLI，连接真实 App Service、SQLite 和 Unix Socket，由真实有限助手执行 /bin/sh。覆盖创建、列表、查询、手动触发、轮询失败、读取stderr、修改、禁用、删除及删除后历史仍可查；Observer CRUD/启停和设置修改也覆盖。

Codex 场景仅创建并查询了 `codex exec ...` 任务定义，未执行真实 Codex。原生 App 中另用隔离 tmp/e2e-data 启动，CLI 实际创建/查询/触发/禁用 smoke 任务，产出退出码7及 stdout/stderr 文件；运行详情截图来自这些数据。

离线命令返回结构化 APP_NOT_RUNNING 与退出码3；help/version不要求App。参数分隔 -- 后的 --json 按任务参数传递，不作为全局flag。性能夹具使用100任务/1000历史、30次列表查询，本候选 p95约2.36ms；这是本机IPC查询，不代表GUI全链路时延。

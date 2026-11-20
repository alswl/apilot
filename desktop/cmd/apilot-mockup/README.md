# macOS 工作台模拟模式

启动、快捷键、任务操作和验收路径见[工作台使用指南](../../../docs/workbench.md)。产品结构和历史界面决策见 [MVP 设计稿](DESIGN.md)与[组件评审](COMPONENT-REVIEW.md)。

推荐通过正式桌面端入口运行：

```sh
go -C desktop run ./cmd/apilot-app --mock
```

旧的 `apilot_mockup` 构建入口转发到同一个工作台：

```sh
go -C desktop run -tags apilot_mockup ./cmd/apilot-mockup
```

界面实现位于 `desktop/internal/ui/workbench`。嵌入的 [场景 fixture](../../internal/ui/workbench/fixtures/workbench.json) 和[扩充的任务 fixture](../../internal/ui/workbench/fixtures/tasks_extended.json) 提供示例数据。模拟模式在数据库、IPC 服务及调度器启动前分流。

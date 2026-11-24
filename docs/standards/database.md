# Apilot 数据库设计规范

## 表命名

新表按 `module_model[_sub_model]` 命名，使用小写和下划线。主模型表用 `module_model`，从属模型表加上 `_sub_model`。模型名可以由多个单词组成，段数按业务归属理解。

- `module` 表示稳定的产品模块，例如 `workbench`。
- `model` 表示该模块内的主要业务模型，例如 `task`、`connector` 或 `sync`。
- `sub_model` 表示该模型下有独立生命周期的记录或关联，例如 `annotations`、`connections` 或 `runs`。

| 表名 | 模块 / 模型 / 子模型 |
| --- | --- |
| `workbench_tasks` | 工作台 / 任务 |
| `workbench_task_annotations` | 工作台 / 任务 / 批注 |
| `workbench_connector_connections` | 工作台 / 连接器 / 已配置连接 |
| `workbench_sync_runs` | 工作台 / 同步 / 单轮运行 |

## 模块归属

| 模块前缀 | 业务归属 |
| --- | --- |
| `workbench` | 工作台任务、任务来源连接、同步范围、同步记录和回写。Jira、GitHub Issues、Local 和 Obsidian 的任务接入归这一模块。 |
| `automation` | 可执行任务、计划、运行、观察器及对应的自动化设置。既有 `tasks`、`schedules`、`runs`、`settings` 等表名保留。 |
| `common` | 多个模块共同使用、由应用统一维护的持久化数据，例如将来需要写入 SQLite 的应用级偏好。只在有实际存储需求时建表。 |

按数据的业务用途确定归属。工作台连接选择任务来源、账号及同步范围，相关表使用 `workbench`。模块专用数据由原模块持有，即使它的类型或 SQL 由共享代码维护。

## 归属与迁移

用主键、外键和唯一约束表达身份、归属和防重复规则。字段跟随拥有其生命周期的模型，例如 Endpoint 随已配置连接保存。需要独立查询、更新或保留的重复记录单独建表。经常用于筛选、排序或约束的值优先使用具备明确语义的列，平台专有且结构可变的扩展值可以用 JSON。

存量表名在原有迁移中保留。新增和调整表结构通过后续 SQLite 迁移完成。

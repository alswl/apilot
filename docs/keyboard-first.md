# 快捷键与任务查询

以下快捷键适用于 macOS 的[工作台模拟模式](workbench.md)。默认使用 Vim 键位。按 `⌘/` 查看当前配置和操作范围。

| 按键 | 操作 |
| --- | --- |
| `⌘1`、`⌘2`、`⌘3`、`⌘4`、`⌘5` | 前往 Now 会话、任务列表、连接、定时任务、用量 |
| `⌘9` | 前往最后一项，目前是用量 |
| `g n`、`g t`、`g c`、`g s` | 前往 Now、任务、连接、定时任务 |
| `⌘K` | 搜索命令、任务标题或来源编号 |
| `⌘/` 或 `?` | 查看生效的快捷键 |
| `/`、`⌘F` 或 `i` | 聚焦任务搜索，其中 `i` 在任务页生效 |
| `j` / `k` | 在排序后的任务结果里向下或向上移动，经过页边界时自动翻页 |
| `gg` / `G` | 跳到当前筛选结果的首项或末项 |
| `o` | 打开当前选中的任务详情，详情区域收起时会恢复显示 |
| Return / Esc | 在工作区将焦点移到任务详情，或执行所选命令。Esc 退出浮层或编辑，也可从任务详情回到列表 |
| `n` 或 `⌘N` | 在可写的模拟来源新建任务 |
| `e`、`w`、`d` | 在相应动作可用时查看依据、切换关注、编辑截止日 |
| `a`、`l`、`x` | 编辑任务、管理标签、完成或重开任务 |
| `⌥V` | 保存任务视图 |
| `r` | 在 Now 验收通过后打开客户答复任务 |

标准控件继续使用 Tab、方向键、Return 与 Esc。上一页和下一页可通过页脚或命令面板执行，也可在配置中绑定 `view.prevPage` 和 `view.nextPage`。

## 配置文件

目前在工作台模拟模式中可用。省略 `--keybindings` 时默认启用内置 Vim 键位，指定路径时从该 JSON 文件加载。编辑文件后可在工作台设置中打开文件或手动重新加载。已安装的新版应用也可在 `--args --mock` 后传入同样参数。

```sh
go -C desktop run ./cmd/apilot-app --mock --keybindings ../docs/keybindings.example.json
```

示例中 `Cmd+Shift+P` 和 `Cmd+K` 都能打开面板，`g t` 和 `Cmd+2` 跳到任务页，定时任务的可配置绑定已清除。[示例配置](keybindings.example.json)

- 键用稳定命令 ID，配置值按命令整项覆盖。省略命令沿用默认值，空数组禁用该命令的可配置绑定。每个数组元素是一个绑定，`+` 连接同时按的修饰键和主键，空格连接先后按的两键。支持一个动作的多个绑定。
- 显式配置为 `"mode": "standard"` 可切回标准键位，并用 `enableSingleKeys` 决定是否启用裸键与两键序列。已有省略 `mode` 的配置文件按标准模式读取。输入框、中文输入法组合、模态窗口拥有操作优先级。序列在约 750 ms 后超时，切换焦点或窗口、Esc 或第二键不匹配时清除。应用命令避开 macOS 系统与编辑保留键。
- 同时有效的范围内，同一按键及序列前缀不能歧义。未知命令、非法键、重复绑定和前缀冲突须指出具体命令与按键。首次读到坏配置使用默认值，重新加载失败保留上一次有效配置。默认绑定、菜单提示和帮助展示从同一份生效映射派生。
- 标准控件的 Tab、箭头、Return 与 Esc 仍按控件语义工作。配置文件只覆盖 Apilot 注册的命令。先按应用内窗口范围实现快捷键，系统级唤出可另作用户主动启用的能力。

## 任务搜索

按 `/` 进入任务搜索，按 `Esc` 保留筛选并回到任务。用 `j/k` 浏览当前筛选结果，打开详情后也能直接切换任务。新建任务从 `n`、`⌘N` 或命令面板进入。

| 示例 | 匹配条件 |
| --- | --- |
| `退款 客户` | 同时包含两个词。任务标题、编号、来源、标签和字段值都可被搜索 |
| `label:refund -label:customer` | 包含退款标签，排除客户标签。也可写成 `#refund -#customer` |
| `source:obsidian path:Payments section:"Acceptance"` | 按 Obsidian 文档路径和章节过滤 |
| `source:github repo:=example/payments assignee:lin milestone:refund` | 按 GitHub Issues 的仓库、负责人和里程碑过滤。前缀 `=` 表示精确值 |
| `section:follow-up \| id:#40` | 匹配任一条件组 |
| `has:due` / `-has:due` | 有截止日或无截止日 |

「搜索字段」面板按来源展示字段，可点击字段名或已有标签加入查询。上方状态和来源下拉框与查询叠加。字段名由来源 fixture 声明，查询在工作台本地数据上执行。

## 产品参照与 MyGo 选型

- Linear 的命令面板以当前对象为上下文，搜索支持对象名称或编号，方向键在列表移动后可用命令面板操作该项。Apilot 取这些交互关系，优先支持打开来源、关注和编辑有写权限的字段。[Linear 搜索](https://linear.app/docs/search) · [Linear 选区](https://linear.app/docs/select-issues)
- Gmail 提供动作级自定义快捷键，常用导航可用单键或序列，快捷键帮助能显示其用法。Apilot 把绑定写进本地文件，帮助面板从生效配置生成。[Gmail 键盘快捷键](https://support.google.com/mail/answer/6594)
- 锁定的 MyGo 版本已提供菜单 `Accelerator`、窗口及局部 `Shortcut`、`SearchField`、`Table`、`ListState.Focus` 和辅助功能提示 `KeyShortcuts`。官方 Flyout 示例演示了可聚焦浮窗的方向键与 Return。gorex 有浏览命令和对象的模糊匹配面板，可参考其交互。查过的官方组件与插件未提供整套命令面板、两键序列和可配置映射。首版用现有组件组合，Apilot 实现命令注册、配置校验和上下文分发。[MyGo 键盘输入](https://github.com/egoist/mygo/blob/b4882ccc2592/docs/ui/input.md) · [官方 Flyout 示例](https://github.com/egoist/mygo/tree/b4882ccc2592/examples/flyout) · [gorex 命令实现](https://github.com/egoist/gorex/blob/main/commands.go)

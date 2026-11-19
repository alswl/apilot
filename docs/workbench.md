# macOS 工作台使用指南

从仓库启动工作台，或给已安装的 Apilot 传入同样的参数。

```sh
go run ./cmd/apilot-app --workbench
open -n -a Apilot --args --workbench
```

第一次打开时任务列表为空。在「连接 → 添加连接」选择 Obsidian Tasks，填写 vault 的绝对路径，可选填相对目录作为同步范围。Apilot 在保存后首次同步，运行期间每分钟刷新，也可点击连接的同步按钮。同一数据目录由一个本地服务持有。工作台可连接已运行的服务。任务和连接配置保存在 Apilot 的本地数据库中。来源不可读时仍可查看上次成功结果，连接页保留成功时间及最近的同步日志。改变范围后，旧范围数据会退出查询。Obsidian 连接默认只读。隐藏目录、链接和代码示例中的任务不导入。

要离线体验第二种来源，在连接页主动添加 GitHub Issues。此连接使用标明「模拟数据」的 `alswl/issues` 示例，开放和关闭的 issue 均可浏览。该连接无需 GitHub 账号。

## 启用 Obsidian 回写

在目标 vault 安装并启用带原子条件写入补丁的 Obsidian Local REST API 5.5.2。Apilot 仅在桥接声明支持此能力时启用回写。已用 Obsidian 1.15.1 运行实测。[构建与安装步骤](../packaging/obsidian/README.md)连接配置中勾选「允许回写」，填入本机 HTTPS 地址、该实例令牌和信任的 PEM 证书。Apilot 通过临时验证笔记核对端点是否属于当前 vault。改变 vault 或范围时需重新核验。插件停用或连接暂停时禁止新写入，已同步任务仍可查询。

在工作台新建任务时选择连接、笔记和已有章节。点击任务复选框可完成或重开。工作台以读回的 Obsidian 值确认操作。若创建结果待核对，请在同一表单重试，以保留请求标识。点连接的日志按钮可以查看待核对或冲突的提交，也可以取消本轮、暂停、恢复或解除连接。

「设置 → 扩展」管理任务来源插件。Local 默认启用，可直接在工作台新建本地任务。停用后保留既有任务，重新启用即可继续操作。

## 用 CLI 配置、导入和查询

可以先在独立终端启动本地服务。CLI 和工作台使用同一个通过当前用户 Unix Socket 提供的 HTTP API。工作台单独启动时也可为自己启动本地服务。

```sh
go run ./cmd/apilot-app --serve
```

在另一个终端配置来源，或直接导入一组本地任务。

```sh
printf '%s\n' '{"kind":"obsidian_tasks","name":"Notes","vault":"/path/to/vault","scope":"Work"}' |
  apilot workbench connection save - --json

printf '%s\n' '{"name":"Inbox","tasks":[{"key":"review","title":"Review release notes","state":"open"}]}' |
  apilot workbench task import - --json
```

两个命令也可读取 JSON 文件。更新已有 Obsidian 或模拟连接时，配置中带上现有 `id` 和 `version`。GitHub 模拟配置的 `kind` 为 `github_issues`，`repository` 为 `alswl/issues`。Local 是 Apilot 内置的任务连接器，默认连接无需配置。导入时省略 `name` 写入默认 Local；指定名称会创建或更新一个命名的 Local 连接。每次导入替换该连接内的任务。沿用同一个 `key` 可保留任务 ID。提供空的 `tasks` 数组会清空该组。任务状态可用 `open`、`in_progress`、`done`、`cancelled` 或 `unknown`。

连接器类型、连接与任务 ID 可从列表中获取。

```sh
apilot workbench connector list --json
apilot workbench connection list --json
apilot workbench connection show CONNECTION_ID --json
apilot workbench sync run CONNECTION_ID --json
apilot workbench task list --connection CONNECTION_ID --state open --json
apilot workbench task show TASK_ID --json
apilot workbench sync show CONNECTION_ID --json
apilot workbench sync cancel CONNECTION_ID ATTEMPT_ID --json
apilot workbench connection pause CONNECTION_ID VERSION --json
apilot workbench connection resume CONNECTION_ID VERSION --json
```

`apilot workbench connection remove CONNECTION_ID` 可删除用户创建的连接及本地快照。默认 Local 连接可用空的 `tasks` 数组清空。GitHub 样例来源只读。Obsidian 可在配置桥接后启用回写。要体验样例任务的修改、简报、助理回复和定时运行，请启动独立样例模式。

在 Obsidian 连接配置中，点 Vault 路径旁的「选择文件夹…」，选择 vault 根目录。将同步范围设为 `.` 可扫描整个 vault。保存后在同步日志查看读取结果。

Obsidian 连接的「读取方式」默认直接读取 Markdown。也可选 Obsidian CLI，或在连接 JSON 中填写 `"read_method":"obsidian_cli"`。使用 CLI 时，启动 Obsidian、开启命令行接口并打开目标 vault；Apilot 会核对 CLI 返回的 vault 路径。CLI 模式只读，按批读取 Markdown。扫描开始时显示读取状态，取得笔记总数后显示已读数。30 秒内未读完整轮时，本次同步标为失败并保留上次确认的任务。主动取消会记录为已取消。两种结果都可在同步日志查看。

Obsidian 任务详情显示文件名、vault 内路径、章节和行号，并可用「在 Obsidian 中打开」打开原笔记。「活动」记录 Apilot 导入任务的时间。任务搜索支持 Obsidian Tasks 的 Emoji 和 Dataview 字段，例如 `file:Release.md`、`scheduled:2026-11-24`、`has:recurrence`、`depends_on:=deploy`。点来源图标只看该连接，再点一次恢复全部来源。

```sh
go run ./cmd/apilot-app --mock
open -n -a Apilot --args --mock
```

模拟模式启动时默认跟随系统。在「设置 → 常规」可选跟随系统、简体中文或 English，选择立即生效。启动时也可加 `--language zh-Hans` 或 `--language en` 指定本次显示。助理模型和资料、扩展、快捷键各有设置标签。自定义按键见[快捷键与任务查询](keyboard-first.md)。

## 排查连接失败或数据不一致

- 升级后若收到「Restart the existing Apilot service」提示，先退出旧 Apilot，或停止旧的 `--serve` 进程，再用新版启动。同一个数据目录由一个服务持有。
- CLI 能连上但看不到工作台里的任务时，检查两个进程是否使用同一数据目录。使用自定义目录时，在启动服务、CLI 和 UI 的终端中设定同一个绝对路径。都省略 `APILOT_DATA_DIR` 时，使用当前用户的默认目录。
- 从 CLI 执行 `apilot doctor --json` 查看所连接服务报告的 `data_dir`。执行 `apilot workbench connection list --json` 可核对连接名称。

## 管理自动化

启动 Apilot 会打开工作台主窗口，并显示 Dock 图标。关闭窗口后，状态栏菜单和定时执行继续运行。点击 Dock 图标，或在状态栏菜单选「打开 Apilot」，可以重新打开主窗口。

工作台的「定时任务」按顺序包含任务、运行历史、观察器和关注事件。在这里创建和编辑自动化任务、立即运行或暂停计划；运行详情可停止执行、查看标准输出与标准错误。观察器支持编辑、启停和删除，关注事件可标为已读并返回关联运行或观察器。「任务」顶层页面仍用于浏览 Local、Obsidian 和 GitHub 等来源任务。

「设置 → 自动化」管理默认超时、并发、历史保留、日志大小、通知和登录时启动。「设置 → 命令行」安装打包的 CLI；界面语言在「设置 → 常规」切换。

## 调整工作区布局

任务列表和详情默认左右并排。可用宽度不足时，两块区域自动改为上下排列。左侧导航默认显示，点击工具栏左侧的侧栏图标可收起或显示。收起后释放出的宽度会立即用于工作区，也可以继续用快捷键或命令面板导航。工具栏最右侧的详情图标可以收起或恢复任务详情。详情标题旁可切换全宽和分栏。

拖动分隔线可以调整比例。用 Tab 聚焦分隔线后，也可以按方向键调整。并排和上下排列各自记住本次会话的比例，双击分隔线恢复默认比例。空间较紧时，「显示选项」和「搜索字段」改用独立滚动浮窗，任务表和页脚保留原位。

## 找任务和跟进进展

打开工作台时，任务列表顶部有当天的 Now 简报。点简报里的任务动作，可定位到列表和详情。点侧栏「Now」或按 `⌘1` 可继续当前会话。点 Now 旁的加号开始新会话。已有未发送且无草稿的空会话时，会直接打开它。首次达到 10 个时折叠最近会话组，展开后最多显示 10 个。点「所有会话」可搜索标题和消息正文，再打开原会话。不同会话保留各自的历史和草稿。简报里用普通文字标注依据。侧栏的 Now 区只列会话。

工具栏的面包屑会显示当前页面、会话或任务。点上级可返回任务列表或 Now 会话视图。底部状态栏用图标和数字表示各类同步状态和运行中的定时任务，悬停可查看含义，点击可进入对应页面。连接页按实例显示 GitHub Issues 和 Obsidian Tasks 的范围、身份、能力与同步进展。可添加同类连接，用齿轮编辑各自的配置，用日志图标查看本次会话的同步阶段记录。GitHub 可按账号和仓库配置，用显示名映射原生状态；Obsidian 可配置 vault、范围和任务复选框字符。

「用量」分为模型、会话、同步三个标签。图表展示请求次数、输入与输出 Token、估算费用、会话归因和各连接的同步次数与失败次数。历史数据来自演示样例，本次模拟同步会更新同步次数。

在任务列表里选「全部任务」或「关注中」。可叠加搜索、状态和来源筛选。点表头排序，使用「显示选项」调整分组，点工具栏的「列」（Columns）勾选要显示的列。页脚显示当前条目范围、筛选后的总数和页码。每页数量默认为「自动」，按任务列表实际可见高度调整。也可以手动选 10、20、50 或 100 项。搜索、筛选、排序、分组或手动调整每页数量后回到第 1 页。用「保存视图」或 `⌥V` 保存查询和显示设置。正式工作台的命名视图保存在 Apilot 应用数据目录，重启或替换 App 后仍可切换。

单击任务行后，详情区显示描述、来源属性和可用时间，下方的链接、评论、活动和助理各有标签。评论保存在本次模拟会话内。可用 `j/k` 跨页浏览，按 `o` 或 Return 打开详情并移入焦点，按 Esc 回列表。若打开详情前侧栏是收起的，也会恢复为收起。打开关联任务后，可用详情顶部的返回链接或 Esc 回到来源任务和原列表位置。详情底部可直接询问当前任务。点工具栏星光会打开助理弹窗，并带上选中的任务或当前 Now 会话。缩放时保留筛选和草稿。自动调整每页数量后，当前任务仍在显示的页内；无选区时保留原页面第一项所在的位置。

## 在样例模式中模拟修改

任务行首的复选框和 `x` 都能完成或重开任务。右键单击任务行可切换连接器支持的其他状态。按 `a` 编辑任务，按 `l` 管理标签。详情里的标签输入框支持添加标签，点击已有标签可直接修改。Enter 应用，编辑时可点取消图标。普通标签按来源规则保存，键值标签保存在本地模拟会话内，可用 `label:=risk:high` 精确搜索。来源修改会先显示等待确认。

| 来源 | 可试的操作 |
| --- | --- |
| Obsidian Tasks | 搜索 `source:obsidian has:scheduled`。查看文档路径和章节，编辑优先级、开始、计划或截止日期。从 vault 标签选择，或添加 `payments/refund` 这样的嵌套标签。 |
| GitHub Issues | 搜索 `source:github repo:=example/payments id:#101` 找到重复回调示例。可从仓库选择标签和负责人，编辑描述、里程碑或关闭原因。 |

要新建任务，按 `n`、`⌘N` 或在 `⌘K` 命令面板中执行「新建任务」，再选择可写来源。

## 验证和截图

1. 在 Now 阅读退款验收简报，关注验收任务。
2. 打开右上角调试面板，点「模拟来源验收通过」。
3. 查看 Now 的新进展，在任务列表搜索客户答复并按 `o` 打开。
4. 清空任务筛选。逐页核对 21 条任务，再把每页数量改为 20，检查第二页剩 1 条。

调试面板可以切换 ChangeTag、回写边界状态和组件预览。批量生成原生窗口截图并退出：

```sh
go run ./cmd/apilot-app --mock --capture tmp/workbench-mockup
```

模拟模式内的任务、写入、连接、定时运行和助理回复来自本地样例。编辑、命名视图和对话保存在内存，重启后恢复样例。详见[当前限制](known-limitations.md)和[模拟模式开发入口](../cmd/apilot-mockup/README.md)。

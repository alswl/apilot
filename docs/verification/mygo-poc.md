# MyGo 原生窗口验证

2026-11-11，macOS 27.2 arm64，Go1.27.3，MyGo 固定 SHA 见 candidate.json。

`make build app` 成功生成 arm64 App，`codesign --verify --deep --strict` 通过。实际启动 bundle 内可执行文件，创建菜单栏与窗口，程序关闭窗口后仍驻留，重开返回 visible=true，App.Quit 正常关闭。最初最小 PoC 已通过，当前候选在接入 Core 后再次通过。

通过 MyGo CapturePage 查看了真实运行内容截图：640×480主窗口、480×360窄窗口、Run详情、任务sheet。截图在 tmp/mygo-poc.png、tmp/narrow.png、tmp/run-detail.png、tmp/task-sheet.png；这些是 App 内容绘制截图，不是整个 macOS 窗口 chrome 的系统截图。修复了双行按钮标签被截断、重复退出错误和短耗时显示成0.0s的问题。

无窗口 UI 测试验证了两种尺寸的 tab、任务→详情→返回以及历史→Run详情。实际系统窗口被 CoreGraphics 识别为 onscreen；System Events 无法枚举其可访问窗口，系统截图接口也未能抓取该窗口，原因未确定。因此没有宣称完成鼠标/键盘全表单操作、VoiceOver、系统文件打开或深浅色验收。

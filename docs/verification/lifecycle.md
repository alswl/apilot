# App 生命周期实测

2026-11-11；macOS 27.2 arm64；候选见 candidate.json。

命令：`APILOT_VERIFY_APP="$PWD/build/darwin-arm64/apilot.app/Contents/MacOS/apilot" scripts/verify-lifecycle.sh`。

针对已打包 App 重复 10 轮 SIGTERM 正常退出、10 轮 SIGKILL 崩溃。每轮通过真实 CLI 创建并启动带子进程的任务，记录父子 PID；退出后确认两者消失、CLI status 返回 APP_NOT_RUNNING/退出码3，再打开 App 验证任务和历史保留。正常退出记录 canceled，强杀恢复记录 interrupted。

20 轮全部通过；本轮最大清理耗时 0.068s（夹具范围内，不是任意命令保证）。原始结果在忽略的 tmp/lifecycle/results.json。SIGTERM 走与 Quit 共用的 OnBeforeQuit 清理路径；菜单点击本身未用鼠标自动化验收。

MyGo 的实际窗口关闭→驻留→重开 visible=true→App.Quit 已单独验证。休眠跨三个定时点、主动脱离进程组、助手自身也被杀仍未验收，不将本次结果外推。

最终助手与目标命令分别建立独立进程组；runner回归还覆盖了模拟App整个进程组被SIGKILL后的清理，避免终端信号同时终止清理助手。

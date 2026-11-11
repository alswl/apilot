# 有限执行助手 PoC

日期：2026-11-11；平台：macOS arm64；Go 1.27.3。范围是 internal/runner，尚未代表完整产品验收。

执行：`go test -v ./internal/runner`，随后 `go test -race ./internal/runner`。测试使用独立测试进程模拟 App 与同一可执行文件的私有助手模式，真实启动 /bin/sh、sleep 和父子进程；模拟 App 被 SIGKILL 后，检查记录的父子 PID 均已消失。

已验证：结构化参数不做 Shell 插值、双流文件、非零退出码、启动失败、超时、主动取消、App 强杀、握手 EOF、Shell 正常结束后的后台子进程清理、日志上限和启动前取消。race 检查通过。

修复：Shell 退出后其子进程仍持有 stdout/stderr 时，os/exec.Wait 可能等待管道不结束。配置有限 WaitDelay，识别其语义，再清理剩余目标进程组，避免误判 TIMEOUT。

助手只在 Run 存续期间存在；App 控制写端关闭触发 TERM，最多等待3秒后KILL。目标任务不会继承 App 的控制写端。没有调度、数据库、Socket 或重启能力。

限制：主动 setsid/换组的进程、助手自身被强杀不保证清理；本记录未验证真实 Codex。完整 App 的 Quit/重启、真实系统休眠和通知权限另行验收。

追加验证：助手与App隔离进程组，模拟App整个进程组退出后目标任务仍由有限助手清理；完整runner -race回归通过。

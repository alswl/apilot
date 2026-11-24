# 交付与验证

## 从 Issue 到发布

1. 在 GitHub Issue 定义一个问题或需求、预期结果和目标版本。涉及规格的改动在 Issue 链接对应的 `specs/`，把实现约束和可复用的验收步骤维护在 spec。
2. 一个 PR 解决一个 Issue。PR 正文用 `Closes #编号` 绑定 Issue，简述改动和 reviewer 需要留意的地方。
3. 每个 Issue 维护一份验收记录，放在 Issue 正文或一条持续更新的评论里。写明验证的 commit、命令或场景、实际结果和未验证项，按需链接 CI 和截图或录屏。PR 链接这份记录。
4. 合并后把 Issue 及其绑定 PR 纳入同一次版本发布，在 Issue 记录发布 tag 或 Release 链接。

## 验证顺序

1. 先验证 Core 和 API。开发中先跑受影响的定向测试，提交前按改动范围跑 `make test-core`、`make check-openapi` 和 `make test-connectors`。CI 先跑 Core、API 契约和连接器检查，再跑桌面测试。
2. 涉及工作台时，最后在 macOS 用实际场景走查；可复现的场景优先用 `make dev SCENE=…`。需要验证安装包、窗口、菜单或退出行为时，本地用 ad-hoc 签名打包，无需 Apple 证书或 API key。

   ```sh
   make app
   make VERIFY_SKIP_BUILD=1 verify-ui verify-lifecycle
   ```

3. 日常 PR 和主分支检查以测试为止。CI 在版本 tag 或显式请求签名公证时产出分发包，见[分发说明](../distribution.md)。

## 跟进 CI

1. 每次 push 后，查看该提交对应的 CI 运行。
2. 持续检查，直到所需检查都有最终结果。
3. 检查失败时，查看失败步骤或运行注释。如果 runner 未启动，在交付记录中注明测试未执行。如果测试已执行，记录首个失败步骤。

## 验证 Obsidian 回写

1. 获准只读检查真实 vault 时，核对路径、同步进度和日志。使用[工作台连接排查步骤](../workbench.md#排查连接失败或数据不一致)定位读取问题。
2. 在两个隔离 vault 中放置路径相同的笔记。验证回写只更新目标 vault。
3. 验证来源确认前保留原任务状态。让来源在提交前发生变化，确认回写失败时界面恢复原状态并显示原因。
4. 分别记录真实 vault 的只读结果和隔离 vault 的写入结果。

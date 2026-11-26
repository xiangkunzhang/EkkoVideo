# AGENTS.md — EkkoVideo 开发约束

> 修改代码或新增功能前，先阅读本文档以及 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)、[docs/ROADMAP.md](docs/ROADMAP.md)。

## 1. 核心架构红线

1. **纯原生 UI，禁用 WebView**：所有界面使用 `github.com/egoist/mygo/ui`（当前锁定 `v0.10.0`），严禁引入 HTML/CSS/JS 或 WebView。
2. **零 CGO**：严禁启用 CGO；`libmpv` 与 macOS/Windows 系统 API 一律使用 `github.com/ebitengine/purego` 或 `syscall`。
3. **独立网关子进程**：在线平台协议与 CENC/HLS 流媒体代理必须运行在 `--gateway` 子进程中，不得把在线平台业务耦合进主 GUI 进程；本地文件、IPTV、片库直链优先直连 `libmpv`。
4. **列表页与播放页按模式分离**：
   - 列表页复用 `BrowseShell`，按场景使用独立模板（`PosterGrid` / `FileBrowser` / `ChannelList` / `LiveGrid`）；
   - 播放页复用 `PlayerStage`，按场景通过 `PlayerProfile` 注入顶栏、底栏、侧栏/浮层与快捷键，禁止在 `PlayerStage` 中堆砌 `if isIPTV / if isLive` 分支。
5. **平台优先级**：当前以 **macOS** 为第一优先级，但平台相关代码必须隔离在 `*_darwin.go` / `*_windows.go` / `*_other.go` 中，保持核心包跨平台可编译。

## 2. MyGo 线程与生命周期铁律

- **主线程唯一状态**：UI 与 App 状态仅在主线程读写；所有网络、磁盘、解码、子进程调用放在 goroutine（通过 `safego.Go`），结果经 `win.Update(func() { ... })` 回到主线程。
- **禁止跨帧持有 `ui.Element`**：不得将 `ui.Element` 存入结构体或被 goroutine 捕获；跨帧焦点/测量使用 `ui.Handle`。
- **有状态控件必须带 Key**：循环和有状态输入控件构造前必须调用 `c.Key(...)`。
- **视图函数无副作用**：`view(c *ui.Context)` 会在单次事件内重建多次，异步加载必须有 `loading`/`started`/`pending` 守卫。
- **取消过期请求**：页面离开或切集/切台时取消对应 `context.Context`，并用 generation 序号丢弃迟到回调。
- **内存纪律**：长列表一律使用 `ui.GridView` / `ui.List` 虚拟化；进入播放页时清空 UI 海报位图缓存并取消待解码任务。

## 3. 开源合规约束

- 本仓库源码为 **MIT**，第三方组件声明维护在 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
- **严禁从 AGPL-3.0 项目（如 `live-mygo`）复制粘贴源代码**，仅允许参考通用设计思想后独立实现。
- 严禁在源码、测试默认值或提交中保存任何个人服务器地址、密码、Cookie、Token 或签名私钥。

## 4. 验证与交付

每次修改 Go 代码后必须运行：

```sh
gofmt -w .
go test -race ./...
go vet ./...
go tool mygo vet .
```

- 领域逻辑与 Provider 编写纯 Go 单元测试（使用录制夹具，不依赖外网）。
- 关键 UI 流程使用 `ui.NewTester` 编写无头渲染测试。

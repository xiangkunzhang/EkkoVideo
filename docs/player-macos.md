# M0 macOS 播放器技术验证结论（`docs/player-macos.md`）

> 验证日期：2026-11-26  
> 验证环境：macOS 15.7.3 (x86_64) · Go 1.26.2 (toolchain `go1.27.3`) · `github.com/egoist/mygo v0.10.0` · `purego v0.11.3` · `libmpv.2.dylib` (IINA 1.5.1 Universal)

---

## 1. 验证结果总览

| 验证项 | 结果 | 实测数据 / 结论 |
| :--- | :--- | :--- |
| **1. `purego` 动态加载 `libmpv`（零 CGO）** | **通过** | `purego.Dlopen` + `purego.Dlsym` + `purego.RegisterFunc` 完整绑定 `mpv_*` 与 `mpv_render_context_*` |
| **2. MyGo + `NSOpenGLView` 同窗合成** | **通过** | `NSOpenGLView` 置于 MyGo `surface.view` 下方（`NSWindowBelow`），视频区域 `ui.Transparent` 镂空，侧栏/控制条/弹幕直接在同一个 `ui.Context` 叠画 |
| **3. 1000 条/分钟弹幕压测** | **通过** | 5 秒实测：`status="playing"`、`gl_frames=288`（约 57.6 fps）、`ui_fps=60.0`、`danmaku_spawned=83`（约 996 条/分钟），UI 保持满帧 60 FPS |
| **4. 静态检查与打包** | **通过** | `go vet ./spike/...`、`go tool mygo vet ./spike`、`go build` 与 `.app` 打包均通过 |

---

## 2. 关键技术踩坑与锁定方案

### 2.1 视图层级与同窗透明合成
- MyGo `v0.10.0` 在 macOS 主窗口 `contentView` 中挂载顶层 Metal 渲染视图（`CAMetalLayer`）。
- 挂载 `NSOpenGLView` 时，必须使用：
  ```objc
  [contentView addSubview:glView positioned:NSWindowBelow relativeTo:topMyGoView];
  ```
  同时 `c.Root().Background(ui.Transparent)` 与视频区容器 `Background(ui.Transparent)` 保持透明，即可零拷贝透出底层 OpenGL 视频画面，并在同一 `ui.Context` 上层绘制所有原生控件与弹幕。

### 2.2 `libmpv.2.dylib` 的 `@rpath` 加载问题（重要）
- **现象**：直接 `go build` 出的纯 Go 二进制没有 `LC_RPATH`，`dlopen("/.../libmpv.2.dylib")` 会因找不到 `@rpath/libass.9.dylib` 而失败。
- **解法**：
  1. 开发期：对二进制执行 `install_name_tool -add_rpath <libmpv所在目录> <bin>`（或将 `libmpv.2.dylib` 及其依赖放入 `resources/darwin/player/`）。
  2. 打包期：将 `libmpv.2.dylib` 及所有 `@rpath` 依赖复制进 `Contents/Resources/player/`（或 `Contents/Frameworks/`），并为可执行文件添加 `@executable_path/../Resources/player` 的 `LC_RPATH`，配合 `go tool mygo build` 签名后即可脱离本机环境独立运行。

### 2.3 MyGo `v0.10.0` API 实测细节
- `p.Image(bmp, rect, ui.Contain)` 第 3 个参数为 `ui.Fit`，非 `ui.ImageOptions`。
- `c.OnShortcut(0, key, fn)` 无修饰键时传 `0`。
- 颜色字面量使用 `ui.RGB(255, 255, 255)` 或 `ui.Hex("#ffffff")`（无 `ui.White` 常量）。
- `mpv_wait_event` 返回 `*mpvEvent` 并在结构体中直接声明 `unsafe.Pointer` 字段，可通过 `go vet` 的指针安全检查。
- 单后台槽（`chan struct{}, 1`）执行 `ui.Render(..., 2)` 生成 2× 描边弹幕 `*ui.Bitmap`，完全避免并发碰 MyGo 共享字形图集，60 FPS 满帧稳定。

---

## 3. M0 结论

**macOS 主方案（`NSOpenGLView` + `vo=libmpv` + MyGo 同窗透明叠层 + 2× 离屏弹幕位图）验证通过，可以正式进入 M1。**

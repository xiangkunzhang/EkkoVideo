# Third-Party Notices & Licensing Plan

## 许可策略说明

1. **EkkoVideo 自身源码**：采用 **MIT License**（见 [LICENSE](LICENSE)）。
2. **核心 Go 依赖**：仅引入宽松开源协议（MIT / Apache-2.0 / BSD-3-Clause）的库，确保源码层清晰可控。
3. **动态加载的 `libmpv` 与编解码库**：
   - EkkoVideo 通过 `purego` 在运行时动态加载 `libmpv`，不静态链接 C/C++ 代码。
   - 默认构建的 `libmpv`（如 Homebrew `mpv` 或官方 GPL 构建）包含 **GPL-2.0+ / GPL-3.0** 组件（如 FFmpeg GPL 编解码器）。
   - 因为 EkkoVideo 为开源项目（不需要闭源分发），发布包含默认 `libmpv` 的二进制安装包时，整体发行包遵守对应 GPL 条款，在 Release 中随附本声明文件、第三方许可证文本及完整构建说明。
   - 若未来需要无 GPL 传染的发行包，只需替换为 LGPL-2.1+ 构建的 `libmpv` 动态库，无需修改 EkkoVideo 源码。
4. **严格隔离强传染代码**：
   - 严禁从 AGPL-3.0 项目（如 `live-mygo`）直接复制粘贴源代码到本仓库；仅可独立重新实现通用工程思想。

---

## 第三方组件清单

| 组件 | 仓库 / 来源 | 许可证 | 用途 |
| :--- | :--- | :--- | :--- |
| **MyGo** | https://github.com/egoist/mygo | MIT | 纯 Go 原生桌面 UI 框架与打包工具 |
| **purego** | https://github.com/ebitengine/purego | Apache-2.0 | 零 CGO 动态库与 Objective-C 运行时绑定 |
| **libmpv** | https://github.com/mpv-player/mpv | GPL-2.0+ / LGPL-2.1+ | 视频解码与渲染引擎（运行时动态加载） |
| **FFmpeg**（随 libmpv 分发） | https://ffmpeg.org | LGPL-2.1+ / GPL-2.0+ | 音视频解封装与编解码底层库 |

> 后续新增第三方依赖（如 `modernc.org/sqlite` 等）时，需同步更新本清单。

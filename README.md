# Velan

**纯 Go 原生跨平台全能视频播放器** —— 基于 [MyGo](https://github.com/egoist/mygo) 原生 UI 与 `libmpv` 构建，**不使用 WebView、不依赖 CGO**。

> 当前处于 **架构落地 & M0 技术验证** 阶段，优先打磨 **macOS** 体验，随后适配 Windows 与 Linux。

---

## 核心能力规划

1. **多在线平台播放**：可插拔 Provider 架构，支持多平台点播（短剧/剧集浏览、分类、榜单、搜索、多季选集、剧集推荐）与后续直播平台接入。
2. **IPTV 直播**：支持 M3U/M3U8 订阅管理、XMLTV 节目单（EPG）、分组与台标、遥控器式防抖换台（1s dwell / 数字键 / 回看上一台）、同名频道多线路自动切换与记忆。
3. **本地视频播放**：文件/文件夹/拖入/文件关联打开、同目录自然排序播放列表、外挂字幕自动加载、续播记忆，以及后续本地媒体库扫描。
4. **原生弹幕引擎**：点播按时间分段拉取、直播长连接、本地 XML/ASS 弹幕、弹弹play 匹配；纯原生 2× 离屏字图缓存渲染，支持过滤、防重叠、区域/透明/速度控制。
5. **片库管理与播放**：后续接入 Emby / Jellyfin、飞牛影视（fnOS）、Alist / OpenList、WebDAV，支持播放进度双向同步与凭据钥匙串保护。
6. **跨平台与稳定性**：macOS 优先（Apple Silicon / Intel），Windows x64 次之，Linux 兜底；遵循常规桌面播放器交互习惯，独立子进程隔离协议与流媒体代理，保障主界面稳定流畅。

---

## 架构亮点

- **零 WebView、零 CGO**：界面由 `egoist/mygo/ui` 直接在 GPU 上绘制；`libmpv` 与平台原生接口全部通过 `ebitengine/purego` 动态绑定。
- **列表页与播放页按场景分离**：
  - 列表层：`BrowseShell` 公共底座 + 4 套独立模板（海报墙 `PosterGrid` / 文件目录 `FileBrowser` / IPTV 频道表 `ChannelList` / 直播间网格 `LiveGrid`）。
  - 播放层：`PlayerStage` 公共视频舞台 + `PlayerProfile` 4 槽位装配（顶栏 / 底栏 / 侧栏或浮层 / 场景快捷键），让点播选集推荐、IPTV 频道 EPG、直播实时聊天互不干扰。
- **独立网关子进程（Media Gateway）**：在线平台协议、CENC Range 原地解密、HLS 改写与签名刷新运行在独立的 `127.0.0.1` 子进程中，与主进程隔离崩溃风险，主进程退出自动回收。

详细设计请阅读：
- [系统架构与核心设计](docs/ARCHITECTURE.md)
- [分阶段里程碑路线图](docs/ROADMAP.md)
- [开发规范与 Agent 协作约束](AGENTS.md)

---

## 里程碑路线图

| 阶段 | 重点交付 | 目标平台 |
| :--- | :--- | :--- |
| **M0 技术验证** | MyGo 窗口 + `purego` 加载 `libmpv` 合成验证、原生弹幕压测、打包验证 | macOS 优先 |
| **M1 骨架与本地播放** | 应用壳、`PlayerStage` + `VODSingle` + `FileBrowser`、完整播放器交互、独立网关框架 | macOS |
| **M2 在线平台** | Provider Registry、`PosterGrid` + 详情页 + `VODSeries`（选集/推荐）、多平台迁移与 CENC/HLS 代理 | macOS |
| **M3 原生弹幕** | 弹幕规范化/过滤/调度/2× 字图渲染、点播弹幕与本地 XML/ASS 接入 | macOS |
| **M4 IPTV** | M3U 订阅、XMLTV EPG、`ChannelList` + `IPTVProfile`、防抖换台与多线路自愈 | macOS |
| **M5 片库与跨平台** | Emby/Jellyfin、飞牛 fnOS、Alist、WebDAV、本地库扫描；补齐 Windows 原生适配与直播间 Profile | macOS + Windows |
| **M6 稳定性与发布** | 自动更新、系统媒体控件（Now Playing / SMTC）、诊断导出、Linux 支持、签名公证与正式发布 | macOS / Windows / Linux |

---

## 开发环境要求

- **Go**：`1.27.3+`（`go.mod` 已通过 `tool` 声明锁定 `github.com/egoist/mygo v0.10.0`）
- **macOS**：macOS 13+，Xcode Command Line Tools，并通过 Homebrew 安装 `mpv`（提供 `libmpv.dylib`）：
  ```sh
  brew install mpv
  ```
- **常用命令**（M1 骨架落地后启用）：
  ```sh
  go tool mygo dev        # 开发模式运行
  go test -race ./...     # 单元测试与竞态检查
  go tool mygo vet .      # MyGo 静态检查（UI 元素生命周期与线程安全）
  go tool mygo build      # 构建应用包
  ```

---

## 开源协议与许可计划

- **本项目源码**采用 [MIT License](LICENSE) 开源，便于社区自由学习、贡献与复用。
- **第三方依赖与发行包**：预构建发行包随附动态加载的 `libmpv` 及其编解码依赖（默认构建遵循 GPL-2.0+ / GPL-3.0 协议）。根据 GPL 要求，发行包的分发将同步遵循对应开源义务并提供完整构建脚本与源码声明；详见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

## 免责声明

Velan 仅为本地与网络媒体播放工具，自身不提供、不存储、不分发任何视频或直播内容。使用本软件访问第三方在线平台、IPTV 订阅或自建片库时，请遵守相关服务条款及当地法律法规，内容版权归原权利人所有。

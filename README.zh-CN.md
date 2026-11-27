<div align="center">
  <img src="assets/icon.png" width="112" height="112" alt="Velan Logo" />
  <h1>Velan · 微澜</h1>
  <p><strong>基于纯 Go + MyGo + purego libmpv 的零 WebView、零 CGO 原生桌面影音播放器</strong></p>
  <p>
    <a href="README.md">English</a> | 简体中文
  </p>
</div>

---

**Velan（微澜）** 将**本地视频**、**在线短剧**、**原生 2x 弹幕**、**IPTV 电视直播**与**网络直播间**统一整合在一个轻量、高速的原生桌面窗口中。

## 核心特性

- **纯原生高性能（零 WebView · 零 CGO）**：`libmpv` 通过 `purego` 动态加载，macOS 原生 OpenGL 与 Windows Win32 子窗口 `HWND` 同窗渲染，支持硬件解码（`auto-safe`）。
- **本地影音与网络流**：目录浏览、记忆上次目录与播放断点（按剧集聚合去重）、直链 URL 播放（`Cmd/Ctrl + U`）及拖拽打开。
- **在线短剧与隔离网关**：第三方接口运行在独立 `VelanGateway` 子进程中（随机端口 + Bearer Token + 崩溃自愈），支持全集恢复、百集分段、剧集推荐及自动连播下一部（更多平台可在「设置」中手动开启）。
- **原生 2x 高对比度弹幕**：8 方向描边 + 投影、时间轴同步防重叠轨道、本地 XML/ASS 自动发现、播放器内实时调节字号/透明度/速度/显示区域。
- **IPTV 电视直播（M3U + XMLTV）**：同名频道多线路自动合并与故障自愈、`.xml`/`.xml.gz` EPG 节目单与实时进度条、台标展示、收藏置顶、1 秒防抖换台与数字选台。
- **纯净直播间（有弹幕、无礼物）**：独立 16:9 宽屏直播广场与 `LiveRoomProfile`，支持 B站直播 / 虎牙 / 斗鱼实时聊天弹幕、线路切换与快速切台，彻底屏蔽礼物与打赏干扰。

## 快速构建

```bash
# 1. 开发运行
go run .

# 2. 打包 macOS（Intel + Apple Silicon 双架构，自动内置通用 libmpv 与 Velan.icns）
bash scripts/build-macos.sh darwin/amd64,darwin/arm64

# 3. 打包 Windows x64（可直接在 macOS/Linux 交叉编译，内置 libmpv-2.dll）
bash scripts/build-windows.sh
```

更多架构说明见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)，开源许可与第三方声明见 [LICENSE](LICENSE) 与 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

## 免责声明

Velan 仅为本地与网络媒体播放工具，自身不提供、不存储、不分发任何视频或直播内容。使用本软件访问第三方在线平台、IPTV 订阅或自建片库时，请遵守相关服务条款及当地法律法规，内容版权归原权利人所有。

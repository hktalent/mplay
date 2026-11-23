# mplay · 随身影音

[![ci](https://github.com/hktalent/mplay/actions/workflows/ci.yml/badge.svg)](https://github.com/hktalent/mplay/actions/workflows/ci.yml)
[![release](https://github.com/hktalent/mplay/actions/workflows/release.yml/badge.svg)](https://github.com/hktalent/mplay/actions/workflows/release.yml)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

下载安装包：[Releases](https://github.com/hktalent/mplay/releases) —— macOS 通用（Intel + Apple Silicon）、Linux x64、Windows x64，解压即用。

本地优先的音视频播放器：扫描媒体目录 → 按文件名/标签自动配歌词与字幕 → 播放时逐行同步。
**同一份前端代码既是网站（m.51pwn.com），也是 macOS / Linux / Windows 桌面包（urlbox 外壳）。**

```
┌──────────────┐   HTTP(回环)    ┌─────────────────────────────┐
│ urlbox 窗口  │ ───────────────▶│  mplay 单二进制（Go）        │
│ 或浏览器     │◀──── 同一份前端 │  go:embed web/ · 零 CGO      │
└──────────────┘                 │  曲库索引 / ID3·FLAC·MP4     │
                                 │  LRC·SRT·VTT·ASS 解析        │
                                 │  LRCLIB·OpenSubtitles·网易云 │
                                 └─────────────────────────────┘
```

## 能力

**播放**
- 本地音视频：MP3/FLAC/M4A/AAC/OGG/OPUS/WAV/APE… 与 MP4/MKV/WEBM/MOV/AVI…
- HTTP Range 流式（视频拖进度条、边下边播），封面从标签里取并缓存
- 播放队列、上一/下一首、±10s、倍速、音量、全屏、迷你播放条
- 直接把本地文件拖进页面播放（不写入曲库，同名 `.lrc/.srt` 一并识别）

**歌词 / 字幕**
- 匹配优先级：同名歌词/字幕 → 文件内嵌（ID3 USLT/SYLT、FLAC、MP4）→ 本地缓存 → LRCLIB →（视频）OpenSubtitles
- 解析 LRC（含 `[offset:]`、多时间戳、增强型逐字 `<mm:ss.xx>`）、SRT、VTT、ASS/SSA
- 双语合并：同时间戳行 / 双文件语言互补 / SRT 双行 → 主行 + 译文行
- 编码识别：UTF-8 BOM / UTF-16 / 严格 UTF-8 / GB18030 兜底（中文老歌词不乱码）
- 逐字高亮、点击歌词跳转、±0.1s 手动校时、大字模式、导出 LRC、手动导入
- 视频字幕压在画面上（中文主行 + 译文行），字号随窗口自适应

**搜索**
- 在线搜歌：Apple 试听（30s 官方预览）、Internet Archive（公有领域/CC 可完整播放）、Jamendo（可选）、**自定义音源插件**（一行 JSON 接自己的接口）
- 输入一句歌词反查歌曲：本地歌词全文索引（可直接跳到那句的时间点）+ 网易云歌词检索（只要元信息）+ 歌词库名称匹配

**页面**
- 顶栏 / 底栏 `position: fixed` 始终可见，正文与浮层用 `--header-h / --footer-h` 实时留位
- 底栏链接来自 `site-links` 单一数据源（`python3 site-links/sync.py` 生成，不手改 `web/footer.js`）
- 移动端适配：390px 宽度下顶栏分两行、播放条自动换行

## 快速开始

### 网页 / 服务端

```bash
cd /Users/51pwn/MyWork/mplay
./scripts/build.sh                 # 产出 bin/mplay（前端已内嵌）
./bin/mplay -media ~/Music -open    # 默认 127.0.0.1:8737
```

启动后：`http://127.0.0.1:8737`。「设置」里可以加媒体目录、开关歌词源、填 OpenSubtitles key、自定义音源、访问令牌。

服务端部署（给公网用）：

```bash
./bin/mplay -addr :8737 -media /srv/media
# 或 systemd / Docker，见 packaging/
```

### 桌面

```bash
./scripts/desktop.sh               # = build + 本地起服务 + urlbox 开窗口
```

- 桌面模式只监听回环地址，端口被占用自动顺延（最多 10 个）
- urlbox 查找顺序：`MPLAY_URLBOX` → PATH → 二进制同目录 → `~/MyWork/urlbox/target/release` → `/usr/local/bin`、`/opt/homebrew/bin`
- 找不到 urlbox 会自动退回系统浏览器打开，不会启动失败
- 桌面多出三个能力：**在文件夹中显示**、**用系统播放器打开**、系统级全屏

### 三平台打包

```bash
./scripts/package.sh               # 服务端/网页版：dist/mplay-<ver>-{os}-{arch}.*（5 个目标）
./scripts/package-desktop.sh       # 桌面版：mplay + urlbox + 双击启动脚本，macOS 额外产出 MPlay.app
```

- 服务端包是**单个可执行文件**（前端已 embed），Windows 双击 `mplay.exe`，macOS/Linux `./mplay -open`
- 桌面包需要先有 `urlbox`：`MPLAY_URLBOX=/path/to/urlbox ./scripts/package-desktop.sh`
- 桌面版包结构：`bin/mplay` + `bin/urlbox` + `start-mplay.{sh,command,cmd}` + `使用说明.txt`；macOS 另有 ad-hoc 签名的 `MPlay.app`

### 自动发布（GitHub Actions）

`.github/workflows/release.yml` —— 打 tag 即自动编译三平台桌面包 + 服务端包并发布 Release：

```bash
git tag v1.0.1 && git push origin v1.0.1
```

| 产物 | 说明 |
| --- | --- |
| `mplay-desktop-<ver>-macos-universal.zip` | 内含 `MPlay.app`（mplay + urlbox 已 lipo 成通用二进制） |
| `mplay-desktop-<ver>-linux-x64.tar.gz` | `./start-mplay.sh` |
| `mplay-desktop-<ver>-windows-x64.zip` | `start-mplay.cmd` |
| `mplay-<ver>-{darwin,linux}-{amd64,arm64}.tar.gz`、`mplay-<ver>-windows-amd64.zip` | 服务端/网页版单文件 |
| `SHA256SUMS` | 校验和 |

CI 里 `urlbox` 是现场 `cargo build` 的（Linux 会装 webkit2gtk 依赖），所以三个桌面包里的窗口外壳与上游同版本。
手动触发：Actions → release → Run workflow（可填版本号、可选草稿）。

`.github/workflows/ci.yml` —— 每次 push：`gofmt` 校验、`go vet`、`go build`、前端 `node --check`、脚本 `bash -n`，再起一次服务做 HTTP 冒烟。

## 常用参数与环境变量

| 参数 | 说明 |
| --- | --- |
| `-addr` | 监听地址，`127.0.0.1:8737` 或 `:8737` |
| `-media` | 媒体目录，可重复 |
| `-desktop` | 桌面模式（只绑回环 + 拉 urlbox） |
| `-open` | 启动后用系统浏览器打开 |
| `-lan` | 允许局域网访问 |
| `-config` / `-cache` | 配置文件 / 缓存目录 |
| `-no-scan` | 启动时先不扫描 |
| `-version` | 打印版本 |

环境变量：`MPLAY_ADDR`、`MPLAY_MEDIA`（多个用 `:` 分隔）、`MPLAY_LAN`、`MPLAY_JAMENDO_CLIENT_ID`、`MPLAY_OS_API_KEY`、`MPLAY_TOKEN`、`MPLAY_URLBOX`、`MPLAY_TRACE=1`（调试日志）。

配置文件：`~/.config/mplay/config.json`；缓存：`~/.cache/mplay`（`lyrics/`、`subtitles/`、`covers/`、`library.json`）。

## HTTP API（前端就是它的第一个客户）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/health`、`/api/version` | 健康检查、版本 |
| GET/POST | `/api/config` | 配置读写（密钥只回布尔） |
| GET | `/api/library?q=&kind=&filter=&sort=&limit=` | 曲库，`filter=lyric|nolyric`，`kind=audio|video` |
| POST | `/api/library/scan` | 重新扫描 |
| GET | `/api/track/{id}`、`/api/stats` | 单曲详情、统计 |
| GET | `/api/lyric/{id}?force=1&pick=` | 取歌词（按优先级自动解析） |
| POST | `/api/lyric/{id}?ext=.lrc` | 原始文本导入（body 为纯文本） |
| GET | `/api/lyric/{id}/candidates` | LRCLIB 候选列表 |
| GET | `/api/remote/lyric?track=&artist=&album=&duration=&pick=` | 在线歌词（不依赖本地曲目） |
| GET | `/api/search?q=&limit=` | 在线搜歌 |
| GET | `/api/search/lyrics?q=` | 歌词片段反查（local / remote / library） |
| GET | `/api/sources` | 当前音源及启用状态 |
| POST | `/api/open` | `{"id":..,"action":"reveal"|"player"}`（仅桌面） |
| GET | `/media/{id}`、`/cover/{id}` | 媒体流（Range）、封面 |

设置 `authToken` 后，`/api` 与 `/media` 都要求 `X-Mplay-Token` 头、`?token=` 或 `mplay_token` Cookie。

## 部署到 m.51pwn.com

```bash
./scripts/build.sh
sudo install -Dm755 bin/mplay /usr/local/bin/mplay
sudo useradd -r -s /sbin/nologin mplay
sudo mkdir -p /srv/media /srv/mplay/cache && sudo chown -R mplay:mplay /srv/mplay
sudo cp packaging/mplay.service /etc/systemd/system/ && sudo systemctl enable --now mplay
sudo cp packaging/nginx-m.51pwn.com.conf /etc/nginx/conf.d/m.51pwn.com.conf && sudo nginx -s reload
```

- Nginx 已关 `proxy_buffering` 并对 `/media` 放宽超时，拖动进度条不受影响
- 公网暴露建议同时设置 `MPLAY_TOKEN`（写进 `/etc/mplay/env`）
- 容器：`docker build -f packaging/Dockerfile -t mplay . && docker run -p 8737:8737 -v ~/Music:/media mplay`

## 维护

- **页脚**：唯一数据源是 `../site-links/links.json`，改完跑 `python3 ../site-links/sync.py --only mplay`；`web/footer.js` 是生成物，不要手改，`web/index.html` 里的 `?v=` 会被自动同步。
- **代码结构**：
  - `main.go` 启动/监听/信号；`webembed.go` 内嵌前端
  - `internal/config` 配置与环境变量；`internal/media` 扫描、标签、文件名解析、封面、Range
  - `internal/lyrics` LRC/SRT/VTT/ASS 解析、编码、LRCLIB、字幕库、片段索引
  - `internal/music` 在线音源聚合、网易云歌词检索、自定义音源
  - `internal/server` 路由与 API；`internal/desktop` urlbox/浏览器/系统播放器
  - `web/` 前端：`index.html` 结构、`style.css` 固定头尾与播放器、`lyrics.js` 解析+同步、`player.js` 播放引擎、`app.js` 路由与业务

## 已知限制（实测得出的，不是猜测）

- 浏览器内核不支持 `.mkv/.avi/.wmv` 等容器时会解码失败；界面会提示并给出「用系统播放器打开」（桌面模式），Web 模式下只能换 `.mp4/.webm`。
- 在线搜歌的**可播放**来源只有两类：30 秒官方试听（Apple）与公有领域/CC 音频（Internet Archive、Jamendo、自定义音源）。项目不内置任何第三方音乐平台的音频地址。
- LRCLIB 只能按歌名/歌手匹配（实测不搜歌词正文），所以「歌词片段反查」靠**本地全文索引 + 网易云元信息检索**，网易云命中只给歌名/歌手/主页，不给音频。
- 歌词片段索引只覆盖**本地曲库**（含已缓存的远端歌词）；纯在线试听拉到的新歌词不会进索引。
- Internet Archive 的搜索接口在国内偶发超时（8s），失败会在结果里提示「部分音源失败」。

## 已验证

- 曲库：2 音频 + 1 视频，歌词 3 条，`cueCount` 正确
- 晴天（同名词幕）8 行同步；Blank Space（LRCLIB）82 行；视频 SRT 双语（中文主行 + 英文译文）
- Range：`206 Partial Content` + 正确 `Content-Range`
- 歌词片段 `故事的小黄花` → 本地命中「晴天 @0.5s」，网易云返回 10 条并带命中歌词行
- 播放器页面：歌词逐行高亮随进度滚动、视频字幕压在画面上、顶栏/底栏始终可见

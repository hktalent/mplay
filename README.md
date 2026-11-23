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
| POST | `/api/track/{id}/identify` | 单曲识别歌名（body `apply/audio/fetchLyrics`） |
| POST | `/api/library/identify` | 批量识别未匹配曲目（body `limit/onlyMissing/apply`） |
| POST | `/api/ai/check` | 探测 AI 端点是否可用 |
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

## 智能识别歌名（本地规则 + 可选 AI）

资源站下来的文件经常叫 `01. 某站 - 歌手 - 歌名 [320k].mp3`，甚至是 `abc1234.mp3`，标签也没有。识别流程分三段，**越往后越贵，所以按顺序短路**：

1. **本地候选**：内嵌标签 → 文件名（正序 + 反序 + 去括号/去后缀）→ 已有歌词第一句。零成本、离线可用。
2. **歌词库校验**：拿候选去 LRCLIB 搜，按「歌名 0.62 / 歌手 0.23 / 时长 0.15」加权打分，≥0.62 才算命中，直接把**歌词库确认过的歌名**写回曲库（这一步不需要 AI）。
3. **AI 兜底**：只有前两步都没把握（未命中或置信 <0.8）才问模型。模型返回的答案**仍要过一遍歌词库校验**：
   - 批量识别（`POST /api/library/identify`）只写回「校验通过」的结果，没把握的条目在响应里给候选建议，不落库；
   - 单曲识别（`POST /api/track/{id}/identify`）校验通过或模型置信 ≥0.75 才写回，否则返回 `apply:false` 并提示「结果没把握，未写回：最接近 …」，避免用噪声覆盖原来的文件名线索。

### 配置（设置页 →「AI 元数据识别（可选）」）

| 字段 | 说明 |
| --- | --- |
| `aiIdentify` | 总开关。关闭时只用本地规则 + 歌词库，功能照常可用 |
| `aiBaseUrl` | OpenAI 兼容端点，如豆包方舟 `https://ark.cn-beijing.volces.com/api/v3`、`https://api.openai.com/v1`，自建 vLLM/Ollama 同样填 `/v1` |
| `aiModel` | 模型/推理接入点 ID，如 `doubao-seed-1-6` |
| `aiApiKey` | 密钥（保存后接口只回 `aiConfigured` 布尔，不回明文） |
| `aiUploadAudio` | 允许把音频片段发给模型。默认关闭；打开后只在**单曲手动点「识别」**时上传，批量识别永远只发文本 |

- 界面入口：曲库工具条「识别未匹配」（批量，默认只处理缺歌名/歌手的音频，单次上限 20 条、总时长 90 秒）；播放器歌词工具条「识别」（单曲，可带上音频片段）；设置页「测试连通」。
- 音频上传细节：只取文件前 6 MB，`m4a/mp4` 按 `mp4` 传、其余按扩展名传，走 `input_audio`（base64），失败自动降级为纯文本识别。
- 隐私边界：不开 `aiUploadAudio` 时，只有文件名、时长和歌词前几行会离开本机；开了也只在你手动点单曲识别时上传片段。
- 不开 AI 完全能用：本地规则 + LRCLIB 校验这条路没有任何外部模型参与。

## 文件名清洗与歌词匹配（多候选 + 双门槛）

上一版是「猜一个结果就定死」，脏文件名很容易把歌手和歌名拆反，或者命中的是网友投稿的脏条目（`周杰伦-稻香`、`成都 - 陳一發兒` 这类）。现在的做法是**先给一堆假设，再让歌词库裁决**：

1. **清理**（`internal/media/guess.go`）：去扩展名、去 `[www.xxx.com]` 站点前缀、括号里按关键词整段丢弃（`320k/128k/192k/flac/ape/wav/hires/hq/4k/1080p/720p/mp3/m4a/live/remix/official/lyrics/instrumental/无损/高清/完整版/伴奏/铃声/音悦台/酷狗/9ku...`），括号外的裸噪音 token 也剥掉，去 `01.` 序号、`- mp3` 尾巴、`feat./ft.`（带词边界，`Taylor Swift` 不会被吃）；非噪音括号保留内容。
2. **候选**（`GuessNames` / `GuessWithTags`）：按 ` - `、` — `、` – `、`－`、`|`、`、`、` _ ` 依次拆，裸 `-` 只在两侧都是中日韩文字时才拆（`Taylor-Swift` 不拆），再叠加「无分隔符时首段/末段当艺人」两种朝向，加上标签与整名兜底，产出**排序过的候选队列**。
3. **裁决**（`internal/lyrics/match.go` 的 `BestMatch`）：每个候选去 LRCLIB 搜，命中记录要同时过**歌名 ≥0.75、艺人 ≥0.55** 双门槛（艺人相似度取「规范化相似度 / 字符集重合」的较大值，兜住 `邓紫棋/鄧紫棋`、`G.E.M./GEM` 的写法差）。评分里带时长接近、带时间轴、多记录支持等加分，并对**反序脏条目**（`track≈你的艺人 && artist≈你的歌名`）扣 25 分、**名字带水印**（`mp3/320k/完整版/无损...`）扣 30 分，因此规范记录会把网友投稿的脏条目挤掉。命中得分 ≥150 时提前收工，省请求。
4. **回填**：确认过的写法只写进曲库索引（`metaSource=lrclib`），**不动你的音频和标签**，标签可信时不覆盖。

实测 28 条真实脏文件名（`01. 夜空中最亮的星 - 逃跑计划 [320k].mp3`、`[www.yymp3.com]陈奕迅-十年.mp3`、`03 晴天 周杰伦.mp3`、`赵雷《成都》.wav`、`周深_大鱼_完整版.mp3`、`Eason Chan - 富士山下.mp3` …）：**27/28 命中规范记录**，原先「命中但命中错条目」的 4 条全部被纠正；剩下的 `S.H.E - 中国话.mp3` 单曲复测能命中（`中国话 / S.H.E`），批量跑时是撞上 LRCLIB 限流，后台缓存会在下一轮自动重试。

## 后台自动缓存歌词

扫描完不再需要你一首首点开播放器才会拉歌词：

- 曲库扫描成功 → 自动开始后台缓存（`Library.OnScanned` 挂钩）；服务启动时也会先跑一轮；设置里改了开关同样立即触发。
- **异步、限并发**：独立 worker（2 并发 + LRCLIB 侧 600ms 节流、429/503 退避重试），走的是和播放同一条解析链路，不占播放请求、不影响听歌。
- **只补缺的**：仅处理音频条目里「没有歌词、没有内嵌字幕、缓存也未命中」的，已有的跳过。
- **边缓边显示**：每首命中立刻回填曲库（歌词角标 + 时长/歌名纠正），曲库页角标实时显示 `缓存歌词 3/12`。
- **失败会重试**：限流或网络失败留在待办里，下一轮（15 秒一次心跳）自动接着做。

| 配置 / 接口 | 说明 |
| --- | --- |
| `autoCacheLyrics` | 默认 **true**。设置页 →「歌词自动缓存」开关 |
| `GET /api/cache/status` | `{enabled,lrclib,pending,audio,withLyric,progress:{running,total,done,hit,miss,...}}` |
| `POST /api/cache/run` | 立即缓存缺歌词的曲目；已在跑返回 `{already:true,pending}`，无需可缓则返回 `hint` |

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
  - `internal/identify` 歌名识别：本地候选生成 + LRCLIB 打分校验 + OpenAI 兼容客户端
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
- 歌名识别（假 AI 端点 + 真 LRCLIB 实测）：`abc1234.mp3` 经 AI 给出「夜空中最亮的星 - 逃跑计划」，再被 LRCLIB 校验为 0.99 写回并拉到 33 行同步歌词；AI 只说「测试歌曲/测试歌手」(0.55) 时单曲识别返回 `apply:false`、批量只给建议不落库；未配置 AI 时功能退化为纯本地候选，不报错
- 设置页：填 base/model/key → 保存 → 「测试连通」回显模型名，`/api/config` 只回 `aiConfigured` 布尔

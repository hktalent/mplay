# mplay · 随身影音

**简体中文** | [English](README.md)

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
                                 │  LRCLIB·酷狗·网易云(可选)     │
                                 │  OpenSubtitles               │
                                 └─────────────────────────────┘
```

## 能力

**播放**
- 本地音视频：MP3/FLAC/M4A/AAC/OGG/OPUS/WAV/APE… 与 MP4/MKV/WEBM/MOV/AVI…
- HTTP Range 流式（视频拖进度条、边下边播），封面从标签里取并缓存
- 播放队列、上一/下一首、±10s、倍速、音量、全屏、迷你播放条
- 音量常驻在进度条同一行（扬声器静音按钮 + 百分比），任何宽度都能调；`M` 静音、`↑/↓` 步进 5%
- 进度条上方常驻**当前歌词条**：唱到哪句随时可见（含译文），前奏/间奏与纯音乐分别给说明；切到曲库页时迷你条也用这句代替歌手名
- **最小化（迷你条）也是完整播放器**：切到曲库/搜索页后浮在底部的迷你条带滑动音量、上一首、播放/暂停、下一首，外加一条 12 根竖条的小频谱；点封面或标题回到播放页。从迷你条切歌不会把你拽回播放页。
- **音频可视化**：播放区下方一条频谱带，用 WebAudio `AnalyserNode` 读真实频谱（48 根竖条，低频占更多格子、峰值平滑衰减）；播放时封面/字号/背景光轻微呼吸
- 直接把本地文件拖进页面播放（不写入曲库，同名 `.lrc/.srt` 一并识别）

**歌词 / 字幕**
- 匹配优先级：同名歌词/字幕 → 文件内嵌（ID3 USLT/SYLT、FLAC、MP4）→ 本地缓存 → LRCLIB → 酷狗 → 网易云（默认关）→ 同名借词兜底 →（视频）OpenSubtitles
- 纯音乐不硬配：歌词库确认没词的（酷狗返回「纯音乐，请欣赏」）标成「无歌词，纯音乐」，不占歌词统计、不再反复重试
- 解析 LRC（含 `[offset:]`、多时间戳、增强型逐字 `<mm:ss.xx>`）、SRT、VTT、ASS/SSA
- 双语合并：同时间戳行 / 双文件语言互补 / SRT 双行 → 主行 + 译文行
- 编码识别：UTF-8 BOM / UTF-16 / 严格 UTF-8 / GB18030 兜底（中文老歌词不乱码）
- 逐字高亮、点击歌词跳转、±0.1s 手动校时、大字模式、导出 LRC、手动导入
- 视频字幕压在画面上（中文主行 + 译文行），字号随窗口自适应

**搜索**
- 在线搜歌：Apple 试听（30s 官方预览）、Internet Archive（公有领域/CC 可完整播放）、Jamendo（可选）、**自定义音源插件**（一行 JSON 接自己的接口）
- 输入一句歌词反查歌曲：本地歌词全文索引（可直接跳到那句的时间点）+ 网易云歌词检索（只要元信息）+ 歌词库名称匹配

**曲库**
- 顶部四个数字：**全部 / 音频 / 有歌词 / 待配歌词**，点一下就是筛选（口径见下面「曲库四个数字」）
- 工具条「打开本地文件夹」：本机会弹**系统目录选择框**（macOS `choose folder` / Linux zenity·kdialog / Windows FolderBrowserDialog），选中的目录自动写进 `mediaDirs` 并立即扫描；已存在的目录不会重复添加
- 没有图形会话的纯网页部署（systemd / 容器 / 远程）会自动退到浏览器目录选择：把这个文件夹里的音视频**临时**塞进播放队列，不写曲库（浏览器拿不到真实路径，不能替你去扫盘）
- 单个文件仍然可以直接拖进窗口播放，不写曲库

**页面**
- 顶栏 / 底栏 `position: fixed` 始终可见，正文与浮层用 `--header-h / --footer-h` 实时留位
- 底栏链接来自 `site-links` 单一数据源（`python3 site-links/sync.py` 生成，不手改 `web/footer.js`）
- 默认英文、可切中文（设置 → 语言），也支持 `?lang=` 链接覆盖，见下面「语言（v1.5）」
- 移动端适配：390px 宽度下顶栏分两行、播放条自动换行

## 语言（v1.5）

界面**默认英文**，中文作为可选语言保留，同一份前端、不需要单独构建。

- **切换**：设置 → 语言 → `English` / `简体中文`。选择存在 `localStorage`，同时写进服务端配置（`language`），换浏览器/换设备也跟着走。
- **链接覆盖**：`?lang=en` / `?lang=zh` 优先于本地选择，方便把中文或英文的入口链接发给别人。
- **配置字段**：`language` = `en`（默认）或 `zh-CN`；`POST /api/config {"language":"zh-CN"}` 可改服务端默认。
- **实现**：`web/i18n.js` 是一张以「中文原文」为 key 的短语表（338 条），英文时 `T()` 按最长短语替换；`index.html` 的静态文案在启动时由 `MPI.localizeDOM()` 统一翻译一遍。**内容不翻译**：歌名、歌手、歌词、路径原样保留，只有界面文案走词表。
- **服务器消息**：接口错误在传输上仍是中文，前端用同一张短语表转成英文，所以 toast 和空状态也是英文。

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

## 支持的环境

### 运行环境

| 形态 | 平台 | 要求 |
| --- | --- | --- |
| 服务端 / 网页版 | macOS（Intel / Apple Silicon）、Linux amd64 / arm64、Windows amd64 | **单文件、零 CGO**，不需要 Go 运行时、不需要数据库；解压即跑 |
| 桌面版 | macOS 通用包（`MPlay.app`）、Linux x64、Windows x64 | 「打开本地文件夹」用系统原生对话框：macOS `osascript`、Linux `zenity` 或 `kdialog`、Windows PowerShell；没有图形会话时前端自动退回浏览器目录选择。另需一个 `urlbox` 窗口外壳：Linux 需要系统 `webkit2gtk`（wry 依赖，Debian/Ubuntu 装 `libwebkit2gtk-4.1-0`），Windows 需要 WebView2 运行时（Win10/11 一般自带）；macOS 为 ad-hoc 签名，首次打开需右键 →「打开」 |
| 浏览器访问 | Chrome / Edge / Firefox / Safari 的现代版本 | 播放依赖内核解码：`.mkv/.avi/.wmv` 多数内核放不了，Web 模式请优先 `.mp4/.webm`；桌面版可用「用系统播放器打开」兜底 |
| 移动端 | 手机浏览器访问同一地址 | 390px 宽度下顶栏分两行、播放条自动换行；`-lan` 或反向代理暴露后用 `MPLAY_TOKEN` 保护 |

### 构建环境（自己编译时）

- **Go 1.26+**（`go.mod` 已声明 `go 1.26.0`），`CGO_ENABLED=0` 交叉编译；`./scripts/build.sh` 一条命令出当前平台二进制。
- 打包脚本：`bash` + `zip`/`tar`；桌面版还要 **Rust 工具链**（`cargo build --release` 编 `urlbox`）。
- 前端零构建：`web/` 是原生 HTML/CSS/JS，靠 `go:embed` 打进二进制；CI 只跑 `node --check` 做语法校验（Node 20+）。
- 维护页脚需要 `python3`（`site-links/sync.py`）。

### 网络与隐私

- 歌词匹配需要访问 `lrclib.net`、`songsearch.kugou.com` / `krcs.kugou.com` / `lyrics.kugou.com`；都走系统代理（`HTTP(S)_PROXY` 环境变量生效）。**完全断网也能用**：同名歌词、内嵌歌词、已缓存歌词、已判定结论都在本地。
- 不上传音频：默认只有歌名/歌手/时长/文件名离开本机；`aiUploadAudio` 打开后，仅在你手动点单曲识别时上传前 6 MB 片段。
- 数据只落在本机：配置 `~/.config/mplay/config.json`，缓存 `~/.cache/mplay`（`lyrics/`、`subtitles/`、`covers/`、`library.json`），没有云端账号、没有遥测。

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

歌词相关配置项：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `enableLRCLIB` | true | LRCLIB 源开关（设置页「歌词与字幕」） |
| `enableKugou` | true | 酷狗源开关（华语网络歌手/翻唱覆盖最好） |
| `enableNeteaseSource` | false | 网易云源开关（本机 IP 易被 405 限流，默认关） |
| `language` | `en` | 界面语言：`en` 或 `zh-CN` |
| `autoCacheLyrics` | true | 扫描后自动后台补歌词 |
| `writeSidecar` | false | 命中后是否把歌词写成同名 `.lrc`（默认只写缓存，不动你的目录） |

## HTTP API（前端就是它的第一个客户）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/health`、`/api/version` | 健康检查、版本 |
| GET/POST | `/api/config` | 配置读写（密钥只回布尔） |
| GET | `/api/library?q=&kind=&filter=&sort=&limit=` | 曲库，`filter=lyric|nolyric|pending|instrumental`，`kind=audio|video`；条目里的 `noLyric` 表示「确认没有歌词（纯音乐）」 |
| POST | `/api/library/scan` | 重新扫描 |
| POST | `/api/track/{id}/identify` | 单曲识别歌名（body `apply/audio/fetchLyrics`） |
| POST | `/api/library/identify` | 批量识别未匹配曲目（body `limit/onlyMissing/apply`） |
| POST | `/api/ai/check` | 探测 AI 端点是否可用 |
| GET | `/api/track/{id}`、`/api/stats` | 单曲详情、统计（`total/audio/video/audioWithLyric/withLyric/noLyric/pending/bytes`） |
| GET | `/api/lyric/{id}?force=1&pick=` | 取歌词（按优先级自动解析）；纯音乐返回 `{doc:{instrumental:true,note:"无歌词，纯音乐",cues:[]}}` |
| POST | `/api/lyric/{id}?ext=.lrc` | 原始文本导入（body 为纯文本） |
| GET | `/api/lyric/{id}/candidates` | LRCLIB 候选列表 |
| GET | `/api/remote/lyric?track=&artist=&album=&duration=&pick=` | 在线歌词（不依赖本地曲目） |
| GET | `/api/search?q=&limit=` | 在线搜歌 |
| GET | `/api/search/lyrics?q=` | 歌词片段反查（local / remote / library） |
| GET | `/api/sources` | 当前音源及启用状态 |
| POST | `/api/pick/folder` | 弹系统目录选择框，返回 `{ok,path}`（或 `{ok:false,cancelled:true}`）；只对本机访问开放 |
| POST | `/api/open` | `{"id":..,"action":"reveal"|"player"}`（仅桌面） |
| GET | `/media/{id}`、`/cover/{id}` | 媒体流（Range）、封面 |

设置 `authToken` 后，`/api`、`/media`、`/cover` 都要求 `X-Mplay-Token` 头、`?token=` 或 `mplay_token` Cookie。

**没设令牌时（v1.4.1 起）**：只有「本机直连」放行，经反向代理或公网进来的请求一律 `403`。判据是请求来源而不是监听地址：对端必须是回环、且没有任何转发头（`X-Forwarded-For`/`X-Real-IP`/`Forwarded`…）、且浏览器没标 `Sec-Fetch-Site: cross-site`。原因很实在——监听 `127.0.0.1` 再挂 HTTPS 反代对外时，`RemoteAddr` 永远是 `127.0.0.1`，只按监听地址判断等于把公网当本机。详见下面「安全模型」。

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

## 歌词源与多候选匹配（v1.3）

歌词匹配分三层：**文件名/标签清洗 → 多候选裁决 → 多歌词源兜底**。原则没变——宁可这首没词，也不给错词。

### 三个歌词源（按顺序、可单独关闭）

| 源 | 默认 | 说明 |
| --- | --- | --- |
| LRCLIB | 开 | 全球社区库，欧美 / 主流华语覆盖最好，带时间轴；`/api/search` 查、`/api/get` 兜精确匹配 |
| 酷狗 | 开 | 华语网络歌手、翻唱、民乐最全，取歌词走「搜索 → 歌词候选 → 下载 base64 LRC」；频控 900ms |
| 网易云 | **关** | 接口能用但按 IP 限流，实测密集请求后持续 405「操作频繁」，冷却也不恢复，所以默认关闭、代码保留（`enableNeteaseSource`） |

同名借词兜底排在最后：只用歌名搜，要求 `歌名 ≥0.92`，且（`艺人 ≥0.45` 或 时长差 ≤3s）才借，避免「无问」民乐被配成毛不易的词。

### 怎么用

- 设置页 →「歌词与字幕」卡片：`enableLRCLIB`、`enableKugou`、`enableNeteaseSource` 三个开关，保存即生效，同时同步到后台缓存 worker。
- 配置文件直改：`~/.config/mplay/config.json` 里同名三个布尔字段。
- 接口：`GET /api/config` 读当前状态，`POST /api/config` 传 `{"enableKugou":false}` 之类做局部更新；`GET /api/sources` 可看当前音源状态。

### 匹配细节（都是真实曲库上踩出来的）

1. **清理**（`internal/media/guess.go`）：去扩展名、站点前缀、括号噪音（码率 / 格式 / 清晰度 / `live` / `remix` / 完整版 / 伴奏 / 倍音质 / 母带 / 杜比 / 试音 / 发烧 / DJ版 / 抖音 / 快手 / 站点名…），括号外裸噪音 token 也剥；`01.` 序号支持 1~4 位、`037刘至佳`（序号粘汉字）与 `067LBI利比`（序号+字母+汉字）都拆得动，`9420-麦小兜` 的 9420 保留；裸 `-` 只在「有一侧是中日韩文字，或两侧都是多词」时拆（`All Night-少女时代` 拆、`Taylor-Swift` 不拆）。
2. **标签不可全信**：资源站批量转码常把标签写脏（`Love Story-Taylor` / `Swift`、`kuwo` / `kuwo`、`8.潇湘水云`、`2、相思（live）`、`artist=小高网分享xgw5.com`）。现在标签值先过站点词过滤 + `CleanName` 清洗，然后**只作第一顺位候选**，文件名候选继续排在后面一起让歌词库裁决。
3. **版本后缀不算另一首歌**：比对前用 `coreTitle` 剥掉 `(Live)`/`(DJ版)`/`(女版)`/`(古琴名曲)`/`(翻奏)` 这类括号附件，`做你的爱人` 与 `做你的爱人 (DJ 慢摇版)` 才不会被当成两首，再由时长把错版本挡掉（实测选到 285s 原版，而不是 194s 慢摇版）。
4. **艺人写法归一**：`normArtistToken` 抹掉括号和 `003` 这类序号前缀，并把 `& / , 、 ; | x vs feat ft with -` 全当分隔符取最高分——`003L（桃籽）` 能与酷狗的 `L（桃籽）、周林枫、三楠` 对上。
5. **双门槛 + 反序歧义**：命中要过 `歌名 ≥0.75`、`艺人 ≥0.55`；候选里同时存在 A/B 与 B/A 时，**非首选朝向且只有单条记录支持的不认**（社区里只有一个上传，不足以证明谁是谁）；反序脏条目直接丢弃。
6. **省请求**：候选上限 4 条、批内 3 并发、全局并发 6、LRCLIB 600ms 节流、单请求超时 15s；搜到 1 条才补一次关键词搜索，0 条不补。
7. **回填**：确认过的写法只写进曲库索引（`metaSource=lrclib`），**不动音频和标签**。

### 实测（584 首真实曲库）

| 口径 | 结果 |
| --- | --- |
| 有歌词 | 498 / 584（85.3%） |
| 纯音乐（标记为无歌词） | 48 条 |
| 剔除纯音乐/音效/超短片段后 | 498 / 511（97.5%） |
| 表面命中（把纯音乐占位也算上，v1.3 前的口径） | 546 / 584（93.5%） |

遗留的 38 条：纯音乐/民乐/音效 19 条、超短或无标签片段 6 条、两库都确实没有的翻唱 13 条。

早期用 28 条极脏文件名单独测过一轮：**27/28 命中规范记录**（歌名相似度全 1.00），原先 4 条「命中但命中错条目」全部纠正；唯一未命中是 LRCLIB 限流所致，单曲复测可命中。

## 纯音乐：标记，不是硬配

歌词库对乐器曲常常返回一段“歌词”，内容其实是 `纯音乐，请欣赏`。这类占位文本以前会被当成命中，界面上出现一行假歌词、统计也虚高。

现在的处理：

- **识别**：先剥掉 LRC 时间标签与 `[ti:]/[total:]/[qq:]` 元信息，再判断是否命中 `纯音乐+欣赏`、`纯音乐`、`暂无歌词`/`无歌词` 等固定说法；真歌词里出现“纯音乐”三个字不会误判（有测试用例锁）。
- **落盘**：`<缓存>/lyrics/<id>.nolyric.json`（`reason=instrumental`、`note=无歌词，纯音乐`），并删掉对应的假歌词缓存。
- **曲库**：条目多出 `noLyric` 字段（与 `lyricSource` 互斥，**不算有歌词**），列表角标显示「无歌词，纯音乐」，详情页直接说明而不是「没找到歌词」。
- **不再反复问**：待缓存列表跳过这类条目——顺带解决了以前「永久 miss 每轮无限重跑」的问题。
- **老数据自动迁移**：启动时扫一遍缓存，把已经存下来的占位歌词转成结论（实测一次修正 48 条）。

想推翻某条结论：删掉 `<缓存>/lyrics/<id>.nolyric.json`，再用 `GET /api/lyric/{id}?force=1` 强制重新匹配。



## 曲库四个数字与播放器界面（v1.4）

| 数字 | 口径 | 点它就是 |
| --- | --- | --- |
| 全部 | 曲库里的音视频总数 | 不过滤 |
| 音频 | `kind=audio` 的条数 | `kind=audio` |
| 有歌词 | 音频里有歌词的条数（纯音乐结论不算有词） | `filter=lyric` |
| 待配歌词 | 音频里既没歌词、也还没判成纯音乐的条数 | `filter=pending` |

`GET /api/stats` 与 `/api/library` 返回的 `stats` 里是这些字段：`total / audio / video / audioWithLyric / withLyric / noLyric / pending / bytes`。其中 `withLyric` 是「含视频字幕」的总数，`audioWithLyric` 只算音频，界面上的「有歌词」用的是后者。
工具条还有两个补充筛选：`纯音乐` = `filter=instrumental`（有 `noLyric` 结论的），`全部缺词` = `filter=nolyric`（没有 `lyricSource` 的，包含纯音乐）。

**播放器**

- **音量常驻**：滑杆和进度条在同一行，右边依次是静音按钮与百分比数字，`M` 静音、`↑/↓` 每次 5%，双击滑杆回 80%。窄到 390px 也不隐藏，只把滑杆收窄到 88px、隐藏百分比文字。
- **当前歌词条**：进度条上方固定一行，显示当前唱到的歌词与译文；前奏/间奏显示 `♪ 前奏 / 间奏…`，纯音乐显示结论，没歌词显示「没有歌词」。切到曲库页时，迷你播放条也用这句代替歌手名。
- **最小化（迷你条）**：离开播放页时浮在底部，可调音量、上一首/播放/下一首，带 12 根竖条的小频谱（与播放页共用同一份频谱数据，不额外分析）；从迷你条切歌不会跳回播放页。窄到 390px 仍保留全套按钮，只把频谱隐掉、滑杆收窄。
- **音频可视化**：播放区与播放条之间的一条频谱带（48 根竖条，低频占更多格子、峰值平滑衰减），播放时封面/字号/背景光轻微呼吸。真实频谱来自 WebAudio `AnalyserNode`；为了不牺牲播放兼容性，**不把主播放元素绑进 WebAudio**（跨域音源绑进去会被静音），而是用一只隐藏 `<audio>` 拉同一路流单独分析、输出增益 0（听不到第二次声音）。所以本地曲库是真实频谱，跨域在线音源 / 不支持 WebAudio / 连续 5 秒拿不到信号时自动退回装饰动画（节奏仍跟播放/暂停走）。视频播放时这条带子隐藏。

## 后台自动缓存歌词

扫描完不再需要你一首首点开播放器才会拉歌词：

- 曲库扫描成功 → 自动开始后台缓存（`Library.OnScanned` 挂钩）；服务启动时也会先跑一轮；设置里改了开关同样立即触发。
- **异步、限并发**：独立 worker（4 并发 + LRCLIB 600ms / 酷狗 900ms 节流、429/503 退避重试），单曲超时 120s，走的是和播放同一条解析链路，不占播放请求、不影响听歌。
- **只补缺的**：仅处理音频条目里「没有歌词、没有内嵌字幕、缓存未命中、也没判定过是纯音乐」的，其余跳过。
- **边缓边显示**：每首命中立刻回填曲库（歌词角标 + 时长/歌名纠正），曲库工具条角标是**圆环 + 百分比 + `3/12`**，鼠标悬停看命中/未找到/跳过的完整口径；跑完显示绿色环与命中率，4 秒后收起。
- **失败会重试，但不是无限重试**：限流或网络失败留在待办里，下一轮（15 秒一次心跳）自动接着做；歌词库确认「纯音乐/无歌词」的直接出结论并移出待办。

| 配置 / 接口 | 说明 |
| --- | --- |
| `autoCacheLyrics` | 默认 **true**。设置页 →「歌词自动缓存」开关 |
| `enableKugou` / `enableNeteaseSource` | 酷狗默认 **true**、网易云默认 **false**；设置页 →「歌词与字幕」里两个开关 |
| `GET /api/cache/status` | `{enabled,lrclib,kugou,pending,audio,withLyric,progress:{running,total,done,hit,miss,skipped,...}}`；`skipped` = 已判定纯音乐/无歌词 |
| `POST /api/cache/run` | 立即缓存缺歌词的曲目；已在跑返回 `{already:true,pending}`，无需可缓则返回 `hint` |

## 安全模型（v1.4.1）

被人当面问过一句：「从互联网打开 m.51pwn.com，会不会读到我这台 Mac 上的非音频文件？」就按这个问题把边界重新收了一遍，结论和措施都写在下面，能复现的都留了验证方法。

**先说结论：读不到任意文件，但有几处真实可利用的越权面，已全部堵上。**

- **目录穿透：不存在。** `/media/{id}`、`/cover/{id}`、`/api/lyric/{id}` 的 `{id}` 都是扫描时算出的曲目哈希，先用 `Lib.Get(id)` 命中索引条目，再 `os.Open(entry.Path)`；请求里的字符串从不拼进文件路径。静态资源走 `go:embed` + `path.Clean` + 拒绝 `..`。实测 `--path-as-is` 打 `/media/../../../../etc/passwd` 之类：`mux` 先 307 归一到 `/etc/passwd`，落到静态处理器后 404，**没有任何文件内容返回**。歌词上传的 `ext` 参数只参与解析，落盘固定是 `<cache>/lyrics/<id>.json`。
- **扫描只认音视频扩展名。** `WalkDir` 里只有 `audioExts`/`videoExts` 会进索引（`.mp3 .flac .m4a … .mp4 .mkv …`），其余文件连条目都不产生，所以「曲库里多出一条」这条路也读不到 `.txt`/`.json`/密钥文件。同名 sidecar 只读 `.lrc/.srt/.vtt/.ass` 这类字幕歌词文件。
- **真正的风险是「改配置」而不是「穿透」。** 老版本没令牌 + 反代的情况下，任何人都能 `POST /api/config` 把 `mediaDirs` 改到 `/Users/<你>` 甚至 `/`，触发重扫后整盘音视频入库、再逐个 `/media/{id}` 下载（可能含私人录音、家庭视频）；`/api/library`、`/api/track`、`/api/config` 还会顺手交出绝对路径、配置路径、曲库清单。现在：公网/反代默认 403，非本机直连的响应里 `path`/`dir`/`configPath` 一律清空，`mediaDirs` 只回目录名。
- **SSRF 已堵。** 自定义音源的 `searchUrl` 与 AI 端点都是「服务端主动出站」，旧版可以指向 `127.0.0.1` 上任意端口，错误串还会带响应体前 256 字节（等于一个读取原语，实测 `ssrf: HTTP 404 404 page not found` 就是内网接口的原文）。现在写入配置时校验、请求前再校验一次：只允许 `http/https`，解析出的地址落在内网/环回/链路本地/`169.254.169.254` 就拒绝。
- **GUI 动作按请求来源判定。** 「打开文件夹」弹窗、「用系统播放器打开」以前只判 `Cfg.Addr` 是不是回环——反代场景下等于对全世界开放。现在必须请求本身来自本机直连。
- **CSRF。** 写方法要求 `Origin` 与本站一致；第三方网页借浏览器 POST 本机接口会被 403。
- **健康检查不再泄漏路径。** `/api/health` 对外开放（监控用），原来返回 `roots` 的绝对路径，现在只返回数量。

还有两条**仍然建议照做**的运维习惯：公网部署一定设 `authToken`（`MPLAY_TOKEN` 或设置页）；令牌是 URL 里的 `?token=` 时，注意别把带令牌的链接贴到聊天里（虽然页面已设 `Referrer-Policy: no-referrer`，上游 nginx 的 access log 仍会记下完整 URL）。

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
  - `internal/lyrics` LRC/SRT/VTT/ASS 解析、编码、LRCLIB / 酷狗 / 网易云三源、纯音乐结论、字幕库、片段索引
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
- 歌词覆盖面受歌词库本身限制：584 首真实曲库里仍有 38 首没词，其中 19 首是纯音乐/民乐/音效、6 首是超短或无标签片段、13 首两库都没有对应版本（多为翻唱/网络歌手）。网易云源默认关闭，需要时可在设置里打开（可能被 IP 限流）。

## 已验证

- 真实曲库实测（584 首来自资源站的脏文件，标签与文件名都不干净）：有歌词 **498/584（85.3%）**，纯音乐占位 **48 条**全部标成「无歌词，纯音乐」，剔除纯音乐/音效/超短片段后 **498/511（97.5%）**；抽检 `做你的爱人`（285s 原版）、`相思`（红豆生南国…）、`晚风作酒` 内容均与原曲一致，未出现乱配
- 纯音乐判定与迁移：启动实测一次性修正 48 条历史占位缓存；`GET /api/lyric` 对纯音乐 0.03s 返回结论、不再打网络；待缓存数量从 48 降到 38，不再无限重跑
- 曲库：2 音频 + 1 视频，歌词 3 条，`cueCount` 正确
- 晴天（同名词幕）8 行同步；Blank Space（LRCLIB）82 行；视频 SRT 双语（中文主行 + 英文译文）
- Range：`206 Partial Content` + 正确 `Content-Range`
- 歌词片段 `故事的小黄花` → 本地命中「晴天 @0.5s」，网易云返回 10 条并带命中歌词行
- 播放器页面：歌词逐行高亮随进度滚动、视频字幕压在画面上、顶栏/底栏始终可见
- 歌名识别（假 AI 端点 + 真 LRCLIB 实测）：`abc1234.mp3` 经 AI 给出「夜空中最亮的星 - 逃跑计划」，再被 LRCLIB 校验为 0.99 写回并拉到 33 行同步歌词；AI 只说「测试歌曲/测试歌手」(0.55) 时单曲识别返回 `apply:false`、批量只给建议不落库；未配置 AI 时功能退化为纯本地候选，不报错
- 设置页：填 base/model/key → 保存 → 「测试连通」回显模型名，`/api/config` 只回 `aiConfigured` 布尔

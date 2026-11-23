## v1.1.0 · 智能识别歌名

资源站下来的文件常常叫 `abc1234.mp3`、`01. 某站 - 歌手 - 歌名 [320k].mp3`，标签也丢了。这个版本加了三级识别，越往后越贵，所以按顺序短路：

1. **本地候选**：内嵌标签 → 文件名（正序 / 反序 / 去括号）→ 已有歌词第一句，零成本；
2. **歌词库校验**：候选拿去 LRCLIB 打分（歌名 0.62 / 歌手 0.23 / 时长 0.15），≥0.62 算命中，直接把确认过的歌名 / 歌手 / 专辑写回曲库，顺手拉同步歌词；
3. **可选 AI 兜底**：只有前两步没把握才问模型（OpenAI 兼容端点，豆包方舟 / OpenAI / 自建都行）。模型答案还要再过一遍歌词库校验——批量识别只写「校验通过」的，单曲识别要求校验通过或模型置信 ≥0.75，否则只提示建议、不覆盖原文件线索。

- 界面：曲库「识别未匹配」（批量，单次 20 条）、播放器歌词条「识别」（单曲）、设置页「AI 元数据识别（可选）」+「测试连通」；
- 隐私：默认不上传音频；打开 `aiUploadAudio` 后只在手动单曲识别时发前 6 MB 片段，批量永远只发文本；不配 AI 时整套功能退化为本地规则 + 歌词库，不报错；
- 不动你的文件：只改曲库索引（`metaSource=ai`），原音频 / 标签一律不写。

## 下载与使用

### 桌面版

每个桌面包都**解压即用**，不需要装 Go / Rust / Node：

| 平台 | 文件 | 怎么启动 |
| --- | --- | --- |
| macOS（Intel + Apple Silicon 通用） | `mplay-desktop-*-macos-universal.zip` | 解压后双击 `MPlay.app`；首次打开若被 Gatekeeper 拦，右键 →「打开」 |
| Linux x64 | `mplay-desktop-*-linux-x64.tar.gz` | `./start-mplay.sh` |
| Windows x64 | `mplay-desktop-*-windows-x64.zip` | 双击 `start-mplay.cmd` |

包里是 `mplay`（Go 后端 + 内嵌网页界面）和 `urlbox`（Rust + wry 的窗口外壳）。
`mplay-desktop-*-cli.tar.gz` 是同一套东西的裸目录版（不用 .app 时用）。

`mplay-<版本>-<os>-<arch>.tar.gz / .zip` 是**服务端 / 网页版**单文件二进制：

```bash
./mplay -addr :8737 -media /srv/media     # 打开 http://127.0.0.1:8737
```

同一份页面可以部署到 m.51pwn.com（`packaging/nginx-m.51pwn.com.conf`、`packaging/mplay.service`、`packaging/Dockerfile`）。

### 第一次使用

1. 启动后窗口里点「设置」→ 添加媒体目录（音乐 / 视频文件夹），会自动扫描；
2. 同名 `.lrc/.srt/.vtt/.ass` 自动识别，没有的按歌名去 LRCLIB 匹配，视频还会叠字幕到画面上；
3. 也可以把音频 / 视频文件直接拖进窗口播放。

### 已知限制

- 浏览器内核放不了 `.mkv/.avi` 时，桌面版可用「用系统播放器打开」兜底；
- 在线可播放源只有 Apple 30 秒官方试听与公有领域 / CC 音频（Internet Archive、Jamendo、自定义音源），不内置第三方平台的音频地址。

校验：同目录的 `SHA256SUMS`。

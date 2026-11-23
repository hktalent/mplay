## mplay 桌面版

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

# musicli — TUI 命令行音乐播放器设计文档

**日期**: 2026-07-05
**状态**: 设计待评审

## 1. 概述

用 Go + Bubble Tea v2 实现的 TUI 音乐播放器。模块化、低耦合、易扩展。支持多格式音频、逐字歌词（SPL 优先）、专辑封面终端显示、频谱可视化、自定义主题、快捷键、歌词自动爬取。配置用 TOML，四级日志审计，git 版本管理（每功能一次提交）。不使用 emoji，可用 MD3 unicode 图标。适配深/浅色并跟随系统，终端大小自适应，键盘+鼠标操作。

## 2. 技术栈

| 关注点 | 选型 | 理由 |
|---|---|---|
| TUI 框架 | `charm.land/bubbletea/v2` + `bubbles/v2` + `lipgloss/v2` | v2 稳定，原生鼠标+帧率渲染，Go 1.25+（我们有 1.26.4） |
| 音频输出 | `github.com/ebitengine/oto/v3` | 吃 ffmpeg PCM 流，低层稳定 |
| 解码+倍速 | ffmpeg/ffprobe 管道 + atempo 滤镜 | 格式最广（含 M4A/AAC/ALAC），倍速不变调 |
| Tag 读取 | `github.com/dhowden/tag` | 覆盖 MP3/MP4/FLAC/OGG/Opus + 封面 |
| 终端图片 | `github.com/blacktop/go-termimg` + `disintegration/imaging` 缩放 | kitty/sixel/iTerm2 自动检测 + half-block 回退 |
| FFT 频谱 | `github.com/cwbudde/algo-fft` + `algo-dsp` | SIMD 零分配 |
| 主题检测 | `godbus/dbus/v5` → gsettings → `muesli/termenv` OSC11 | 三级覆盖 |
| 配置 | `github.com/pelletier/go-toml/v2` | TOML 解析 |
| 日志 | 标准库 `log/slog` | 结构化日志，4 级（debug/info/warning/error） |

## 3. 架构

### 3.1 目录结构

```
musicli/
  cmd/musicli/main.go          # 入口
  internal/
    config/                    # TOML 配置加载、默认值、XDG 路径
      config.go
      defaults.go
      paths.go                 # XDG 路径解析
      keybindings.go           # 快捷键配置模型
      theme.go                 # 主题配置模型
    log/                       # 日志审计系统
      logger.go                # slog 封装，4 级，文件+终端
    audio/                     # 音频引擎
      engine.go                # 播放控制接口
      ffmpeg.go                # ffmpeg 管道后端（解码+atempo+PCM）
      oto.go                   # oto 输出
      spectrum.go              # PCM tee → FFT 环形缓冲
    library/                   # 媒体库
      track.go                 # Track 数据模型
      scanner.go               # 文件/文件夹扫描
      tag.go                   # dhowden/tag 封装
      album.go                 # 专辑分组
      sort.go                  # 排序（名字/大小/年份，正序倒序）
    playlist/                  # 歌单
      playlist.go              # 歌单模型、增删、排序
    lyrics/                    # 歌词系统
      model.go                 # Line/Word/Lyric 归一化模型
      parser.go                # 解析器接口
      spl.go                   # SPL 解析器（首要，含普通 LRC）
      lrc.go                   # 普通 LRC（SPL 子集）
      yrc.go                   # YRC（暂缓）
      qrc.go                   # QRC + 解密（暂缓）
      krc.go                   # KRC + 解密（暂缓）
      fetcher.go               # 歌词爬取接口
      netease.go               # 网易云 API
      qq.go                    # QQ 音乐 API
      kugou.go                 # 酷狗 API
      matcher.go               # 本地歌曲→在线匹配
    cover/                     # 专辑封面
      image.go                 # 从 tag/文件提取封面
      render.go                # 终端图片渲染（协议检测+缩放）
      protocol_kitty.go        # kitty 图形协议
      protocol_sixel.go        # sixel
      protocol_iterm.go        # iTerm2
      protocol_halfblock.go    # ASCII 块回退
    theme/                     # 主题系统
      theme.go                 # 主题模型、深/浅色
      detect.go                # 系统主题检测+监听
      palette.go               # 调色板
    ui/                        # TUI 层
      app.go                   # 顶层 model（bubbletea）
      layout.go                # 响应式布局
      views/                   # 各视图
        library.go             # 媒体库列表
        nowplaying.go          # 正在播放（封面+歌词）
        playlist.go            # 歌单管理
        spectrum.go            # 频谱可视化
      widgets/                 # 可复用组件
        playerbar.go           # 底部播放栏（进度+控制）
        lyricsview.go          # 歌词逐字高亮
        coverview.go           # 封面显示
        list.go                # 通用列表（封装 bubbles/list）
      keys.go                  # 快捷键绑定
      mouse.go                 # 鼠标处理
      styles.go                # lipgloss 样式（绑定主题）
  config/                      # 默认配置模板
    config.example.toml
  docs/
  go.mod
  Makefile
```

### 3.2 模块依赖（低耦合）

```
config ← log ← (所有模块依赖这两个)
audio ← ui
library ← ui
playlist ← ui
lyrics ← ui
cover ← ui
theme ← ui
```

模块间通过接口通信，不直接依赖具体实现。例如 `audio.Engine` 是接口，`ffmpeg` 是实现；`lyrics.Parser` 是接口，`spl` 是实现。这样便于扩展（加新格式/新源/新后端只加实现，不改接口）。

### 3.3 数据流

```
用户操作 → ui (bubbletea Update) → 命令
  → audio.Engine (play/seek/speed/volume)
    → ffmpeg 子进程 → PCM 流
      ├→ oto 播放
      └→ spectrum 环形缓冲 → FFT → 频谱数据
  → library.Scanner (异步扫描) → tracks
  → lyrics.Fetcher (异步爬取) → Lyric
  → cover.Render (异步渲染) → 终端图片
  → theme.Detect (系统监听) → 主题切换

tick (30fps) → 更新进度/频谱/歌词高亮 → View 重绘
```

## 4. 核心模块设计

### 4.1 配置 (config/)

- 路径：`~/.config/musicli/config.toml`（XDG）
- 默认值：`config/defaults.go` 提供完整默认配置，缺失字段自动补默认
- 热重载：监听配置文件变化（可选，v1 先不做）

```toml
[audio]
volume = 80          # 0-100
speed = 1.0          # 0.5-2.0

[playback]
repeat = "list"      # none|one|list
shuffle = false

[library]
sort_field = "title" # title|artist|album|size|year
sort_order = "asc"   # asc|desc
group_by_album = true

[lyrics]
auto_fetch = true
sources = ["qq", "netease", "kugou"]
fetch_priority = "qq"  # qq|netease|kugou
save_dir = "~/.cache/musicli/lyrics"

[cover]
show = true
protocol = "auto"    # auto|kitty|sixel|iterm|halfblock

[theme]
mode = "auto"        # auto|dark|light
name = "default"     # 主题名

[keybindings]
# 见 4.8

[log]
level = "info"       # debug|info|warning|error
file = "~/.local/state/musicli/musicli.log"
```

### 4.2 日志审计 (log/)

- 4 级：debug/info/warning/error（对应 slog 的 Debug/Info/Warn/Error）
- 输出：文件（`~/.local/state/musicli/musicli.log`）+ 可选终端（debug 模式）
- 结构化：JSON 或文本，带时间戳、级别、模块名、消息、上下文字段
- **错误必须带上下文**：文件路径、错误链（`fmt.Errorf("%w", err)` 或 slog 的 With）、可定位位置
- 轮转：按大小轮转（默认 10MB × 5 份），避免无限增长

```go
// log/logger.go 核心接口
type Logger struct { *slog.Logger; file *os.File }
func New(level string, path string) (*Logger, error)
func (l *Logger) WithModule(name string) *Logger  // 子 logger，带 module 字段
// 用法：log.WithModule("audio").Error("ffmpeg 启动失败", "path", p, "err", err)
```

### 4.3 音频引擎 (audio/)

接口优先，ffmpeg 是实现：

```go
type Engine interface {
    Play(path string) error
    Pause() error
    Resume() error
    Seek(positionMs int) error
    SetVolume(v int) error        // 0-100
    SetSpeed(s float64) error     // 0.5-2.0
    Position() int                // 当前位置 ms
    Duration() int                // 总时长 ms
    State() State                 // Playing|Paused|Stopped
    SpectrumData() []float64      // 频谱数据（ tapped from PCM）
    OnUpdate(cb func(Update))     // 播放进度回调
}
```

ffmpeg 后端：
- 启动：`ffmpeg -ss <seek> -i <path> -filter:a "atempo=<speed>" -f s16le -ar 48000 -ac 2 pipe:1`
- 倍速 >2 链式：`atempo=2.0,atempo=1.5`
- seek：终止当前进程，用新 `-ss` 重启（流式管道 seek 困难，重起最稳）
- PCM tee：goroutine 读 ffmpeg stdout，一份喂 oto，一份喂 FFT 环形缓冲
- 时长：ffprobe 预先获取（`-show_entries format=duration`）
- 错误处理：ffmpeg 启动失败/非零退出码记 error 日志（带 path、stderr 内容）

### 4.4 媒体库 (library/)

```go
type Track struct {
    Path      string
    Title     string
    Artist    string
    AlbumArtist string
    Album     string
    Composer  string
    Genre     string
    Year      int
    TrackNo   int
    DiscNo    int
    Duration  int    // ms
    Size      int64  // bytes
    HasCover  bool
}
```

- 扫描：递归遍历目录，按扩展名过滤（mp3/flac/ogg/wav/m4a/aac/opus/aiff），异步扫描（bubbletea command），tag 读取失败记 warning 但不中断
- 专辑分组：按 Album + AlbumArtist 聚合
- 排序：字段（title/artist/album/size/year）× 方向（asc/desc），`sort.Slice` + 字段访问器

### 4.5 歌单 (playlist/)

```go
type Playlist struct {
    Name    string
    Tracks  []*library.Track
}
type Manager struct {
    playlists map[string]*Playlist
    current   string  // 当前歌单名
}
```
- 添加歌单、删除歌单、加入当前歌单、对当前歌单排序（复用 library/sort 逻辑）

### 4.6 歌词 (lyrics/)

归一化模型：
```go
type Line struct {
    StartMs     int
    EndMs       int
    Words       []Word
    Translation string
    Agent       string
    IsBackground bool
}
type Word struct {
    Text    string
    StartMs int
    EndMs   int
}
type Lyric struct {
    Lines []Line
    Tags  map[string]string
}
```

Parser 接口：
```go
type Parser interface {
    Parse(text string) (*Lyric, error)
    Format() string  // "spl"|"lrc"|"yrc"|...
}
```

**SPL 解析器（首要）**：tokenizer 扫描时间戳+文本交替，首 ts=行开始，文本在 ts[i]~ts[i+1] 为 Word，末 ts 无文本=行结束，翻译检测（共享时间戳/无时间戳紧随），重复行展开。普通 LRC 是 SPL 子集（无行内时间戳）。约 80 行。

本地歌词加载：按音频文件路径查找同名 `.lrc`/`.spl`，找到则解析；找不到且开启 auto_fetch 则爬取。

爬取（lyrics/fetcher.go + 各源）：
- 接口 `Fetcher interface { Search(query) ([]Result, error); FetchLyric(id) (*Lyric, error) }`
- 匹配：时长 ±4s 门控 + 标题/歌手/专辑文本相似度（LCS ratio），cutoff 55，差 15 内取最丰富
- 源：QQ（musicu.fcg，QRC 需 3DES+zlib 解密）、网易云（公开 API `yv=-1`，YRC 纯文本）、酷狗（lyrics.kugou.com，KRC 需 XOR+zlib）
- 错误处理：网络失败记 warning（带 query/source），解析失败记 error（带原始内容片段）

**暂缓格式**：YRC/QRC/KRC 解析器+解密、TTML/LYS/LRCv2/Lyrics File。留 parser 接口和文件，实现为 `return nil, ErrNotImplemented`。

### 4.7 专辑封面 (cover/)

```go
type Renderer interface {
    Render(img image.Image, w, h int) string  # 返回带转义的字符串
}
```
- 提取：优先 tag.Picture()，无则查同目录 cover.jpg/folder.png 等
- 协议检测：KITTY_WINDOW_ID / TERM / TERM_PROGRAM → kitty/sixel/iterm/halfblock
- 缩放：`disintegration/imaging` resize 到目标单元格像素尺寸（按终端字体像素估算）
- kitty：`\x1b_Ga=T,t=d,f=100,s=,v=,c=,r=,m=;<b64>\x1b\\` 分块
- halfblock：`▀▄` + ANSI 前后景色，回退方案
- 渲染策略：View() 占位空格 + 帧后 ANSI 光标定位写转义（goberzurg 做法）

### 4.8 主题 (theme/)

```go
type Theme struct {
    Name     string
    Mode     Mode  // Dark|Light
    Bg       lipgloss.Color
    Fg       lipgloss.Color
    Accent   lipgloss.Color
    Muted    lipgloss.Color
    // ... 调色板
}
```
- 检测：XDG portal `org.freedesktop.appearance.color-scheme`（godbus）→ gsettings → termenv OSC11
- 监听：dbus SettingChanged 信号 → goroutine → `p.Send(themeChangedMsg{})` → Update 重设样式
- 自定义：TOML 定义调色板，深/浅色各一套

### 4.9 TUI (ui/)

顶层 model 管理：
- 当前视图（library/nowplaying/playlist）
- 焦点窗格
- 全局状态（播放状态、进度、频谱数据）
- 子 model（各视图/组件）

响应式布局（WindowSizeMsg）：
- 宽 ≥100：三栏（侧栏列表 | 中间主视图 | 右侧歌词/封面）
- 宽 60-99：两栏（侧栏 | 主视图，歌词/封面切 tab）
- 宽 <60：单栏（列表/播放页/歌词 切 tab）
- 高度：底部固定播放栏 3 行，剩余主区

鼠标（v2 MouseMsg 接口）：
- 点击窗格切换焦点
- 点击列表项选中/播放
- 滚轮滚动列表
- 点击进度条 seek
- 点击播放栏按钮

快捷键（可配置，keybindings 包）：
```go
type KeyMap struct {
    PlayPause key.Binding
    Next      key.Binding
    Prev      key.Binding
    SeekFwd   key.Binding
    SeekBack  key.Binding
    VolUp     key.Binding
    VolDown   key.Binding
    SpeedUp   key.Binding
    SpeedDown key.Binding
    Shuffle   key.Binding
    Repeat    key.Binding
    // ... 视图切换、歌单操作
}
```
默认绑定可被 TOML `[keybindings]` 覆盖。

歌词逐字高亮（widgets/lyricsview.go）：
- 根据当前播放位置定位当前行
- 行内 Word 按时间戳高亮（已唱=accent，未唱=muted）
- 自动滚动（当前行居中）
- viewport 滚动

频谱（views/spectrum.go）：
- 30fps tick 读 SpectrumData()
- `▁▂▃▄▅▆▇█` 块字符或 lipgloss 渐变柱
- attack/decay 物理（参考 go-cli-beat）

## 5. 图标

使用 Material Design 3 unicode 图标（如 `▶ ⏸ ⏹ ⏮ ⏭ ♻ ★`），**不用 emoji**。在样式中作为文本渲染。

## 6. 错误处理与日志

- 所有外部交互（ffmpeg/文件IO/网络/tag/歌词解析）失败必须记日志
- 错误链：`fmt.Errorf("ffmpeg 启动失败: %w", err)`，slog With 附加上下文字段
- 关键路径：
  - ffmpeg：启动失败/非零退出 → error（path, stderr, exitcode）
  - tag 读取：失败 → warning（path, err），Track 仍入库（用文件名兜底）
  - 歌词解析：失败 → warning（file, format, snippet）
  - 歌词爬取：网络失败 → warning（query, source）；解析失败 → error（raw snippet）
  - 配置加载：字段非法 → warning（用默认值），文件缺失 → info（创建默认）
  - 主题检测：dbus 失败 → info（降级 gsettings）；gsettings 失败 → info（降级 OSC11）
- 用户可见错误：TUI 底栏短暂提示（如"歌词未找到"），详细见日志文件

## 7. 实现阶段

每阶段一次或多次 git 提交，提交信息遵循 Conventional Commits（`feat:`/`fix:`/`refactor:`/`docs:`/`chore:`）。

| 阶段 | 内容 | 提交 |
|---|---|---|
| 0 | 工程骨架：go mod、目录、日志、配置、Makefile | feat: scaffold project |
| 1 | 音频引擎（ffmpeg+oto，播放/暂停/seek/音量/倍速） | feat: audio engine |
| 2 | 媒体库（扫描、tag、专辑分组、排序） | feat: media library |
| 3 | 基础 TUI（布局、列表、播放栏、进度、键鼠） | feat: basic tui |
| 4 | SPL 歌词（解析+逐字高亮视图） | feat: spl lyrics |
| 5 | 专辑封面（kitty/sixel/halfblock） | feat: album cover |
| 6 | 排序与专辑视图 | feat: sorting and album view |
| 7 | 播放模式（随机/单曲/列表循环） | feat: playback modes |
| 8 | 歌单（增删/加入/排序） | feat: playlists |
| 9 | 频谱可视化 | feat: spectrum visualizer |
| 10 | 主题（TOML 自定义+深浅色+跟随系统） | feat: theming |
| 11 | 快捷键自定义 | feat: custom keybindings |
| 12 | 歌词自动爬取（QQ/网易云/酷狗） | feat: lyric crawling |
| 13 | 其他逐字格式（YRC/QRC/KRC，暂缓但留接口） | feat: more lyric formats |

## 8. 非目标（YAGNI）

- 音频均衡器
- 在线音乐流媒体播放（只爬歌词，不播放在线音频）
- 歌曲评分/统计
- 多用户/网络同步
- 音乐格式转换
- 歌词编辑/制作

## 9. 待决策

1. Bubble Tea v2 vs v1：倾向 v2（鼠标+帧率，Go 版本够），但 v2 示例少。→ 交 oracle 评审。
2. 终端图片库：go-termimg vs goberzurg vs 手写。→ 交 oracle 评审。
3. ffmpeg seek 重启 vs 其他方案：→ 交 oracle 评审。

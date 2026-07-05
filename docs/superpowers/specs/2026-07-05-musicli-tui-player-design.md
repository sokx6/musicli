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
| 终端图片 | 手写各协议 + `disintegration/imaging` 缩放 | kitty/sixel/iTerm2/halfblock，环境变量检测（无 TTY 查询，避免与 bubbletea 输入循环冲突） |
| FFT 频谱 | `github.com/cwbudde/algo-fft` + `algo-dsp` | SIMD 零分配 |
| 主题检测 | `godbus/dbus/v5` → gsettings → `muesli/termenv` OSC11 | 三级覆盖 |
| 配置 | `github.com/pelletier/go-toml/v2` | TOML 解析 |
| 日志 | 标准库 `log/slog` | 结构化日志，4 级（debug/info/warning/error） |

## 3. 架构

### 3.1 目录结构

```
musicli/
  cmd/musicli/main.go          # 入口：root context + 信号处理 + defer 清理
  internal/
    config/                    # TOML 配置加载、默认值、XDG 路径
      config.go
      defaults.go
      paths.go                 # XDG 路径解析（os.UserConfigDir 等）
      config.example.toml      # go:embed 嵌入，首次运行写出
      keybindings.go           # 快捷键配置模型
      theme.go                 # 主题配置模型
    log/                       # 日志审计系统
      logger.go                # slog 封装，4 级，启动时 O_TRUNC 清空
    audio/                     # 音频引擎（纯，不依赖 bubbletea）
      engine.go                # 播放控制（具体类型，非接口）
      ffmpeg.go                # ffmpeg 管道后端（解码+atempo+PCM）
      oto.go                   # oto 输出
      # spectrum.go 在阶段 9 加，不在阶段 1
    library/                   # 媒体库（叶子包，无跨依赖）
      track.go                 # Track 数据模型
      scanner.go               # 文件/文件夹扫描（具体类型）
      tag.go                   # dhowden/tag 封装
      album.go                 # 专辑分组
      sort.go                  # 排序（名字/大小/年份，正序倒序）
    playlist/                  # 歌单（依赖 library）
      playlist.go              # 歌单模型、增删、排序、持久化到 JSON
    lyrics/                    # 歌词系统（叶子包，无跨依赖）
      model.go                 # Line/Word/Lyric 归一化模型
      parser.go                # 解析器接口
      spl.go                   # SPL 解析器（含普通 LRC，SPL 是 LRC 超集，无单独 lrc.go）
      # yrc.go/qrc.go/krc.go 在实现时才创建，不预置 stub
      # fetcher.go/netease.go/qq.go/kugou.go/lrclib.go 在阶段 12 才创建
      matcher.go               # 本地歌曲→在线匹配（Search 接受 Query 结构体，非 Track）
    cover/                     # 专辑封面（叶子包，Render 接受 path/image）
      image.go                 # 从 path 提取封面
      render.go                # 渲染接口+协议检测（环境变量，无 TTY 查询）
      protocol_kitty.go        # kitty 图形协议
      protocol_sixel.go        # sixel
      protocol_iterm.go        # iTerm2
      protocol_halfblock.go    # ASCII 块回退
    theme/                     # 主题系统（叶子包）
      theme.go                 # 主题模型、深/浅色（具体类型）
      detect.go                # 系统主题检测+监听
      palette.go               # 调色板
    ui/                        # TUI 层（唯一依赖 bubbletea 的包）
      app.go                   # 顶层 model（bubbletea）
      layout.go                # 响应式布局
      views/                   # 各视图
        library.go             # 媒体库列表
        nowplaying.go          # 正在播放（封面+歌词）
        playlist.go            # 歌单管理
        spectrum.go            # 频谱可视化（阶段 9）
      widgets/                 # 可复用组件
        playerbar.go           # 底部播放栏（进度+控制）
        lyricsview.go          # 歌词逐字高亮
        coverview.go           # 封面显示
        list.go                # 通用列表（封装 bubbles/list）
      keys.go                  # 快捷键绑定（阶段 3 默认 KeyMap，阶段 11 TOML 覆盖）
      mouse.go                 # 鼠标处理
      styles.go                # lipgloss 样式（绑定 Theme，阶段 3 起就有）
  go.mod
  Makefile
  *_test.go                    # 各包核心逻辑自检测试
```

**关键调整（oracle 评审）**：
- 删除所有暂缓格式的 stub 文件（yrc/qrc/krc/fetcher 各源），实现时才创建
- 删除 `lyrics/lrc.go`：SPL 是 LRC 超集，一个解析器覆盖两者
- 删除顶层 `config/` 目录：用 `go:embed` 嵌入 `config.example.toml`
- `config.example.toml` 放 `internal/config/` 下

### 3.2 模块依赖（低耦合，叶子包优先）

```
config ← log ← (所有模块依赖这两个)
audio      (叶子包，纯，不依赖 bubbletea)
library    (叶子包)
lyrics     (叶子包，Search 接受 Query 结构体，非 library.Track)
cover      (叶子包，Render 接受 path/image，非 library.Track)
theme      (叶子包)
playlist → library
ui → {audio, library, playlist, lyrics, cover, theme}  # 唯一依赖 bubbletea 的包
```

**接口使用原则（oracle 评审）**：接口只在 ≥2 实现或有测试替身时才引入。
- `lyrics.Parser`：保留（SPL 现在，YRC/QRC/KRC 计划中）
- `lyrics.Fetcher`：保留（阶段 12 才创建，多源）
- `cover.Renderer`：保留（4 个实现）
- `audio.Engine`、`library.Scanner`、`theme.Detector`、`playlist.Manager`：**用具体类型**，单实现不接口化

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
- 输出：文件（`~/.local/state/musicli/musicli.log`），启动时 `O_TRUNC` 清空（v1 不做轮转，避免引入 lumberjack 依赖；日志增长慢，真有问题再加）
- 结构化：文本格式，带时间戳、级别、模块名、消息、上下文字段
- **错误必须带上下文**：文件路径、错误链（`fmt.Errorf("%w", err)`）、可定位位置
- 子 logger：`WithModule(name)` 带 module 字段

```go
// log/logger.go
type Logger struct { *slog.Logger; file *os.File }
func New(level string, path string) (*Logger, error)
func (l *Logger) WithModule(name string) *Logger
// 用法：log.WithModule("audio").Error("ffmpeg 启动失败", "path", p, "err", err)
```

### 4.3 音频引擎 (audio/)

具体类型（非接口，单实现）。纯包，不依赖 bubbletea——UI 用 30fps `tea.Tick` 轮询 `Position()`/`State()`，不用回调。

```go
type Engine struct { /* unexported fields */ }
func New(ctx context.Context, otoCtx *oto.Context, log *log.Logger) *Engine
func (e *Engine) Play(path string) error
func (e *Engine) Pause() error
func (e *Engine) Resume() error
func (e *Engine) Seek(positionMs int) error
func (e *Engine) SetVolume(v int) error        // 0-100
func (e *Engine) SetSpeed(s float64) error     // 0.5-2.0
func (e *Engine) Position() int                // 客户端计算，非查询 ffmpeg
func (e *Engine) Duration() int                // 总时长 ms（ffprobe 预取）
func (e *Engine) State() State                 // Playing|Paused|Stopped
func (e *Engine) Err() error                   // 异步错误（ffmpeg 崩溃等），UI tick 读取
// 阶段 9 加 SpectrumData，不在阶段 1 承诺
```

ffmpeg 后端：
- 启动：`ffmpeg -ss <seek> -i <path> -filter:a "atempo=<speed>" -f s16le -ar 48000 -ac 2 pipe:1`
- 倍速 >2 链式：`atempo=2.0,atempo=1.5`
- **seek（oracle 评审强制项）**：
  1. SIGTERM 旧 ffmpeg → 短超时 → SIGKILL
  2. `oto.Player.Reset()` 刷新 oto 缓冲（避免残留旧音频 ~100ms）
  3. 等 reader goroutine 退出（done channel / WaitGroup）→ `cmd.Wait()` 收割（防僵尸，每次 seek）
  4. 用新 `-ss` 重启 ffmpeg + 新 reader goroutine
- **位置追踪（客户端）**：记录 `startTimestampMs` + `playStartWallTime`，`Position() = startTimestampMs + int(time.Since(playStartWallTime)*speed)`，扣除暂停时长。不查询 ffmpeg。
- PCM 读取 goroutine：读 ffmpeg stdout → 阻塞写 oto（背压）→ 非阻塞写频谱环形缓冲（阶段 9 加，满则丢最旧）
- 时长：ffprobe 预取（`-show_entries format=duration`）；失败记 warning，duration=0，UI 显示 `--:--`，按百分比 seek 禁用
- **并发安全（oracle Risk-C）**：`Position()/State()/Duration()` 用 `sync.Mutex` 或 `atomic` 保护（UI 30fps 读 vs 音频 goroutine 写，`go test -race` 必过）
- **采样格式单一真相源**：常量 `SampleRate=48000, Channels=2, BitDepthInBytes=2`，ffmpeg 命令构造和 oto 初始化共用
- 错误处理：ffmpeg 启动失败/非零退出记 error（path、stderr、exitcode），设 `State()=Stopped` + `Err()`

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
    current   string
    stateDir  string  // ~/.local/state/musicli/
}
```
- 添加歌单、删除歌单、加入当前歌单、对当前歌单排序（复用 library/sort 逻辑）
- **持久化（oracle Miss-3）**：变更时 marshal 到 `~/.local/state/musicli/playlists.json`，启动时加载。几行代码，不丢用户数据。

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

爬取（lyrics/fetcher.go + 各源，阶段 12）：
- 接口 `Fetcher interface { Search(q Query) ([]Result, error); FetchLyric(id) (*Lyric, error) }`
- **`Query` 结构体**（非 library.Track，解耦）：`Query{Title, Artist, Album, DurationMs string/int}`
- 匹配：时长 ±4s 门控 + 标题/歌手/专辑文本相似度（LCS ratio），cutoff 55，差 15 内取最丰富
- **源顺序（oracle Adj-D）**：LRCLIB 首选（公开 REST，无 auth 无 crypto，返回纯 LRC，SPL 解析器直接吃）→ QQ（musicu.fcg，QRC 需 3DES+zlib）→ 网易云（公开 API `yv=-1`，YRC 纯文本）→ 酷狗（lyrics.kugou.com，KRC 需 XOR+zlib）
- 错误处理：网络失败记 warning（带 query/source），解析失败记 error（带原始内容片段）
- **QRC/KRC 解密自检（oracle Risk-E）**：实现时带已知密文→明文测试向量，crypto 不自检不可接受

**暂缓格式**：YRC/QRC/KRC 解析器+解密、TTML/LYS/LRCv2/Lyrics File。留 parser 接口和文件，实现为 `return nil, ErrNotImplemented`。

### 4.7 专辑封面 (cover/)

```go
type Renderer interface {
    Render(img image.Image, w, h int) string  # 返回带转义的字符串
}
```
- 提取：`Extract(path string) (image.Image, error)`——优先 tag.Picture()，无则查同目录 cover.jpg/folder.png 等（接受 path，非 Track，解耦）
- **协议检测（oracle 评审，无 TTY 查询，避免与 bubbletea 输入循环冲突）**：
  - `KITTY_WINDOW_ID` set 或 `TERM=xterm-kitty` → kitty
  - `TERM_PROGRAM == "WezTerm"` 或含 `iTerm` → iterm
  - `TERM` 含 `sixel` → sixel
  - `TMUX` set → 警告图形可能不透传，回退 halfblock（oracle Miss-5）
  - 否则 → halfblock
- 缩放：`disintegration/imaging` resize 到目标单元格像素尺寸
- kitty：`\x1b_Ga=T,t=d,f=100,s=,v=,c=,r=,m=;<b64>\x1b\\` 分块（~30 行）
- iterm：`\x1b]1337;File=...\x1b\\`（~10 行）
- halfblock：`▀▄` + ANSI 前后景色（~20 行）
- sixel：像素编码（~100 行，参考现有实现）
- 渲染策略：View() 占位空格 + 帧后 ANSI 光标定位写转义

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

### 6.1 优雅关闭与终端恢复（oracle Risk-A/F，spec 原缺失）

`main()` 强制项：
1. **root `context.Context`** + 信号处理（SIGINT/SIGTERM）
2. context 取消 → 关闭 ffmpeg stdout → reader goroutine 见 EOF 退出 → `cmd.Wait()` 收割
3. 关闭 oto context/player（oto context 在 `main()` 创建一次，全程复用，oracle Risk-B2）
4. **defer 终端清理**：退出 alt-screen、关鼠标模式、发 kitty 图形删除 `\x1b_Ga=d\x1b\\`
5. **`recover()`**：panic 时先记日志+堆栈，再恢复终端，避免崩溃留下损坏终端
6. flush 日志文件

测试：强制在封面渲染时 panic，验证终端恢复正常。

## 7. 实现阶段

每阶段一次或多次 git 提交，遵循 Conventional Commits。每阶段核心逻辑带至少一个 `go test` 自检（oracle Miss-6）。

| 阶段 | 内容 | 提交 |
|---|---|---|
| 0 | 工程骨架：go mod、目录、log（slog+O_TRUNC）、config（XDG+go:embed+默认）、root context+信号处理+defer 清理、Makefile、`go test` 约定、gitignore 测试音频 | feat: scaffold project |
| 1 | 音频引擎（ffmpeg+oto，播放/暂停/seek/音量/倍速，客户端位置追踪，oto context 单例，goroutine 同步，race-safe） | feat: audio engine |
| 2 | 媒体库（扫描、tag、专辑分组、排序） | feat: media library |
| 3 | 基础 TUI（布局、列表、播放栏、进度、键鼠）+ **最小 Theme 结构体**（默认深色调色板，styles.go 从第一天就绑定）+ **默认 KeyMap**（硬编码，阶段 11 才 TOML 覆盖） | feat: basic tui |
| 4 | SPL 歌词（解析器+逐字高亮视图，含普通 LRC 子集） | feat: spl lyrics |
| 5 | 专辑封面（kitty/sixel/iterm/halfblock，环境变量检测，tmux 警告） | feat: album cover |
| 6 | 排序与专辑视图 | feat: sorting and album view |
| 7 | 播放模式（随机/单曲/列表循环） | feat: playback modes |
| 8 | 歌单（增删/加入/排序/JSON 持久化） | feat: playlists |
| 9 | 频谱可视化（PCM tap 扇出 + FFT + 30fps tick） | feat: spectrum visualizer |
| 10 | 主题（TOML 自定义+深浅色+跟随系统检测+dbus 监听） | feat: theming |
| 11 | 快捷键自定义（TOML `[keybindings]` 覆盖默认） | feat: custom keybindings |
| 12 | 歌词自动爬取（**LRCLIB 首选** → QQ → 网易云 → 酷狗） | feat: lyric crawling |
| 13 | 其他逐字格式（YRC/QRC/KRC，带解密自检） | feat: more lyric formats |

## 8. 非目标（YAGNI）

- 音频均衡器
- 在线音乐流媒体播放（只爬歌词，不播放在线音频）
- 歌曲评分/统计
- 多用户/网络同步
- 音乐格式转换
- 歌词编辑/制作

## 9. 待决策

1. ~~Bubble Tea v2 vs v1~~ → **v2**（oracle 确认：greenfield 2026 选 v2，鼠标原生，charmbracelet 前向投入；bubbletea 导入限制在 `ui/` 包）
2. ~~终端图片库~~ → **手写各协议**（oracle 确认：库的值增薄，bubbletea 集成自己做；环境变量检测无 TTY 查询；sixel 若耗时可换库但只在 Renderer 接口后）
3. ~~ffmpeg seek 重启~~ → **kill+restart + `-ss`**（oracle 确认：管道流不可 seek，gapless 非目标；强制 oto flush + cmd.Wait 收割 + 客户端位置追踪 + goroutine 同步）

## 10. 非目标补充（oracle Miss-4）

- 播放状态恢复（下次启动恢复上次曲目/位置/音量）——v1 不做，YAGNI
- 音频均衡器
- 在线音乐流媒体播放（只爬歌词，不播放在线音频）
- 歌曲评分/统计
- 多用户/网络同步
- 音乐格式转换
- 歌词编辑/制作

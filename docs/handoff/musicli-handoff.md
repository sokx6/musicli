# musicli — TUI 音乐播放器交接文档

> 本文档供 Codex 接手开发使用，涵盖项目需求、技术选型、已完成工作、踩过的坑、待做阶段。

## 1. 项目概述

用 Go + Bubble Tea v2 写一个 TUI 命令行音乐播放器。模块化、低耦合、易扩展；TOML 配置；四级日志审计（debug/info/warning/error）；git 版本管理，每功能一次提交，遵循 Conventional Commits；不使用任何 emoji，可用 Material Design 3 unicode 图标；适配深/浅色并跟随系统自动切换；终端大小实时自适应；键盘+鼠标操作；可自定义快捷键。

## 2. 用户原始需求

### 核心功能
- 多种音乐格式支持（MP3/FLAC/OGG/WAV/M4A/AAC/Opus/AIFF）
- 添加音乐文件和音乐文件夹
- 读取 tag 显示歌曲信息（歌曲名、歌手、作曲家等）
- 按专辑分离
- 按名字、大小、年份等正序倒序排序
- 终端显示专辑封面（终端需支持图片协议）
- 歌词显示（默认展示封面和歌词）
- 进度显示、暂停、继续、下一首、上一首
- 随机播放、单曲循环、列表循环
- 倍速播放（不变调）
- 添加歌单、将歌曲添加到当前歌单、对当前歌单排序
- 频谱音乐可视化
- 自定义主题配色
- 自动爬取歌词（参考 https://github.com/chenmozhijin/LDDC）
- 易于键盘操作，可自定义快捷键
- 同样可以鼠标操作
- 界面美观，可用 Google MD3 unicode 图标，但不要使用任何 emoji
- 适配深色和浅色模式，能跟随系统自动切换
- 根据终端大小变化实时自适应布局

### 歌词要求
- 支持普通 LRC 歌词（基本要求）
- 支持逐字歌词：**SPL 首要支持**，其他格式（Enhanced LRC、YRC、LYS、TTML、LRCv2）暂缓留接口
- SPL 标准：https://moriafly.com/standards/spl.html
- 逐字歌词文档参考：/home/locxl/Downloads/lyric format.md
- 实现参考：https://github.com/sokx6/music-cli（ASCII 实现，脆弱，可参考思路）

### 工程要求
- 所有可配置项使用 TOML 配置文件
- 代码模块化、低耦合、便于扩展
- 完整的日志审计系统（warning、info、error、debug 四个等级）
- 日志格式规范：`时间[级别][模块][函数]消息`，错误必须包装传递（`fmt.Errorf("...: %w", err)`）
- 使用 git 版本管理，每实现一个功能进行一次提交
- 符合 commit message 规范（Conventional Commits）

## 3. 环境信息

- **OS**: Arch Linux
- **Go**: go1.26.4 linux/amd64
- **gcc**: 16.1.1
- **ffmpeg/ffprobe**: n8.1.2（已安装，作为音频后端）
- **终端**: kitty（TERM=xterm-kitty, KITTY=1）
- **git**: locxl <sokx126@gmail.com>

## 4. 技术选型（已确认）

| 关注点 | 选型 | 理由 |
|---|---|---|
| TUI 框架 | `charm.land/bubbletea/v2` + `bubbles/v2` + `lipgloss/v2` | v2 稳定，原生鼠标+帧率渲染，Go 1.25+ 满足 |
| 音频输出 | `github.com/ebitengine/oto/v3` | 吃 ffmpeg PCM 流，低层稳定 |
| 解码+倍速 | ffmpeg/ffprobe 管道 + atempo 滤镜 | 格式最广（含 M4A/AAC/ALAC），倍速不变调 |
| Tag 读取 | `github.com/dhowden/tag` | 覆盖 MP3/MP4/FLAC/OGG/Opus + 封面 |
| 终端图片 | **手写各协议**（不用库） | 库与 bubbletea 冲突；环境变量检测，无 TTY 查询 |
| 图片缩放 | `github.com/disintegration/imaging` | Lanczos 缩放 |
| FFT 频谱 | `github.com/cwbudde/algo-fft` + `algo-dsp` | SIMD 零分配 |
| 主题检测 | `godbus/dbus/v5` → gsettings → `muesli/termenv` OSC11 | 三级覆盖 |
| 配置 | `github.com/pelletier/go-toml/v2` + `go:embed` | XDG 目录，首次运行写默认配置 |
| 日志 | 标准库 `log/slog` 自定义 Handler | 4 级，truncate-on-start，格式 `时间[级别][模块][函数]消息` |

### 为什么选 ffmpeg 后端（而非纯 Go）
- 纯 Go 解码器不支持 M4A/AAC（无成熟纯 Go AAC 解码器）
- 纯 Go 不变调倍速不成熟（algo-dsp 预 v1；sonic-go 实为 CGo）
- ffmpeg `atempo` 滤镜覆盖 0.5x-2.0x，不变调，实时

### 为什么选 Bubble Tea v2（而非 v1）
- greenfield 2026 项目，v1 是遗留路径
- v2 原生鼠标支持（`MouseMsg` 接口 + `View.MouseMode`）
- Go 1.26.4 满足 v2 的 Go 1.25+ 要求
- v2 的 `View()` 返回 `tea.View` 结构体（非 string），`Content` 字段存渲染内容

### 为什么手写终端图片协议（而非用库）
- `go-termimg` 做同步 ANSI 查询，与 bubbletea 的 TTY 输入循环冲突
- `goberzurg` 仅环境变量检测（正确方向），但值增薄
- bubbletea 集成（View 里占位 + 帧后 ANSI 光标定位）必须自己做，库帮不了
- kitty 协议 ~30 行，iTerm2 ~10 行，halfblock ~20 行，sixel ~100 行

## 5. 项目结构

```
musicli/
  cmd/musicli/main.go              # 入口：root context + 信号处理 + defer 清理
  internal/
    config/                        # TOML 配置（叶子包）
      config.go                    # Config 结构体、Load、Defaults、applyDefaults、expandPaths
      config.example.toml          # go:embed 嵌入的默认配置
      paths.go                     # XDG 路径解析（os.UserConfigDir 等）
      config_test.go
    log/                           # 日志系统（叶子包）
      logger.go                    # slog 自定义 Handler，格式: 时间[级别][模块][函数]消息
      logger_test.go               # 4 级测试 + 格式测试 + truncate 测试
    audio/                         # 音频引擎（叶子包，不依赖 bubbletea）
      engine.go                    # Engine 结构体（具体类型，非接口）
      ffmpeg.go                    # probeDuration、bytesBuffer、信号常量
      engine_test.go               # 集成测试（真实 MP3 播放）
    library/                       # 媒体库（叶子包）
      track.go                     # Track、Album 数据模型
      tag.go                       # ReadTags（dhowden/tag 封装，失败用文件名兜底）
      scanner.go                   # Scanner（递归扫描，9 种扩展名）
      duration.go                  # probeDuration（ffprobe 封装，本地副本避免依赖 audio）
      album.go                     # GroupByAlbum
      sort.go                      # SortTracks（title/artist/album/size/year，asc/desc）
      *_test.go
    theme/                         # 主题（叶子包）
      theme.go                     # Theme 结构体、Default() 返回暗色主题
    ui/                            # TUI 层（唯一依赖 bubbletea 的包）
      app.go                       # App model、View、Update、布局、播放控制
      styles.go                    # Styles、newListStyles、newListComponentStyles、KeyMap
      render_probe.go              # 诊断程序（//go:build ignore，可删）
  docs/superpowers/specs/          # 设计文档
  docs/handoff/                    # 本交接文档
  go.mod
  Makefile
  .gitignore                       # 含 *.mp3 等音频文件排除
```

### 模块依赖图（低耦合，叶子包优先）
```
config ← log ← (all)
audio      (叶子包，纯，不依赖 bubbletea)
library    (叶子包)
lyrics     (叶子包，Query 结构体，非 library.Track)  ← 待创建
cover      (叶子包，Render 接受 path/image)          ← 待创建
theme      (叶子包)
playlist → library                                   ← 待创建
ui → {audio, library, playlist, lyrics, cover, theme}  # 唯一依赖 bubbletea
```

### 接口使用原则
- 接口只在 ≥2 实现或有测试替身时引入
- `lyrics.Parser`：保留（SPL 现在，YRC/QRC/KRC 计划中）
- `lyrics.Fetcher`：保留（阶段 12 才创建，多源）
- `cover.Renderer`：保留（4 个实现）
- `audio.Engine`、`library.Scanner`、`theme.Detector`、`playlist.Manager`：**用具体类型**，单实现不接口化

## 6. 已完成阶段

### 阶段 0: 工程骨架 (6589a1d)
- go module `github.com/locxl/musicli`
- `internal/config`：XDG 路径、go:embed 默认配置、首次运行写出、字段校验/钳制、`~` 展开
- `internal/log`：slog 自定义 Handler，4 级，truncate-on-start
- `cmd/musicli`：root context + 信号处理（SIGINT/SIGTERM）、deferred logger close
- Makefile、.gitignore（排除测试音频）

### 阶段 1: 音频引擎 (65a6bff, 多次修复至 5b90120)
- ffmpeg 管道后端：`ffmpeg -ss <offset> -i <path> -filter:a "atempo=<speed>" -f s16le -ar 48000 -ac 2 -vn pipe:1`
- oto v3 输出：Context 单例（main 创建一次），每曲/每次 seek 新建 Player
- **io.Pipe 中间层**：goroutine 把 ffmpeg stdout 复制到 pipe writer，oto 从 pipe reader 读。关闭 pipe writer → oto reader 立即 EOF → mux 不再轮询旧 player（这解决了"没声音"的核心 bug）
- 客户端位置追踪：`Position() = startTimestampMs + int(time.Since(playStartWallTime)*speed) - pausedDuration`
- **Seek**：SIGKILL 旧 ffmpeg → player.SetVolume(0) + player.Pause()（立即静音 oto buffer）→ 新 ffmpeg 在 `-ss` 重启
- **Pause**：stopInternal(false)（不等 readerLoop）→ player 立即静音；Resume 重新 spawn ffmpeg 在暂停位置
- race-safe：`sync.Mutex` 保护所有共享状态，`go test -race` 通过
- 采样格式单一真相源：`SampleRate=48000, ChannelCount=2, BitDepthInBytes=2`
- probeDuration 后台 goroutine（不阻塞播放启动）

**关键 bug 修复历史**：
1. **"没声音"根因**：oto v3 的 mux 是单 goroutine 遍历所有 players。旧 player 的 reader（ffmpeg stdout）没关闭，mux 卡在旧 player 的阻塞 reader 上，新 player 饥饿。修复：io.Pipe 中间层，停止时关 pipe writer 让 oto reader EOF。
2. **"暂停后多遍播放"根因**：Pause() 只改了 state 标记，没停 ffmpeg/oto，音频继续播；Resume() 又启动新 ffmpeg → 多路同时播放。修复：Pause 真正调 stopInternal。
3. **"切歌延迟 2 秒"根因**：SIGTERM + 3 秒超时，ffmpeg 写满管道时不响应 SIGTERM。修复：直接 SIGKILL + player 立即静音 + 不等 readerLoop。

### 阶段 2: 媒体库 (54f3107)
- `Track` 结构体：Path/Title/Artist/AlbumArtist/Album/Composer/Genre/Year/TrackNo/DiscNo/Duration/Size/HasCover
- `ReadTags`：dhowden/tag 封装，失败用文件名兜底（Track 仍入库）
- `Scanner`：递归扫描，9 种扩展名（mp3/flac/ogg/wav/m4a/aac/opus/aiff/wma），tag/stat 失败记 warning 不中断
- 启动扫描不再对每首歌跑 `ffprobe`；当前播放曲的时长由 audio engine 在播放时异步探测，避免大目录启动被大量子进程阻塞
- `GroupByAlbum`：按 Album 聚合，Unknown Album/Artist 兜底，专辑内按 DiscNo/TrackNo/Title 排序
- `SortTracks`：5 字段 × 2 方向，空值/零值排末尾

### 阶段 3: 基础 TUI (9692813, 多次修复至 62089cb)
- **布局**（最终版）：
  ```
  ┌──────────────────────────────────┐
  │ ▶ title - artist - album (居中)  │  顶部栏（1行 + 底边框）
  ├──────────────┬───────────────────┤
  │ cover+lyrics │  tracks list      │  左：封面/歌词（占位）│ 右：歌曲列表
  │ (占位)       │  (bubbles/list)   │
  ├──────────────┴───────────────────┤
  │ progress + time + vol/speed      │  播放栏（3行 + 顶边框）
  └──────────────────────────────────┘
  ```
- **list 宽度跟随内容**（非填满 rightPane）：`trackListContentWidth()` 根据实际标题/描述宽度计算，`[ui] track_list_max_width` 配置上限
- 100ms tick 轮询 engine 状态（Position/State/Duration/Volume/Speed），状态变化时才记日志
- 键盘：enter 播放、space 暂停/继续、n/b 下一首/上一首、←→ seek、+- 音量、[] 倍速、/ 过滤、q 退出
- 鼠标：点击列表项选中+播放（不同曲目才触发）、点击进度条 seek
- 最小 Theme 结构体（暗色调色板），styles.go 从第一天就绑定 Theme
- 默认 KeyMap（硬编码，阶段 11 加 TOML 覆盖）
- 透明背景（不用 Background 填充，用终端原生背景）
- border 用 `Border(lipgloss.NormalBorder(), sides...)`（lipgloss v2 必须先设 border 类型）
- 响应式：宽 <80 隐藏左区，所有尺寸 clamp ≥1

**关键 bug 修复历史**：
1. **灰色块根因**：bubbles/list 的 `list.DefaultItemStyles` 和 `list.DefaultStyles` 自带背景色。修复：用 `lipgloss.NewStyle()` 全新创建所有样式，不继承默认背景。
2. **border 不渲染根因**：lipgloss v2 的 `BorderRight(true)` 单独使用不渲染任何边框。必须先调 `Border(lipgloss.NormalBorder())` 设置边框类型，再用参数指定哪些边。通过独立测试程序确认。
3. **list 右侧空白根因**：`bubbles/list` 在 `list.New()` 时存了 delegate 的内部副本。`resizeComponents` 更新 `a.delegate.Styles`（app 副本），list 内部还是旧样式。修复：更新后调 `a.trackList.SetDelegate(a.delegate)`。但最终方案改为 list 宽度跟随内容（非填满 pane），从根本上消除空白。
4. **紫色标题栏根因**：`list.DefaultStyles` 的 TitleBar 有默认紫色背景。修复：用 `lipgloss.NewStyle()` 全新创建。

### 阶段 4: SPL 歌词解析 + 逐字高亮
- `internal/lyrics/`：普通 LRC、Enhanced LRC 子集、SPL 解析，统一为 `Lyric`/`Line`/`Word`
- 本地歌词加载：按音频文件路径查找同名 `.spl` 或 `.lrc`
- UI：当前行居中、逐字高亮、CJK 宽字符宽度约束
- 逐字高亮 CJK 偏移修复：歌词活跃行/词变化时强制全屏重绘，规避 Bubble Tea diff 对 SGR + 宽字符的错位

### 阶段 5: 专辑封面
- `internal/cover/`：封面提取、halfblock 渲染、kitty 图片协议渲染
- 封面提取优先 tag artwork，无内嵌图时查同目录 `cover.*`/`folder.*`
- 左栏支持 `v` 切换封面+歌词、只封面、只歌词
- `c` 切换 `fit`/`stretch`，默认由 `[cover] scale` 配置
- kitty 渲染使用覆盖层命令，避免歌词重绘导致图片闪烁；歌词-only 时清除图片

### 阶段 6: 排序与专辑视图
- `[library] sort_field`、`sort_order` 控制扫描后曲目排序
- `[library] group_by_album` 控制默认进入全部曲目或专辑视图，当前默认 `false`
- `tab` 切换全部曲目/专辑列表，`enter` 进入专辑曲目，`esc`/`backspace` 返回专辑列表
- 专辑曲目选择映射回全局曲目索引；在专辑曲目视图中，播放范围限定为当前专辑

### 阶段 7: 播放模式
- `[playback] repeat = "none" | "one" | "list"`，默认 `list`
- `[playback] shuffle = false | true`
- 歌曲自然结束后按播放模式自动续播：`one` 重播当前曲，`list` 下一首并在末尾回到第一首，`none` 在最后一首结束后停止
- shuffle 开启时自动下一首和手动下一首会选择不同于当前曲的随机曲目（至少两首时）
- 手动 `n`/`l` 下一首保留显式导航语义，`repeat = "none"` 时仍可从末尾回到开头
- 手动 `b`/`h` 上一首保持列表顺序，不维护 shuffle 历史
- 在专辑曲目视图中，下一首/上一首/自动续播/shuffle 都限定在当前专辑内
- `r` 切换 repeat：`list` → `one` → `none` → `list`
- `s` 切换 shuffle：`off` ↔ `on`
- 播放栏显示当前 repeat/shuffle 状态

### 阶段 7.5: 播放栏与歌词布局稳定化
- 进度条使用当前 `pos/dur` 直接渲染静态百分比，避免只显示空进度
- 播放栏保持 1 行顶边框 + 3 行内容：状态行、进度条、快捷键提示
- 窄终端下状态行和快捷键提示逐级降级为简写，必要时隐藏低优先级提示，保证每行不换行
- `a` 切换歌词对齐：左对齐 → 居中 → 右对齐 → 左对齐
- 歌词对齐只作用在歌词 pane 内；封面+歌词同时显示时，歌词不会进入封面区域
- `[lyrics] align = "left" | "center" | "right"` 控制启动时默认歌词对齐，`a` 只切换当前会话
- CJK 逐字高亮当前行仍保持定宽渲染，避免宽字符高亮状态变化时产生偏移

### 阶段 8.5: 列表/filter 稳定化 + 大目录启动性能
- filter 搜索态和已应用 filter 态下，歌曲标题保持单行显示，不因高亮 SGR 或外层 right pane 二次渲染而换行
- filter 命中的长标题会按列表可用宽度正常截断，不会被高亮 chunk 的固定宽度样式切碎或过早截断
- filtered queue 只在曲目列表处于过滤状态时生效；重置 filter 后播放队列回到全部曲目/当前专辑语义
- right pane 使用 `fitBlock(trackList.View(), width, height)` 裁剪到栏位尺寸，避免 Lip Gloss 外层 `Width()` 对已渲染列表再次 word wrap
- 启动扫描移除逐文件 `ffprobe` duration probe：大目录下不再为每个音频文件启动外部进程；当前播放曲仍由 audio engine 异步探测时长
- 回归测试覆盖 filter 单行显示、完整 App.View 渲染、filter reset 后 queue 来源、以及 library 扫描不调用 `ffprobe`

### 阶段 8.6: 音乐目录索引缓存 + filter 播放映射
- `[library] music_dir` 可配置默认音乐目录；命令行传入路径时优先使用命令行路径
- `[library] index_cache = true` 默认启用持久化索引，保存在 `~/.local/state/musicli/library-index.json`
- 启动时仍轻量遍历目录以发现变化，但路径、文件大小和修改时间未变的音频文件直接复用缓存 tag；新增、删除、重命名或修改文件会自动更新索引
- filter 结果播放通过当前选中的实际 `trackItem` 映射回全库，避免把过滤结果索引误当作全库索引而播放错歌

### 阶段 8: 歌单
- `internal/playlist/`：路径列表 JSON 存储，原子写入 `~/.local/state/musicli/playlists.json`
- 默认不可删除的 `Favorites` 歌单；`f` 快速收藏/取消收藏当前播放歌曲，未播放时作用于选中歌曲
- 收藏歌曲在曲目标题前显示 `★`
- `p` 进入/退出歌单列表；`enter` 进入歌单，歌单内播放会将下一首/上一首范围限定为该歌单
- `m` 在曲目列表选择目标歌单并加入选中歌曲；`N` 在歌单列表创建新歌单
- 歌单曲目视图中 `x` 移除选中歌曲，`o` 按当前曲目标题排序；歌单列表中 `d` 删除用户歌单

## 7. 日志系统

### 格式
```
2026-07-06T00:08:28.140+08:00[DEBUG][ui][View] render diagnostics rightPaneW=117 listW=117
```

### 位置
- 配置文件：`~/.config/musicli/config.toml`
- 日志文件：`~/.local/state/musicli/musicli.log`（每次启动 O_TRUNC 清空）
- 开发期默认级别：`debug`

### 使用方式
```go
fl := e.log.WithFunc("startFFmpeg")
fl.Debug("spawning ffmpeg", "path", path, "offset_ms", offsetMs, "speed", speed)
fl.Error("ffmpeg start failed", "path", path, "err", fmt.Errorf("Start: %w", err))
```

### 日志规范
- 每个函数开头创建 `fl := log.WithModule("module").WithFunc("FuncName")`
- 错误必须用 `fmt.Errorf("...: %w", err)` 包装，作为 `"err"` 属性传递
- 关键路径（ffmpeg/tag/歌词/网络/主题/配置）失败必须记日志
- 热路径（tick/poll）只在状态变化时记日志，不每帧记
- 所有上下文信息（路径、值、PID 等）作为结构化属性传递

## 8. 配置

### 配置文件 (~/.config/musicli/config.toml)
```toml
[audio]
volume = 80
speed = 1.0

[playback]
repeat = "list"      # none | one | list
shuffle = false

[library]
sort_field = "title" # title | artist | album | size | year
sort_order = "asc"   # asc | desc
group_by_album = true
music_dir = ""       # empty = command-line path, then current directory
index_cache = true    # reuse unchanged metadata from ~/.local/state/musicli/library-index.json

[lyrics]
auto_fetch = false
sources = ["lrclib", "qq", "netease", "kugou"]
save_dir = ""        # empty = ~/.cache/musicli/lyrics
align = "left"       # left | center | right

[cover]
show = true
protocol = "auto"    # auto | kitty | sixel | iterm | halfblock

[theme]
mode = "auto"        # auto | dark | light
name = "default"

[ui]
track_list_max_width = 80  # 0 = unlimited
progress_style = "bar"     # bar | separator; separator uses a pixel-level kitty overlay when available
separator_progress_thickness = 1 # 1-8 pixels; kitty separator overlay only

[keybindings]
# 空 = 用内置默认

[log]
level = "debug"      # debug | info | warning | error
file = ""            # empty = ~/.local/state/musicli/musicli.log
```

### XDG 目录
- 配置：`~/.config/musicli/`
- 状态（日志、playlists.json）：`~/.local/state/musicli/`
- 缓存（封面、歌词）：`~/.cache/musicli/`

## 9. 待完成阶段

### 阶段 9: 频谱可视化
- PCM tap：ffmpeg stdout reader goroutine 扇出到环形缓冲（非阻塞，满则丢最旧）
- FFT：`cwbudde/algo-fft` + `algo-dsp`（Hann 窗口）
- 30fps tick 读频谱数据
- 渲染：`▁▂▃▄▅▆▇█` 块字符或 lipgloss 渐变柱
- 参考：`Charlyhokno-eng/go-cli-beat`（cava 风格 attack/decay）

### 阶段 10: 主题
- TOML 自定义调色板（深/浅色各一套）
- 系统检测：`godbus/dbus/v5` XDG portal → gsettings → termenv OSC11
- 监听变化：dbus SettingChanged 信号 → goroutine → p.Send(themeChangedMsg{})
- 扩展 Theme 结构体（阶段 3 已有最小版本）

### 阶段 11: 快捷键自定义
- TOML `[keybindings]` 覆盖默认 KeyMap（阶段 3 已有默认）

### 阶段 12: 歌词自动爬取
- 创建 `internal/lyrics/fetcher.go` + 各源
- `Fetcher` 接口：`Search(q Query) ([]Result, error)` / `FetchLyric(id) (*Lyric, error)`
- `Query` 结构体（非 library.Track，解耦）：`{Title, Artist, Album, DurationMs}`
- 匹配：时长 ±4s 门控 + 文本相似度评分，cutoff 55
- 源顺序：**LRCLIB 首选**（公开 REST，无 auth 无 crypto）→ QQ → 网易云 → 酷狗
- QQ 音乐：musicu.fcg，QRC 需 3DES ECB（key `!@#)(*$%123ZXC!@!@#)(NHL`）+ zlib 解密
- 网易云：公开 API `yv=-1`，YRC 纯文本无需解密
- 酷狗：lyrics.kugou.com，KRC 需 XOR（key `@Gaw^2tGQ61-\xce\xd2ni`）+ zlib 解密
- QRC/KRC 解密必须带已知密文→明文测试向量（crypto 不自检不可接受）

### 阶段 13: 其他逐字格式（暂缓）
- YRC/QRC/KRC 解析器 + 解密
- TTML/LYS/LRCv2/Lyrics File

## 10. 歌词格式规格速查

### SPL（首要格式）
- LRC 的严格超集，行式文本
- 时间戳 `[mm:ss.xx]`：分 1-3 位，秒 1-2 位，小数 1-6 位（<3 位右补零到 3 位取毫秒）
- 逐字：`[05:20.22]你好[05:23.22]椒盐音乐[05:24.22]`（首 ts=行开始+首字开始，中间=字界，末=行结束）
- 延迟逐字：`[05:20.22]<05:21.22>你好<05:23.22>椒盐音乐[05:24.22]`（`<>` = 非边界标记）
- 翻译：与主行共享时间戳，或紧随其后的无时间戳行
- 字时间戳必须单调递增

### 其他格式（暂缓）
| 格式 | 行 | 字 | 关键点 |
|---|---|---|---|
| YRC | `[start,dur]` | `(start,dur,0)text` abs | ts 先于 text，3 字段，纯文本 |
| QRC | `[start,dur]` | `text(start,dur)` abs | text 先于 ts，2 字段，需 3DES+zlib |
| KRC | `[start,dur]` | `<offset,dur,0>text` **相对行** | 需 XOR+zlib；word_abs=lineStart+offset |
| LYS | 首字 start | `text(start,dur)` abs | `[prop]` 编码 bg+duet |
| TTML | `p@begin/end` | `span@begin/end` | `itunes:timing=Word`；秒或时钟格式 |
| LRCv2 | — | `<mm:ss.xx>word</mm:ss.xx>` | 不是 YAML（YAML 是 lrcget 的 Lyrics File）|

## 11. 关键经验教训（踩过的坑）

### lipgloss v2 border
- `BorderRight(true)` / `BorderTop(true)` 单独使用**不渲染任何边框**
- 必须先调 `Border(lipgloss.NormalBorder())` 设置边框类型，再用参数指定哪些边
- 验证方法：独立测试程序打印输出，用 `lipgloss.Width()` 量每行宽度

### bubbles/list delegate 副本
- `list.New(items, delegate, w, h)` 存了 delegate 的**内部副本**
- 更新 `a.delegate.Styles` 只改 app 副本，list 内部还是旧样式
- 修复：调 `a.trackList.SetDelegate(a.delegate)` 推回，或让 list 宽度跟随内容

### bubbles/list 默认样式带背景色
- `list.NewDefaultItemStyles()` 和 `list.DefaultStyles()` 自带背景色（紫色/灰色）
- 导致灰色块、紫色标题栏
- 修复：用 `lipgloss.NewStyle()` 全新创建所有样式，不继承默认

### oto v3 mux 单 goroutine
- oto 内部 mux 是单 goroutine 遍历所有 players
- 旧 player 的 reader 没关闭 → mux 阻塞在旧 reader → 新 player 饥饿 → "没声音"
- 修复：io.Pipe 中间层，停止时关 pipe writer → oto reader EOF → mux 释放

### oto v3 API 要点
- `NewContext` 需 SampleRate/ChannelCount/Format，Context 全局单例
- `NewPlayer(io.Reader)` — oto 内部 mux goroutine 从 reader 拉数据
- `Play()` / `Pause()` — 无 `Resume`（用 `Play()` 恢复）
- `SetVolume(float64)` — 范围 [0, 1]，不是 0-100
- `Reset()` 已弃用；清缓冲用 `Seek(0, io.SeekCurrent)`（需 Seeker）或丢弃 player 新建
- `Close()` 已弃用（v3.4 no-op），靠 finalizer 清理

### ffmpeg 管道 seek
- 管道流不可 seek，seek = kill + restart with new `-ss`
- SIGTERM 对写满管道的 ffmpeg 无效，用 SIGKILL
- kill 后 oto buffer 仍有残留音频，需 `player.SetVolume(0)` + `player.Pause()` 立即静音
- 每次 seek 必须 `cmd.Wait()` 收割防僵尸

## 12. 参考项目

- `zyoung11/BM`：beep + dhowden/tag + kitty/sixel 封面 + 取色
- `Charlyhokno-eng/go-cli-beat`：cava 频谱 + beep + lipgloss + 清晰 internal 分层
- `sphildreth/tunez`：bubbletea + mpv 后端（架构参考）
- `raziman18/gomu`：beep Ctrl/Resampler/Volume 组合范例
- `MiChongs/karpov-gateway`（MIT，歌词爬取，musicu.fcg + EAPI）
- `go-musicfox/go-musicfox`（MIT，NetEase EAPI + YRC）
- `winterssy/mxget`（GPL-3，多源歌词）

## 13. 开发约定

- 每阶段一次 git 提交，Conventional Commits（`feat:`/`fix:`/`refactor:`/`docs:`/`chore:`）
- 每阶段核心逻辑带至少一个 `go test` 自检
- `go test -race` 必须通过
- 开发期日志级别 `debug`
- 不使用 emoji，可用 MD3 unicode 图标（▶ ⏸ ⏹ ⏮ ⏭ 等）
- 测试音频文件不提交 git（.gitignore 排除 *.mp3 等）

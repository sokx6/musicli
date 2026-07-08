# Kitty 封面图 Base64 泄漏与性能卡顿：根因分析与修复

## 问题现象

### 问题一：按 `v`/`c` 切换或拉伸时 base64 泄漏到屏幕

在 kitty 终端下，按 `v` 键（切换左栏内容模式）或 `c` 键（切换
fit/stretch）后，终端会闪过一大串 base64 字符而非正常切换图片。
泄漏的字符残留在屏幕上，图片不更新。

### 问题二：所有按键延迟严重（界面卡顿）

播放期间所有按键（不止 `v`/`c`）响应延迟很大，操作不跟手。

### 问题三：封面图比例失真（非正方形）

封面图视觉上宽度比高度略小，fit 模式下尤其明显。

## 环境

- 渲染框架：charm.land/bubbletea/v2 v2.0.8，启动配置 `WithFPS(30)`
  （渲染器帧间隔 33ms，非默认 60fps/16ms）
- 渲染器：ultraviolet v0.0.0-20260703014108-f5a850f9c2b7
  的 `cursedRenderer`
- 图形协议：kitty graphics protocol（`a=T` 虚拟放置模式）
- 终端：kitty（`TERM=xterm-kitty`）
- tick 间隔：50ms（`tickInterval = 50 * time.Millisecond`）

## 根因总结

三个独立根因，对应三个问题。

### 根因一：kitty APC 分块传输格式错误导致图片不显示

`RenderKitty()` 将整个 base64 PNG 作为单个 APC
（Application Programming Command）发送。典型专辑封面经 PNG
编码 + base64 后达到 200KB–600KB。当这个巨型 APC 与 bubbletea
渲染器的 SGR diff 输出在同一个 renderer flush 中写入 TTY 时，
kitty 的 APC 解析器被打断，base64 作为普通文本泄漏到屏幕。

修复方案是将大 payload 分块传输，但最初的分块实现有两个致命错误
（详见下文「失败方案回顾」），导致 kitty 无法显示图片。最终的
正确分块格式使用 `m=1`/`m=0` 标志，符合 kitty graphics protocol
规范。

### 根因二：每 50ms tick 触发昂贵的 PNG 编码 + base64 阻塞事件循环

`tickMsg` 处理器每 50ms 调用 `kittyCoverCmd()`，后者调用
`renderKittyCoverOverlay()` → `RenderKitty()`，即使图片没有任何
变化也会执行完整的 PNG 编码 + base64 编码。这个 CPU 密集操作
（典型耗时 20–50ms）阻塞 bubbletea 事件循环，导致所有按键消息
排队等待，表现为全局卡顿。

### 根因三：硬编码终端单元格像素尺寸

`cover/protocol.go` 硬编码 `kittyCellPixelWidth = 10`、
`kittyCellPixelHeight = 20`。如果终端实际单元格是 `9×20`
（某些字体配置下常见），画布像素宽度 `C×10` 但显示区域
`C×9`，横向被压缩，图片看起来「宽比高小」。

## 渲染管线详解

### 1. 事件循环与刷新循环

bubbletea v2 有两条并行执行的路径：

**事件循环**（`tea.go:eventLoop`）：

```go
for {
    select {
    case msg := <-p.msgs:
        switch msg := msg.(type) {
        case clearScreenMsg:
            p.renderer.clearScreen()    // 设 s.clear = true
        case RawMsg:
            p.execute(fmt.Sprint(msg.Msg))  // 写入 p.outputBuf
        // ...
        }
        model, cmd = model.Update(msg)  // 调 Update
        p.render(model)                 // 存储 View 到 renderer.s.view
    }
}
```

**刷新循环**（`tea.go:startRenderer`）：

```go
go func() {
    for {
        select {
        case <-p.rendererDone:
            return
        case <-p.ticker.C:              // 30fps ≈ 33ms
            _ = p.flush()               // (A) 刷 p.outputBuf → TTY
            _ = p.renderer.flush(false) // (B) 刷 s.view diff → TTY
        }
    }
}()
```

关键点：**(A) 原始输出（kitty APC）永远在 (B) 视图 diff 之前写**。
两条输出共享同一个 `p.output`（即 `os.Stdout`）。

### 2. 两套输出通道

| 通道 | 来源 | 写入方式 | 内容 |
|------|------|----------|------|
| `p.outputBuf` | `tea.Raw()` → `RawMsg` → `p.execute()` | `p.flush()` 直接写 TTY | kitty APC（base64 PNG） |
| `s.buf` | `View()` → diff 引擎 | `p.renderer.flush()` 写 TTY | SGR 增量更新（文本 cell） |

当两个通道在同一帧刷新时，输出顺序是 `p.outputBuf` 先、
`s.buf` 后。如果 APC payload 过大，kitty 的解析器还没处理完，
SGR 序列就到了，APC 流被打断。

### 3. kitty graphics protocol 的虚拟放置

kitty 的 `a=T`（transmit + virtual placement）模式把图片以虚拟
放置方式叠加在终端文本层之上。传输序列结构：

```
\x1b_Ga=d,d=I,i=1\x1b\           ← 删除旧图片 (id=1)
\x1b[3;1H                          ← 光标定位到 (行3, 列1)
\x1b_Ga=T,t=d,f=100,i=1,c=W,r=H,z=1;<base64 PNG>\x1b\\  ← 传输新图片
```

图片「浮」在文本层上面。渲染器的 diff 引擎不知道图片的存在。
当 diff 引擎把文本 cell 写到图片所在区域时，kitty 用文本覆盖
图片像素（因为文本在图片之后绘制）。

### 4. kitty graphics protocol 的分块传输

当 base64 payload 过大时，kitty 规范要求分块传输。每个 APC chunk
通过 `m` 控制字段标记：

- `m=1`：后面还有更多数据
- `m=0`：序列完成，可以处理
- 省略 `m`：默认 `m=0`（完整消息）

**关键规则**：
- 第一个 chunk 携带完整命令头（`a=T,f=100,i=ID,...`）+ `m=1`
- 中间 chunk **只有** `m=1`（不能有 `i=ID`，关联靠顺序）
- 最后一个 chunk **只有** `m=0` + 剩余 payload
- chunk 之间不能插入其他 graphics 转义码
- kitty 在收到 `m=0` 之前不会显示图片

### 5. 为什么 halfblock 不受影响

halfblock 协议把图片编码成 `▀`（上半块）+ 24bit 前景色/背景色
的文本。图片本身就是 `View()` 的一部分，存在 cell buffer 里。
不存在 APC 解析器被打断的问题，也不存在虚拟图片被文本覆盖的问题。

## 修复方案

### 修复一：正确分块传输 kitty APC payload

`internal/cover/protocol.go` 的 `RenderKitty()` 函数：

```go
const (
    // ...
    // kittyMaxChunkSize is the maximum base64 payload per APC.
    kittyMaxChunkSize = 4096
)

func RenderKitty(img image.Image, placement KittyPlacement) (string, error) {
    if img == nil || placement.ID <= 0 || placement.Width <= 0 || placement.Height <= 0 {
        return ClearKittyImage(placement.ID), nil
    }

    renderImg := imageCanvas(img, placement.Width, placement.Height, placement.Scale, placement.CellW, placement.CellH)

    var pngBuf bytes.Buffer
    if err := png.Encode(&pngBuf, renderImg); err != nil {
        return "", fmt.Errorf("encode kitty png: %w", err)
    }
    payload := base64.StdEncoding.EncodeToString(pngBuf.Bytes())

    var b strings.Builder
    b.WriteString(ClearKittyImage(placement.ID))
    b.WriteString(fmt.Sprintf("\x1b[%d;%dH", placement.Y, placement.X))

    if len(payload) <= kittyMaxChunkSize {
        // Single chunk — no m flag needed (default m=0 = complete message).
        b.WriteString(fmt.Sprintf("\x1b_Ga=T,t=d,f=100,i=%d,c=%d,r=%d,z=1;",
            placement.ID, placement.Width, placement.Height))
        b.WriteString(payload)
        b.WriteString("\x1b\\")
    } else {
        // Chunked transmission: first chunk has m=1 (more data follows),
        // middle chunks have m=1, last chunk has m=0 (sequence complete).
        b.WriteString(fmt.Sprintf("\x1b_Ga=T,t=d,f=100,i=%d,c=%d,r=%d,z=1,m=1;",
            placement.ID, placement.Width, placement.Height))
        b.WriteString(payload[:kittyMaxChunkSize])
        b.WriteString("\x1b\\")

        remaining := payload[kittyMaxChunkSize:]
        for len(remaining) > kittyMaxChunkSize {
            b.WriteString("\x1b_Gm=1;")
            b.WriteString(remaining[:kittyMaxChunkSize])
            b.WriteString("\x1b\\")
            remaining = remaining[kittyMaxChunkSize:]
        }

        // Last chunk: m=0 signals the sequence is complete.
        b.WriteString("\x1b_Gm=0;")
        b.WriteString(remaining)
        b.WriteString("\x1b\\")
    }

    return b.String(), nil
}
```

#### 分块后的传输序列示例

一个 base64 payload 为 10000 字节的图片，分成 3 个 chunk：

```
\x1b_Ga=d,d=I,i=1\x1b\                                        ← 删除旧图片
\x1b[3;1H                                                       ← 光标定位
\x1b_Ga=T,t=d,f=100,i=1,c=40,r=20,z=1,m=1;<4096 bytes>\x1b\   ← 第一块，m=1
\x1b_Gm=1;<4096 bytes>\x1b\                                     ← 中间块，m=1
\x1b_Gm=0;<1808 bytes>\x1b\                                     ← 最后一块，m=0
```

kitty 收到 `m=0` 后才拼接所有 chunk、解码 PNG、显示图片。

#### 为什么分块能防止 base64 泄漏

分块前：一个 200KB 的 APC 作为单条消息写入 TTY。kitty 的 APC
解析器需要缓冲整个 200KB payload，此时如果有 SGR 序列紧跟其后
写入同一个 TTY，解析器可能将 SGR 字节误认为 APC payload 的
一部分，或者 APC 的 `\x1b\\` 终止符被 SGR 序列干扰，导致 payload
截断、base64 泄漏为明文。

分块后：每个 APC chunk 最多 4096 字节，kitty 可以快速处理完每个
chunk 并缓冲。即使 SGR 序列紧跟在 chunk 后到达，前一个 chunk 已经
被正确解析和缓冲，不会泄漏。kitty 规范要求客户端在发送完所有
chunk 之前不发送其他 graphics 转义码，分块传输天然将 APC payload
与后续 SGR 输出隔离开。

### 修复二：指纹去重避免每 tick 重复 PNG 编码

`internal/ui/app.go` 新增 `lastKittyFingerprint` 字段和
`kittyCoverFingerprint()` 方法：

```go
// App struct 新增字段
type App struct {
    // ...
    lastKittyCover      string
    kittyCoverDrawn     bool
    lastKittyFingerprint string  // 新增
    // ...
}

// kittyCoverFingerprint returns a lightweight string that captures all
// state affecting the kitty cover overlay. When this string is unchanged,
// the expensive renderKittyCoverOverlay() (PNG encode + base64) can be
// skipped.
func (a *App) kittyCoverFingerprint() string {
    return fmt.Sprintf("%d|%d|%p|%d|%d|%d|%d",
        a.leftContent,
        a.coverScale,
        a.coverImage,
        a.cellPixelW,
        a.cellPixelH,
        a.leftPaneWidth(),
        a.bodyHeight(),
    )
}
```

`kittyCoverCmd()` 在调用昂贵的 `renderKittyCoverOverlay()` 之前
先比较指纹：

```go
func (a *App) kittyCoverCmd() tea.Cmd {
    // Fast path: if nothing affecting the kitty overlay has changed since
    // the last render, skip the expensive renderKittyCoverOverlay() entirely.
    fp := a.kittyCoverFingerprint()
    if fp == a.lastKittyFingerprint {
        return nil
    }

    seq := a.renderKittyCoverOverlay()
    a.lastKittyFingerprint = fp
    if seq == "" {
        return nil
    }
    isDraw := strings.Contains(seq, "\x1b_Ga=T")
    if isDraw && a.kittyCoverDrawn && seq == a.lastKittyCover {
        return nil
    }
    if !isDraw && !a.kittyCoverDrawn && seq == a.lastKittyCover {
        return nil
    }
    a.lastKittyCover = seq
    a.kittyCoverDrawn = isDraw
    return tea.Raw(seq)
}
```

#### 指纹包含哪些状态

指纹字符串由以下字段拼接：

| 字段 | 变化触发场景 |
|------|-------------|
| `a.leftContent` | 按 `v` 切换左栏模式（封面+歌词/纯封面/纯歌词） |
| `a.coverScale` | 按 `c` 切换 fit/stretch |
| `a.coverImage`（指针） | 切歌（`loadCurrentCover()` 加载新图片） |
| `a.cellPixelW` | 收到 `CellSizeEvent`（终端字体变化） |
| `a.cellPixelH` | 同上 |
| `a.leftPaneWidth()` | 窗口 resize |
| `a.bodyHeight()` | 窗口 resize |

使用 `coverImage` 的指针值（`%p`）而非图片内容哈希，因为切歌时
`loadCurrentCover()` 总是创建新的 `image.Image`，指针变化即代表
图片可能变化。同一首歌的图片指针不变，指纹不变，正确跳过。

#### 性能对比

| 场景 | 修复前 | 修复后 |
|------|--------|--------|
| tick 无变化 | `renderKittyCoverOverlay()` + PNG 编码 + base64（20–50ms） | 字符串比较（<1μs） |
| 按 v/c 切换 | 同上 | 同上（指纹变化，正常渲染） |
| 切歌 | 同上 | 同上（指针变化，正常渲染） |

事件循环不再被 PNG 编码阻塞，按键消息可以即时处理。

#### 指纹重置点

以下三处重置 `lastKittyFingerprint = ""`，确保下一次 `kittyCoverCmd()`
不会因指纹相同而跳过渲染：

1. `clearScreenAndKittyCoverCmd()` — 按 `v`/`c` 时调用
2. `Update(uv.CellSizeEvent)` — 收到终端单元格尺寸变化
3. （`kittyCoverCmd()` 自身第一次调用时 `lastKittyFingerprint` 为零值 `""`，
   与指纹比较必然不等，触发渲染）

### 修复三：kitty 协议跳过 ClearScreen

kitty 协议下 `clearScreenAndKittyCoverCmd()` 返回 nil，不调
`tea.ClearScreen()`：

```go
func (a *App) clearScreenAndKittyCoverCmd() tea.Cmd {
    a.lastKittyCover = ""
    a.kittyCoverDrawn = false
    a.lastKittyFingerprint = ""
    if a.coverProtocol == cover.ProtocolKitty {
        return nil
    }
    return tea.Sequence(func() tea.Msg { return tea.ClearScreen() }, a.kittyCoverCmd())
}
```

`ClearScreen` 会让渲染器走 `clearUpdate()` 全量重绘路径，把
`View()` 的每个 cell（包括封面区域的空白 cell）写到终端。
kitty 的虚拟图片浮在文本层上面，全量重绘的空白 cell 会覆盖图片
像素，导致图片消失。跳过 `ClearScreen` 后，视图切换由 diff 引擎
增量更新，下一个 tick 的 `kittyCoverCmd()` 重绘图片。

分块传输确保即使 `kittyCoverCmd()` 产生的 APC 与 diff 输出在
同一个 renderer flush 中写入，base64 也不会泄漏。

### 修复四：查询真实终端单元格像素尺寸

终端支持 `\x1b[16t`（XTWINOPS 16）查询单元格像素尺寸，响应格式
`\x1b[6;<height>;<width>t`。ultraviolet 自动解析为
`CellSizeEvent{Width, Height}`。

#### 启动时查询

```go
func (a *App) Init() tea.Cmd {
    return tea.Batch(
        tickCmd(),
        tea.Raw(ansi.WindowOp(ansi.RequestCellSizeWinOp)), // \x1b[16t
    )
}
```

#### 处理响应

```go
case uv.CellSizeEvent:
    a.cellPixelW = msg.Width
    a.cellPixelH = msg.Height
    a.lastKittyCover = ""
    a.kittyCoverDrawn = false
    a.lastKittyFingerprint = ""
    return a, nil
```

#### 传递给封面渲染器

halfblock：
```go
cover.RenderHalfBlockWithScale(a.coverImage, w, h, a.coverScaleMode(),
    a.cellPixelW, a.cellPixelH)
```

kitty：
```go
cover.RenderKitty(a.coverImage, cover.KittyPlacement{
    ID: kittyImageID, X: x, Y: y,
    Width: coverW, Height: h,
    Scale: a.coverScaleMode(),
    CellW: a.cellPixelW,
    CellH: a.cellPixelH,
})
```

#### coverDrawSize 重写

```go
// internal/cover/cover.go coverDrawSize()
if cellW <= 0 { cellW = kittyCellPixelWidth }  // 默认 10
if cellH <= 0 { cellH = kittyCellPixelHeight }  // 默认 20
availW := width * cellW     // 可用像素宽度
availH := height * cellH    // 可用像素高度
pxW := availW
pxH := imgH * pxW / imgW    // 按图片宽高比算像素高度
if pxH > availH {           // 超出可用高度则按高度限制
    pxH = availH
    pxW = imgW * pxH / imgH
}
cellsW := pxW / cellW
cellsH := (pxH + cellH - 1) / cellH  // 向上取整
```

#### imageCanvas 用真实尺寸

```go
// internal/cover/protocol.go imageCanvas()
if cellW <= 0 { cellW = kittyCellPixelWidth }
if cellH <= 0 { cellH = kittyCellPixelHeight }
canvasW := width * cellW    // 用真实 cellW 而非硬编码 10
canvasH := height * cellH   // 用真实 cellH 而非硬编码 20
```

#### 终端不响应时的回退

`CellSizeEvent` 不到达时 `cellPixelW/H` 保持零值，`if cellW <= 0`
回退到默认 `10×20`，行为与修复前一致。tmux 下 `SelectProtocol`
返回 halfblock，不受影响。

## 失败方案回顾

### 失败方案一：错误的分块格式（第一版）

第一版分块实现有三个错误，导致 kitty 完全不显示图片：

```go
// 错误实现
b.WriteString(fmt.Sprintf("\x1b_Ga=T,t=d,f=100,i=%d,c=%d,r=%d,z=1;",
    placement.ID, placement.Width, placement.Height))  // ← 缺少 m=1
b.WriteString(payload[:firstSize])
b.WriteString("\x1b\\")

for len(remaining) > 0 {
    b.WriteString(fmt.Sprintf("\x1b_Gm=1,i=%d;", placement.ID))  // ← i=ID 不应出现
    // ...
    // ← 缺少 m=0 终止符
}
```

**错误一：第一个 chunk 缺少 `m=1`。** kitty 规范规定 `m` 默认为 `0`
（完整消息）。不加 `m=1` 时 kitty 把前 4096 字节当作整个 payload，
解码截断的 PNG 失败，丢弃图片。

**错误二：没有 `m=0` 终止符。** kitty 规范规定「在收到完整序列之前
不显示任何内容」。没有 `m=0`，序列永远不完整，图片永远不显示。

**错误三：后续 chunk 包含 `i=ID`。** kitty 规范规定后续 chunk 只有
`m` 字段（可选 `q`、`a=f`）。chunk 关联靠发送顺序，不靠 ID。多余的
`i=ID` 字段不符合规范。

正确格式参见上文「修复一」。

### 失败方案二：tickMsg 中粗暴跳过 kittyCoverCmd

为解决性能卡顿，第一版尝试在 `tickMsg` 中检查 `lastKittyCover != ""`
来跳过 `kittyCoverCmd()`：

```go
// 错误实现
if a.coverProtocol == cover.ProtocolKitty && a.lastKittyCover != "" {
    return a, tickCmd()  // 跳过 kittyCoverCmd()
}
```

**问题：** `lastKittyCover` 存储的是上次输出的序列（可能是清除命令
`\x1b_Ga=d,d=I,i=1\x1b\\`，非空）。切歌后 `coverImage` 变了，但
`lastKittyCover` 仍是清除命令（非空），所以 tick 永远跳过
`kittyCoverCmd()`，图片不会被绘制。

**正确做法：** 用指纹（`kittyCoverFingerprint()`）在 `kittyCoverCmd()`
内部跳过，而不是在 `tickMsg` 层跳过。指纹包含 `coverImage` 指针，
切歌后指针变化，指纹变化，`kittyCoverCmd()` 正常调用
`renderKittyCoverOverlay()`。

### 失败方案三：用 tea.Tick 延迟重绘

曾尝试在 `clearScreenAndKittyCoverCmd()` 中用 `tea.Tick(50ms)` 延迟
kitty 重绘，试图让视图 diff 先落地：

```go
// 错误实现
if a.coverProtocol == cover.ProtocolKitty {
    return tea.Tick(tickInterval, func(time.Time) tea.Msg {
        return kittyRedrawMsg{}
    })
}
```

**问题：** 这既没有解决 base64 泄漏（分块才是正解），又增加了
额外的消息和延迟。正确做法是返回 nil + 分块传输。

## 时序分析

### 按 v 键切换的完整时序

```
t=0ms   KeyMsg(v) 处理
        ├─ toggleLeftContent() 改 a.leftContent
        ├─ clearScreenAndKittyCoverCmd()
        │  ├─ lastKittyCover = ""
        │  ├─ kittyCoverDrawn = false
        │  ├─ lastKittyFingerprint = ""
        │  └─ return nil (kitty 不 ClearScreen)
        └─ p.render(model) 存储 newView (左栏内容变了)

t=33ms  渲染器 tick (30fps)
        ├─ p.flush() — outputBuf 空, 无输出
        └─ p.renderer.flush(false)
           ├─ diff oldView vs newView → 左栏变化区域重画
           └─ lastView = newView

t=50ms  tickMsg 处理
        ├─ pollEngine()
        ├─ kittyCoverCmd()
        │  ├─ fp = kittyCoverFingerprint()  ← leftContent 变了, fp 不同
        │  ├─ lastKittyFingerprint = fp
        │  ├─ renderKittyCoverOverlay() → drawSeq (分块 APC)
        │  └─ tea.Raw(drawSeq) → RawMsg
        └─ p.render(model) 存储 sameView

t=83ms  渲染器 tick
        ├─ p.flush() — drawSeq 写入 TTY (分块 APC, 每块 ≤4096 字节)
        │  → kitty 逐块接收, 收到 m=0 后显示图片
        └─ p.renderer.flush(false)
           ├─ diff oldView vs sameView → 无变化
           └─ 不写任何 cell → 图片不被覆盖 ✓
```

### tick 无变化时的时序（播放中）

```
t=Nms   tickMsg 处理
        ├─ pollEngine()
        └─ kittyCoverCmd()
           ├─ fp = kittyCoverFingerprint()  ← 所有字段都没变
           ├─ fp == lastKittyFingerprint
           └─ return nil  ← 跳过 renderKittyCoverOverlay(), 零开销

t=N+33ms 渲染器 tick
         ├─ p.flush() — outputBuf 空, 无输出
         └─ p.renderer.flush(false) — view 无变化, 无输出
```

事件循环几乎零阻塞，按键即时响应。

## halfblock 不变

halfblock 协议仍走 `ClearScreen` 路径。这是必要的——halfblock
的封面图是文本 cell，CJK 宽字符 diff 引擎的 SGR 偏差问题
（见 `cjk-render-shift-bug.md`）在 halfblock 封面和歌词共存时
仍然存在。`ClearScreen` 绕过 diff，整帧重画，消除偏差。

指纹去重对 halfblock 无影响——halfblock 的 `renderCoverPane()`
直接在 `View()` 中返回文本，不经过 `kittyCoverCmd()`。

## 为什么没有用 Unicode 占位符方案

kitty 图形协议的 `U=1`（Unicode placeholder）模式把图片引用
编码进文本网格——每个 cell 放一个 `U+10EEEE` 占位字符，终端
识别后在该位置渲染图片。这样图片成为 `View()` 的一部分，
diff 引擎正常处理，`ClearScreen` 全量重绘也会重画占位符。

这是理论上更正确的方案，但需要重写整个 kitty 渲染策略
（从 `a=T` 虚拟放置改为 `a=p,U=1` 占位符放置），是一个大重构。
当前修复用分块传输 + 指纹去重解决了泄漏和性能问题，是最小 diff
的根因修复。

## 涉及的源码位置

### 项目代码

- `internal/cover/protocol.go`
  - `kittyMaxChunkSize = 4096`（:24）：分块大小常量
  - `RenderKitty()`（:74）：分块传输逻辑，小 payload 单块、大 payload m=1/m=0
  - `imageCanvas()`（:121）：用真实 `cellW, cellH` 算画布尺寸
  - `KittyPlacement`（:47）：`CellW, CellH` 字段
- `internal/cover/cover.go`
  - `RenderHalfBlockWithScale()`（:121）：`cellW, cellH` 参数
  - `coverDrawSize()`（:159）：用真实像素算宽高比
- `internal/ui/app.go`
  - `App.lastKittyFingerprint`（:114）：指纹字段
  - `kittyCoverFingerprint()`（:618）：生成状态指纹
  - `kittyCoverCmd()`（:633）：指纹检查 + 去重逻辑
  - `clearScreenAndKittyCoverCmd()`（:660）：kitty 跳过 ClearScreen，重置指纹
  - `tickMsg` 分支（:314）：每 50ms 调 `kittyCoverCmd()`（指纹去重后零开销）
  - `Init()`（:232）：启动时发 `\x1b[16t` 查询单元格像素尺寸
  - `Update()` `uv.CellSizeEvent` 分支（:330）：存储真实尺寸，重置指纹
  - `renderCoverPane()`（:985）：halfblock 传 `cellPixelW/H`
  - `renderKittyCoverOverlay()`（:998）：kitty 传 `CellW/CellH`

### bubbletea v2（charm.land/bubbletea/v2@v2.0.8）

- `tea.go`
  - `eventLoop()`（:743）：处理 `RawMsg` → `p.execute()`，
    `clearScreenMsg` → `renderer.clearScreen()`
  - `execSequenceMsg()`（:892）：Sequence 逐条执行，`p.Send(msg)`
  - `startRenderer()`（:1393）：渲染器 tick 循环，`p.flush()` 后
    `p.renderer.flush(false)`；帧率由 `WithFPS(30)` 设为 33ms
  - `execute()`（:1214）：写 `p.outputBuf`
  - `flush()`（:1221）：刷 `p.outputBuf` 到 TTY
- `screen.go` — `ClearScreen()`（:20）：返回 `clearScreenMsg{}`
- `raw.go` — `Raw()`（:33）：返回 `RawMsg{r}`
- `cursed_renderer.go`
  - `clearScreen()`（:634）：`scr.MoveTo(0,0)` + `scr.Erase()`
  - `flush()`（:257）：diff `lastView` vs `view`，写 `s.buf` 到 TTY

### ultraviolet

- `decoder.go:647`：解析 `\x1b[6;<h>;<w>t` → `CellSizeEvent{Width, Height}`
- `terminal_renderer.go`：`Erase()` 设 `s.clear = true`，
  `Render()` 检测 `s.clear` 走全量重绘

### charmbracelet/x/ansi

- `winop.go:27`：`RequestCellSizeWinOp = 16`（`\x1b[16t` 常量）

## 测试

### 分块传输

- `TestKittyRenderChunksLargePayload`：验证大 payload 分块——
  第一块有 `m=1`，中间块只有 `m=1`（无 `i=ID`），最后一块有 `m=0`，
  拼接后的 payload 是有效 base64 PNG，每块 ≤ 4096 字节。
- `TestKittyRenderSmallPayloadNotChunked`：验证小 payload 不分块——
  不含 `m=1`/`m=0`，使用标准传输头。
- `TestKittyRenderSequenceContainsDeletePositionAndPayload`：验证
  传输序列包含删除命令、光标定位、传输头，payload 是有效 base64。

### 指纹去重

- `TestKittyCoverCmdSkipsRenderWhenFingerprintUnchanged`：验证
  指纹不变时 `kittyCoverCmd()` 返回 nil（跳过渲染），coverImage
  变化后指纹变化、重新渲染。
- `TestKittyToggleResetsDedupForNextTickRedraw`：验证
  `clearScreenAndKittyCoverCmd()` 重置指纹，下一个 `kittyCoverCmd()`
  正常渲染。

### ClearScreen 跳过

- `TestHalfBlockToggleStillClearsScreen`：验证 halfblock 仍返回
  `clearScreenMsg`。
- `TestKittyLyricChangeDoesNotClearScreen`：验证 kitty 歌词变化
  不清屏（走 `kittyCoverCmd()` 路径）。

### 单元格像素尺寸

- `TestCoverDrawSizeDefaultMatchesOldBehavior`：验证 `cellW=0, cellH=0`
  时与旧公式产出一致。
- `TestCoverDrawSizeNonSquareCellProducesCorrectAspect`：验证
  `cellW=9, cellH=20` 时正方形图片显示比例在 ±5% 以内。
- `TestKittyCanvasUsesNonDefaultCellSize`：验证 `imageCanvas`
  用 `cellW=9` 时画布宽度 = `width * 9`。
- `TestCellSizeEventUpdatesDimensionsAndResetsDedup`：验证
  `uv.CellSizeEvent` 更新尺寸并重置指纹。

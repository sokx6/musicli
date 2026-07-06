# CJK 逐字高亮字符偏移 Bug：根因分析与修复

## 问题现象

播放带逐字时间戳的日文歌词时，高亮切换到某些字符会导致同一行其他字符
向左偏移或消失。例如 `ツギハギだらけの君との時間も` 在高亮「と」的瞬间，
「君」向左偏移，「の」被覆盖消失。高亮完「と」后有时会自行恢复，有时
不会。该 bug 稳定可复现，每次播放都在同一位置出现。

## 环境

- 渲染框架：charm.land/bubbletea/v2 v2.0.8
- 底层渲染器：ultraviolet v0.0.0-20260703014108-f5a850f9c2b7
- 终端模式：原本为 inline 模式（无 alt screen）
- 歌词格式：SPL / word-timed LRC，每个 CJK 字符为一个 word

## 根因总结

bubbletea v2 的 `cursedRenderer` 使用 ultraviolet 的
`TerminalRenderer` 做 cell 级增量 diff。在 inline 模式下，光标移动采用
**相对定位**（CursorForward / CursorBackward），其正确性依赖 renderer
记录的光标位置 `s.cur.X` 与终端实际光标位置始终一致。

CJK 宽字符占 2 列，其中第二列为 width=0 的 placeholder cell。当 diff
引擎在 `putRange` 中跳过未变化的 placeholder 时，以及在行末处理
phantom cell（pending wrap）时，renderer 记录的光标位置可能与终端实际
位置产生微小偏差。这些偏差在大部分帧中不可见（因为歌词行未变化时
`transformLine` 直接返回，不触发光标移动），但在高亮词切换、歌词行需要
重画时，相对移动带着积累的偏差走，字符被画到错误的列上。

`reanchorWideLine` 本应用绝对定位修正偏差，但它用 `defer` 仅在每行
**结束时**执行一次，无法修正行中间 `putRange` 跳转产生的偏差；且它锚
回的是 renderer 记录的位置，若该位置本身有误则修正无效。

## 渲染管线详解

### 1. View() 返回整帧字符串

App 的 `View()` 方法用 lipgloss 拼装整屏内容，返回一个带 ANSI 转义码
的字符串。对于歌词高亮行，`renderCurrentLyricLine` 产出三段 SGR：

```
M ツギハギだらけの A 君 M との時間も R 空空空空
└─── muted ───────┘ └accent┘ └─ muted ─┘ └pad┘
```

其中 M = `\033[38;2;86;95;137m`（muted 色 #565f89），
A = `\033[38;2;122;162;247m`（accent 色 #7aa2f7），
R = `\033[0m`（reset）。

### 2. StyledString.Draw() 拆成 cell 网格

`cursedRenderer.flush()` 调 `StyledString.Draw(cellbuf, bounds)`（ultraviolet
`styled.go:50`），内部调 `printString`（`styled.go:101`）用 ANSI parser
逐字符拆解字符串为 cell 网格。

**Cell 结构**（`cell.go:15`）：

```go
type Cell struct {
    Content string  // 字符内容（一个 grapheme cluster）
    Style   Style   // 颜色/属性
    Link    Link    // 超链接
    Width   int     // 显示宽度（CJK=2，ASCII=1，placeholder=0）
}
```

CJK 字符（如「ツ」）width=2，占两列：cell N 存字符，cell N+1 是
width=0 的 placeholder，标记「此列属于左边的宽字符，不可单独写入」。

ASCII 字符（如空格）width=1，占一列。

**帧 A（高亮「君」）的 cell 网格**（假设内容区 30 列）：

```
位置  内容   宽度  样式      说明
──────────────────────────────────────────
 0    ツ     2    muted     ┐ CJK 宽字符
 1    (空)   0    muted     ┘ placeholder（width=0）
 2    ギ     2    muted     ┐
 3    (空)   0    muted     ┘
 4    ハ     2    muted     ┐
 5    (空)   0    muted     ┘
 6    ギ     2    muted     ┐
 7    (空)   0    muted     ┘
 8    だ     2    muted     ┐
 9    (空)   0    muted     ┘
10    ら     2    muted     ┐
11    (空)   0    muted     ┘
12    け     2    muted     ┐
13    (空)   0    muted     ┘
14    の     2    muted     ┐
15    (空)   0    muted     ┘
16    君     2    accent    ┐ ← 高亮段开始
17    (空)   0    accent    ┘ ← placeholder 也带 accent
18    と     2    muted     ┐
19    (空)   0    muted     ┘
20    の     2    muted     ┐
21    (空)   0    muted     ┘
22    時     2    muted     ┐
23    (空)   0    muted     ┘
24    間     2    muted     ┐
25    (空)   0    muted     ┘
26    も     2    muted     ┐
27    (空)   0    muted     ┘
28    ' '    1    none      ← ASCII 空格填充
29    ' '    1    none      ← ASCII 空格填充
```

**帧 B（高亮「と」）的差异**：

```
位置  帧A样式   帧B样式   变化
──────────────────────────────
16    accent    muted    ← 变
17    accent    muted    ← 变
18    muted     accent   ← 变
19    muted     accent   ← 变
```

4 个 cell 的字符内容不变，只有样式（颜色）变了。accent 段从位置
16-17 右移到 18-19，移动了 2 列（一个 CJK 字符宽）。

### 3. TerminalRenderer.transformLine() 做 cell diff

`cursedRenderer.flush()` 把 cell 网格画入 `cellbuf` 后，调
`s.scr.Render(s.cellbuf.RenderBuffer)`（`cursed_renderer.go:461`）。
这进入 `TerminalRenderer.Render()`（`terminal_renderer.go:1177`），
对每个 touched 的行调 `transformLine`。

`transformLine`（`terminal_renderer.go:814`）的工作：对比旧行
（`curbuf`，上一帧）和新行（`newbuf`，当前帧），找出变化区域，用最少
的 ANSI 序列把终端从旧状态转到新状态。

#### 步骤 1：找 firstCell（第一个变化的 cell）

```go
for firstCell < newbuf.Width() &&
    cellEqual(oldLine.At(firstCell), newLine.At(firstCell)) {
    firstCell++
}
```

从位置 0 向右扫，跳过 `cellEqual` 返回 true 的 cell。位置 0-15 全部
相同，位置 16 样式从 accent 变 muted，`cellEqual` 返回 false。

```
firstCell = 16
```

`cellEqual`（`terminal_renderer.go:478`）比较 cell 的 Width、Content、
Style、Link 全部相等。

#### 步骤 2：找 nLastCell（最后一个变化的 cell）

从行末向左扫，跳过与 blank 相同的 cell（trailing spaces），再跳过
old/new 相同的 cell。位置 28-29 是空格（`canClearWith` = true），
27 是 placeholder 不等于空格，停下。继续向左，27→20 全部相同，位置
19 样式变化，停住。

```
nLastCell = 20
```

#### 步骤 3：move + putRange

```go
s.move(newbuf, firstCell, y)                              // 移光标到 16
s.putRange(newbuf, oldLine, newLine, y, firstCell, nLastCell)  // 重画 16-20
```

#### 步骤 4：putRange 内部

`putRange`（`terminal_renderer.go:704`）判断这段是否值得做跳过优化。
本例 5 个 cell，不满足跳过条件（`5 > 5` 为 false），走 `emitRange`
逐个输出。

#### 步骤 5：emitRange 逐个输出 cell

```
cell 16 (君, muted, w=2):
  → updatePen: 若 pen 已是 muted 则不换色
  → buf.WriteString("君")
  → s.cur.X += 2  →  光标 16 → 18

cell 17 (placeholder, muted, w=0):
  → putAttrCell 检测 width==0，直接 return，不写任何内容
  → s.cur.X 不变，仍为 18

cell 18 (と, accent, w=2):
  → updatePen: pen 从 muted 变 accent，写 SGR diff
  → buf.WriteString("と")
  → s.cur.X += 2  →  光标 18 → 20

cell 19 (placeholder, accent, w=0):
  → width==0，return，不写
  → s.cur.X 不变，仍为 20

cell 20 (の, muted, w=2):
  → updatePen: pen 从 accent 变 muted，写 SGR diff
  → buf.WriteString("の")
  → s.cur.X += 2  →  光标 20 → 22
```

最终输出字节序列：

```
[移动光标到位置16] SGR_muted 君 SGR_accent と SGR_muted の
```

## 为什么会出错

上面的 trace 在光标移动连续时没有问题。**问题出在「移动光标到位置 16」
这一步。**

### inline 模式下光标移动是相对的

app 原本跑在 inline 模式（未开 alt screen），`tRelativeCursor` 标志
开启。`moveCursor()`（`terminal_renderer.go:1516`）不用绝对定位
`\033[y;xH`，而用相对移动：

```
CursorForward(n)  = \033[nC   前进 n 列
CursorBackward(n) = \033[nD   后退 n 列
```

其中 `n = 目标位置 - s.cur.X`。`s.cur.X` 是 renderer **自己记录的**
光标位置。如果它和终端实际光标位置不一致，n 就算错了。

### s.cur.X 如何产生偏差

renderer 在 `putAttrCell`（`terminal_renderer.go:516`）中更新光标：

```go
s.cur.X += cellWidth
if s.cur.X >= newbuf.Width() {
    s.atPhantom = true   // 到行末，进入 pending wrap
}
```

CJK 字符 width=2，`cur.X += 2`。placeholder width=0，`putAttrCell`
直接 return，`cur.X` 不动。逻辑上自洽，但实际运行中 `s.cur.X` 可能
与终端实际位置产生偏差，原因包括：

1. **行末宽字符与 phantom cell**：当光标到达行末最后一列时，终端进入
   pending wrap（phantom cell）状态。不同终端对此的处理行为不一致，
   renderer 的 `atPhantom` 标记可能与终端实际状态不同步。

2. **placeholder 被部分覆盖**：`Line.Set()`（`buffer.go:41`）在新 cell
   覆盖宽字符的第一列时，会把整个宽字符的所有列清成空格。这个操作改
   cell buffer 但不产生终端输出，下一帧 diff 对比时可能触发意外重画。

3. **多行处理的状态积累**：整帧有 24 行（top bar、歌词、曲目列表、
   播放栏），diff 引擎从上到下逐行处理。每行处理完光标移到下一行。
   如果任何一行的处理让 `s.cur.X` 偏差 1 列，后面所有行的相对移动
   都基于错误起点。

### 为什么只在换词时可见

每 100ms tick 一次，但大部分 tick 里歌词行不变（同一个词高亮），
`cellEqual` 全部返回 true，`transformLine` 直接 return，不触发光标
移动。偏差无法被观察到。

只有高亮词切换时（君→と），4 个 cell 样式变化，diff 引擎需要移动
光标到位置 16 并重画。此时相对移动带着积累的偏差走，字符画到错误
列，「の」被覆盖消失。

### reanchorWideLine 为什么没救回来

```go
// terminal_renderer.go:1017
func (s *TerminalRenderer) reanchorWideLine(newbuf *RenderBuffer) {
    if !s.lineHadWide || s.flags.Contains(tGraphemeWidth) {
        return
    }
    s.lineHadWide = false
    if s.atPhantom || s.cur.X < 0 || s.cur.X >= newbuf.Width() {
        return
    }
    _, _ = s.buf.WriteString(ansi.CursorHorizontalAbsolute(s.cur.X + 1))
}
```

它用 `defer` 在 `transformLine` **结束时**发绝对定位 `CHA`
（`\033[nG`），把光标锚回 `s.cur.X` 位置。问题：

1. 只在行末执行一次，行中间的 `putRange` 跳转产生的偏差无法修正。
2. 锚回的是 `s.cur.X`（renderer 记录的位置），若该位置本身有误，
   锚回去仍是错的。
3. 在 grapheme width 模式下直接跳过（`tGraphemeWidth` 标志开启时
   return），但 bubbletea v2 的 `setWidthMethod` 只设 `cellbuf.Method`，
   不调 `s.scr.SetGraphemeWidth(true)`，所以 `tGraphemeWidth` 标志
   实际未开启，`reanchorWideLine` 会执行——但只在行末，救不了行中。

## 修复方案

### 第一层：开启 alt screen 模式

```go
// internal/ui/app.go View()
v := tea.NewView(frame)
v.AltScreen = true
```

`AltScreen = true` 触发 `enterAltScreen()`，设置：
- `tFullscreen = true`
- `tRelativeCursor = false`

`moveCursor()` 对长距离（>7 列，由 `notLocal` 判断）改用绝对定位
`CursorPosition(x+1, y+1)`（`\033[y;xH`），直接跳到指定坐标，不依赖
`s.cur.X` 的准确性。

但短距离（≤7 列，`notLocal` 阈值）仍走相对移动。歌词 diff 范围
4-6 列，在此范围内，光靠 alt screen 不足以消除偏差。

### 第二层：高亮换词时强制全屏重绘

```go
// internal/ui/app.go Update() tickMsg 分支
case tickMsg:
    prevWord := a.lastLyricWord
    a.pollEngine()
    newWord := a.currentLyricWordIndex()
    a.lastLyricWord = newWord
    if newWord != prevWord {
        return a, tea.Batch(tickCmd(), func() tea.Msg { return tea.ClearScreen() })
    }
    return a, tickCmd()
```

`tea.ClearScreen()` 产生 `clearScreenMsg`，renderer 调 `clearScreen()`
（`cursed_renderer.go:634`）：

```go
func (s *cursedRenderer) clearScreen() {
    s.scr.MoveTo(0, 0)
    s.scr.Erase()   // 设 s.clear = true
}
```

下次 flush 时 `Render()` 检测 `s.clear == true`，走 `clearUpdate()`
（`terminal_renderer.go:1120`）：

```go
func (s *TerminalRenderer) clearUpdate(newbuf *RenderBuffer) {
    blank := s.clearBlank()
    s.clearScreen(blank)   // 发 \033[2J，把 curbuf 整个 Fill 成空白
    for i := 0; i < nonEmpty; i++ {
        s.transformLine(newbuf, i)
    }
}
```

`curbuf`（旧帧）被整个填成空白 cell。`transformLine` 对比时，old
cell 全是空白，new cell 是歌词字符，`cellEqual` **全部返回 false**。
不存在「跳过相同 cell」的优化，整行从位置 0 开始逐个 cell 输出，光标
连续前进，不依赖 `s.cur.X` 的历史状态。

**等价于每帧都是首帧**，没有积累偏差的机会。

### 为什么不闪烁

整屏重绘理论上先擦后画，有闪烁风险。但 bubbletea v2 启动时探测终端
能力（`tea.go:1113`）：

```go
p.execute(ansi.RequestModeSynchronizedOutput +
    ansi.RequestModeUnicodeCore)
```

若终端支持 synchronized output（mode 2026，kitty/wezterm/ghostty/
alacritty/tmux/xterm 等现代终端均支持），`cursedRenderer.syncdUpdates`
被设为 true。flush 时整帧包在 BSU/DSU 里（`cursed_renderer.go:528`）：

```
\033[?2026h        ← 开启 synchronized output
  <所有更新>       ← 终端缓存，不立即显示
\033[?2026l        ← 关闭，一次性刷新到屏幕
```

终端缓存整帧更新，最后原子性刷新，用户看到完整一帧，无中间态。

### 时机分析

renderer flush 在独立 goroutine 按 FPS 定时执行（`tea.go:1409`）：

```go
case <-p.ticker.C:
    _ = p.flush()
    _ = p.renderer.flush(false)
```

event loop 处理 `tickMsg` 后立刻返回 `clearScreen` 命令。
`clearScreenMsg` 在亚毫秒内进入 event loop，调 `clearScreen()` 设
`s.clear = true`。等下一次 flush（最多 33ms 后）触发时，`s.clear`
已为 true，走全屏重绘路径。clear 标志在 flush 前设好。

最坏情况：flush 在 tickMsg 处理后、clearScreenMsg 处理前触发，
此时走正常 diff 路径可能有一帧错位，但下一个 flush（33ms 后）因
`s.clear` 已设会全屏重绘修正。用户最多看到 33ms 的错位，人眼基本
不可察。

## 为什么之前 8 次尝试失败

| 尝试 | 做法 | 失败原因 |
|------|------|----------|
| 1 | `Styled()` 替换 `String()`，每段加 reset | diff 引擎算的是 cell 不是字符串，字符串层面改 SGR 结构对 cell 对比无影响 |
| 2 | 3 段 SGR → 2 段（played/unplayed） | 2 段 SGR 仍随词移动，diff 仍需跳转 |
| 3 | `\033[2K` 行首清行嵌入渲染字符串 | bubbletea 不透传，cell buffer 把 `\033[2K` 当普通字符处理 |
| 4 | 先算纯文本宽度再拼样式 | 宽度一致但 diff 仍按 cell 跳转，偏差依旧 |
| 5 | `➣` 箭头 + `█` 光标（music-cli 风格） | SGR 结构更复杂，偏差更严重 |
| 6 | `ClearScreen` 每 tick | inline 模式下 `clearBelow` 只清光标以下，相对光标 + 无 sync output，疯狂闪烁 |
| 7 | `ClearScreen` 词变化时 | 仍在 inline 模式，仍有瞬间空白闪烁，且 clear 路径与 alt screen 不同 |
| 8 | 回退到 `\033[2K` | 同尝试 3，不透传 |

**关键区别**：成功的修复是 **alt screen + ClearScreen 组合**。
- alt screen 提供绝对定位（长距离）和 synchronized output 基础设施
- ClearScreen 绕过 diff 引擎（`curbuf` 全填空白，`cellEqual` 全 false，
  整行重画）
- synchronized output 保证整帧原子刷新，无闪烁

两者缺一不可。单独 alt screen 无法解决短距离相对移动的偏差；单独
ClearScreen 在 inline 模式下闪烁且 clear 行为不同。

## 涉及的源码位置

### 项目代码

- `internal/ui/app.go`
  - `View()`：设置 `v.AltScreen = true`
  - `Update()` tickMsg 分支：高亮换词时发 `tea.ClearScreen()`
  - `currentLyricWordIndex()`：返回当前高亮词索引
  - `renderCurrentLyricLine()`：产出三段 SGR 的歌词行字符串

### bubbletea v2（charm.land/bubbletea/v2@v2.0.8）

- `cursed_renderer.go`
  - `flush()`（:257）：主渲染入口，调 `content.Draw` 和 `s.scr.Render`
  - `clearScreen()`（:634）：设 `s.scr.Erase()`，标记全屏重绘
- `tea.go`
  - `eventLoop()`（:743）：处理 `clearScreenMsg`，调 `renderer.clearScreen()`
  - `startRenderer()`（:1392）：启动 flush goroutine，按 FPS 定时 flush
  - `:1113`：启动时探测 synchronized output 和 Unicode core mode

### ultraviolet（github.com/charmbracelet/ultraviolet）

- `styled.go`
  - `printString()`（:101）：ANSI 字符串 → cell 网格
- `buffer.go`
  - `Line.Set()`（:41）：cell 写入，处理宽字符 placeholder 覆盖
  - `RenderBuffer.SetCell()`（:705）：带 touched 标记的 cell 写入
- `terminal_renderer.go`
  - `transformLine()`（:814）：逐行 diff 核心
  - `putRange()`（:704）：范围重画，跳过相同 cell
  - `emitRange()`（:622）：逐 cell 输出
  - `moveCursor()`（:1516）：光标移动，inline 相对 / alt 绝对
  - `reanchorWideLine()`（:1017）：行末绝对锚定（defer）
  - `clearUpdate()`（:1120）：全屏重绘路径
  - `notLocal()`（:1346）：判断是否用绝对定位（阈值 7 列）
- `cell.go`
  - `Cell.Equal()`（:55）：cell 相等比较
  - `isWidePlaceholder()`（:70）：width=0 的 placeholder 判断

## 附：等时间戳标点合并

修复字符偏移后发现的附带问题。歌词 `[00:01.718]：[00:01.718]い` 中
`：` 和 `い` 时间戳相同（1718ms）。原 `buildWords` 用
`tokens[i].ms >= tokens[i+1].ms` 跳过等时间戳 segment，导致 `：` 从
words 列表消失，高亮 `い` 时 `：` 不显示。

修复：等时间戳 segment 的 text 累积到下一个有实际时长的 word，合并为
一个 word。`：` 和 `い` 合并为 `：い`（1718-1886），高亮时一起 accent。

```go
// internal/lyrics/spl.go buildWords()
if tokens[i].ms == tokens[i+1].ms {
    carry += text   // 累积零时长 segment
    continue
}
words = append(words, Word{Text: carry + text, ...})
carry = ""
```

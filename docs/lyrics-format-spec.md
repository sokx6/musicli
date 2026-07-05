# 歌词格式规格文档

> 本文档汇总 musicli 需要支持的歌词格式规格，供开发参考。
> 信息来源：LDDC 源码、SPL 官方标准、Wikipedia LRC、AMLL 格式文档、Lyricify 文档。

## 归一化数据模型

所有格式解析后归一化为此模型：

```go
type Line struct {
    StartMs      int    // 行开始时间（毫秒）
    EndMs        int    // 行结束时间（显式或下一行 start）
    Words        []Word // 逐字时间戳，空则纯行级歌词
    Translation  string // 翻译文本
    Agent        string // 歌手（TTML ttm:agent, LRCv2 <s:>, LYS duet）
    IsBackground bool   // 背景人声（LYS prop 6-8, QRC/YRC 全括号行, TTML x-bg）
}

type Word struct {
    Text    string
    StartMs int
    EndMs   int
}

type Lyric struct {
    Lines []Line
    Tags  map[string]string // ti, ar, al, by, offset, ...
}
```

## 格式映射总表

| 格式 | Line.StartMs | Line.EndMs | Word.StartMs | Word.EndMs | 关键点 |
|---|---|---|---|---|---|
| **SPL** | 首 `[...]` | 末 `[...]` 或下行 start | ts 前的文本 | ts 后的文本 | `<>` = 非边界标记；翻译共享 ts；小数右补零 |
| LRC | `[mm:ss.xx]` | 下行 start | — | — | `offset` tag 全局偏移 |
| Enhanced LRC (A2) | `[mm:ss.xx]` | 下行 start | `<mm:ss.xx>` | 下个 `<>` | 百分秒 vs 毫秒 |
| YRC | `[start,dur]` | start+dur | `(start,dur,0)text` abs | start+dur | ts 先于 text，3 字段，纯文本 |
| QRC | `[start,dur]` | start+dur | `text(start,dur)` abs | start+dur | text 先于 ts，2 字段，需 3DES+zlib |
| KRC | `[start,dur]` | start+dur | **lineStart +** `<offset,dur,0>text` | +offset+dur | 相对偏移；需 XOR+zlib |
| LYS | 首字 start | 末字 end | `text(start,dur)` abs | start+dur | `[prop]` 编码 bg+duet |
| TTML | `p@begin/end` | `p@end` | `span@begin/end` | `span@end` | `itunes:timing=Word`；秒或时钟格式 |
| LRCv2 | — | — | `<mm:ss.xx>word</mm:ss.xx>` | — | **不是 YAML**（YAML 是 lrcget 的 Lyrics File）|

---

## 1. SPL（Salt Player Lyrics）— 首要格式

**标准来源**：https://moriafly.com/standards/spl.html（制定 2024-12-16，修订 2025-11-14）

SPL 是 LRC 的严格超集，行式文本，人可读。支持行级时间、显式/隐式行结束、重复行、翻译、逐字时间、延迟逐字标记。

### 1.1 时间戳

`[mm:ss.xx]` — 方括号包裹，冒号分隔分秒，点分隔秒和小数。

- 分：1-3 位（`1`、`02`、`103`）
- 秒：1-2 位（`1`、`02`、`13`）
- 小数：1-6 位（`1`、`02`、`130`、`450000`）
- **少于 3 位 = 省略末尾零**：`.1` → `.100` = 100ms，`.02` → `.020` = 20ms

毫秒计算：小数部分右补零到 ≥3 位，取前 3 位 → int。`min*60000 + sec*1000 + ms3`

示例：
- `[103:3.405]` = 103 分 3 秒 405ms
- `[3:12.5]` = 3 分 12 秒 500ms

### 1.2 歌词行

`[mm:ss.xx]<text>` — 时间戳后跟文本。

```
[05:20.22]你好椒盐音乐
```

### 1.3 显式行结束

内联末尾时间戳，或单独一行无文本的时间戳：

```
[05:20.22]你好椒盐音乐[05:21.22]      // 行 320220→321220 (1s)
```
或
```
[05:20.22]你好椒盐音乐
[05:21.22]
```

### 1.4 隐式行结束

无结束标签 → 行持续到下一行的开始：

```
[05:20.22]你好椒盐音乐        // 结束于 322220（下行 start）
[05:22.22]天天开心
```

### 1.5 重复行

多个前导时间戳折叠为同一文本重复：

```
[05:20.22][05:30.22]你好椒盐音乐
```
≡ 两行。**与逐字不兼容**（spec 限制）。

### 1.6 翻译（歌词翻译）

翻译行与主行**共享时间戳**，且必须在主行之后：

```
[05:20.22]你好椒盐音乐
[05:20.22]Hello Salt Player
```

省略时间戳形式（必须**紧随**主行）：

```
[05:20.22]你好椒盐音乐
Hello Salt Player
```

支持多行翻译（堆叠省略时间戳行）。

### 1.7 逐字歌词

行内时间戳分割文本为带时间的段。**首** `[...]` = 行开始 + 首字开始；**中间**时间戳 = 字界；**末** `[...]` = 行结束：

```
[05:20.22]你好[05:23.22]椒盐音乐[05:24.22]
```
- 你好：320220 → 323220（3s）
- 椒盐音乐：323220 → 324220（1s）

字时间戳**必须单调递增**，越界或非递增的标记被忽略。

### 1.8 延迟逐字标记

非首非末的字时间戳可用 `< >` 代替 `[ ]`，支持"行到达但首字未开始"：

```
[05:20.22]<05:21.22>你好<05:23.22>椒盐音乐[05:24.22]
```
行在 320220 显示，但"你好"直到 321220 才开始。（SPW 1.8 / Salt Player 11.0.0+ 支持）

### 1.9 完整示例

```
[103:3.405] // 103min 3s 405ms
[3:12.5]    // 3min 12s 500ms
[05:20.22]你好椒盐音乐
[05:20.22]你好椒盐音乐[05:21.22]
[05:20.22]你好椒盐音乐
[05:21.22]
[05:20.22][05:30.22]你好椒盐音乐
[05:20.22]你好椒盐音乐
Hello Salt Player
[05:20.22]你好椒盐音乐
Hello Salt Player
こんにちは Salt Player
[05:20.22]你好[05:23.22]椒盐音乐[05:24.22]
[05:20.22]你好<05:23.22>椒盐音乐[05:24.22]
[05:20.22]<05:21.22>你好<05:23.22>椒盐音乐[05:24.22]
```

### 1.10 Go 解析策略

每行 tokenize 为时间戳+文本交替序列。时间戳匹配 `\[` 或 `\<` 然后 `(\d{1,3}):(\d{1,2})\.(\d{1,6})` 然后 `\]` 或 `\>`。

```
LINE := TS [ (TEXT TS)+ | (TEXT TS)* TEXT ]
```

算法（每物理行）：
1. 从左到右扫描所有时间戳 token（`[...]` 和 `<...>`），记录毫秒值和括号类型
2. **首** token 必须是 `[...]`（行开始）。`Line.StartMs = ts[0]`
3. `ts[i]` 和 `ts[i+1]` 之间的文本是一个 `Word{Text, StartMs: ts[i], EndMs: ts[i+1]}`
4. 若行末是时间戳（无尾随文本），最后那个时间戳是**显式行结束** → `Line.EndMs = ts[last]`。否则 `EndMs` 隐式 = 下一行 `StartMs`（第二遍填充）
5. **翻译检测**：StartMs 等于前一行 StartMs 的行（或无时间戳且紧随带时间戳行的行）是翻译 → 附加到同一 `Line.Translation`
6. **重复行展开**：多个前导 `[...]` 且无行内逐字时间戳 → 每个时间戳发射一行（复制 words）；有行内逐字时间戳则只展开第一个（spec 限制）

时间戳毫秒计算：
```go
func parseTS(min, sec, frac string) int {
    if len(frac) < 3 { frac += strings.Repeat("0", 3-len(frac)) }
    if len(frac) > 3 { frac = frac[:3] }
    m, _ := strconv.Atoi(min); s, _ := strconv.Atoi(sec); x, _ := strconv.Atoi(frac)
    return m*60000 + s*1000 + x
}
```

**重要**：SPL 解析器也覆盖普通 LRC（无行内时间戳时退化为行级）和 Enhanced LRC（`<>` 标签被 SPL tokenizer 支持）。无需单独的 LRC/A2 解析器。

---

## 2. 普通 LRC（基线）

**来源**：Wikipedia `LRC_(file_format)`

行级时间戳，最简格式。

```
[ti:Somebody to Love][ar:Jefferson Airplane][length:2:58]
[00:12.00]Line 1 lyrics
[00:17.20]Line 2 lyrics
[00:21.10][00:45.10]Repeating lyrics
```

- `[mm:ss.xx]`（xx = 百分秒；`.xxx` 三位也常见）
- ID 标签：`ti/ar/al/au/lr/length/by/offset/re/ve/#`
- `offset` = 全局毫秒偏移（`+` = 提前）
- 多前导时间戳 = 重复行
- 字结束 = 下一行 start（隐式）
- **SPL tokenizer 直接处理**，无需单独解析器

---

## 3. Enhanced LRC（A2 扩展）

**来源**：Wikipedia LRC A2 + `amll.dev/en/guides/lyric/formats.html`

```
[00:00.00] <00:00.04> When <00:00.16> the <00:00.82> truth <00:01.29> is <00:01.63> found <00:03.09> to <00:03.37> be <00:05.92> lies
```

- `[mm:ss.xx]` = 行开始；`<mm:ss.xx>` = 字开始（内联）
- 字结束 = 下个 `<>`（或行末下个 `[...]`）
- 同时支持 `.xx`（2 位）和 `.xxx`（3 位）
- **SPL tokenizer 直接处理**（`<>` 是 SPL 的延迟逐字标记）

---

## 4. YRC（网易云音乐逐字）

**来源**：LDDC `core/parser/yrc.py` + `amll.dev`

NetEase 公开 API `yv=-1` 返回，**纯文本无需解密**。

```
[190871,1984](190871,361,0)For (191232,172,0)the (191404,376,0)first (191780,1075,0)time
[193459,4198](193459,412,0)What's (193871,574,0)past (194445,506,0)is (194951,2706,0)past
```

- 行：`[lineStart_ms, lineDur_ms]`（整数毫秒）。`lineEnd = lineStart + lineDur`
- 字：`(wordStart_ms, wordDur_ms, 0)text` — **时间戳在前，然后文本**；第 3 字段恒为 `0`
- 字时间为**绝对值**（从歌曲开始）。`wordEnd = wordStart + wordDur`
- 全括号行 `(d,d)` 无文本 → 空行/间奏
- amll 将全括号行视为背景人声并去掉外层 `()`

**解析正则（LDDC）**：
```
line: ^\[(\d+),(\d+)\](.*)$
word: (?:\[\d+,\d+\])?\((?P<start>\d+),(?P<duration>\d+),\d+\)(?P<content>(?:.(?!\d+,\d+,\d+\)))*)
```

---

## 5. QRC（QQ 音乐逐字）

**来源**：LDDC `core/parser/qrc.py` + `amll.dev`

解密后的 payload 是 XML：
```xml
<?xml version="1.0" encoding="utf-8" standalone="yes"?>
<QrcInfos><QrcHeadInfo SqUin="0" .../>
<LyricInfo><Lyric_1 LyricType="1" LyricContent="..."/></LyricInfo></QrcInfos>
```

提取 `LyricContent` 后内容：
```
[ti:Song Title]
[ar:Artist]
[190871,1984]For (190871,361)the (191232,172)first (191404,376)time(191780,1075)
[193459,4198]What's (193459,412)past (193871,574)is (194445,506)past(194951,2706)
```

- 行：`[lineStart_ms, lineDur_ms]`。`lineEnd = lineStart + lineDur`
- 字：`text(wordStart_ms, wordDur_ms)` — **文本在前，然后时间戳**（无第 3 字段）
- 字时间为**绝对值**。`wordEnd = wordStart + wordDur`
- 内容为 `(\d+,\d+)`（字时间戳无文本）的行 → 空行（间奏）

**解析正则（LDDC）**：
```
line: ^\[(\d+),(\d+)\](.*)$
word: (?:\[\d+,\d+\])?(?P<content>(?:(?!\(\d+,\d+\)).)*)\((?P<start>\d+),(?P<duration>\d+)\)
```

**QRC vs YRC 的唯一区别**：YRC = `(start,dur,0)text`（ts 先，3 字段）；QRC = `text(start,dur)`（text 先，2 字段）。都是绝对毫秒。

### QRC 解密（CLOUD 类型，musicu.fcg 返回）

1. 输入是 **hex 字符串** → `bytearray.fromhex(s)`
2. **3DES ECB** 解密，key = `!@#)(*$%123ZXC!@!@#)(NHL`（24 字节）
3. `zlib.decompress(...)` 
4. `.decode("utf-8")` → QRC XML 文本

Go 实现：`crypto/des.NewTripleDESCipher` + 手动 ECB 分块（Go `crypto/des` 无 ECB 模式内置）+ `compress/zlib`。约 30 行。**必须带已知密文→明文测试向量**。

---

## 6. KRC（酷狗音乐逐字）

**来源**：LDDC `core/parser/krc.py`

解密后内容：
```
[ver:v1.0]
[ti:Title][ar:Artist][language:<base64-JSON>]
[320220,2000]<0,500,0>Hello <500,1500,0>world
```

- 行：`[lineStart_ms, lineDur_ms]`。`lineEnd = lineStart + lineDur`
- 字：`<offset_ms, wordDur_ms, 0>text` — 尖括号，3 字段，第 3 恒为 `0`
- **字时间是相对行开始的偏移**（KRC 独有！）。`wordStart_abs = lineStart + offset`；`wordEnd_abs = lineStart + offset + wordDur`
- `[language:...]` 标签是 base64 JSON，含额外轨道：`type:0` → 罗马音（字对齐），`type:1` → 翻译（行对齐）

**解析正则（LDDC）**：
```
line: ^\[(\d+),(\d+)\](.*)$
word: (?:\[\d+,\d+\])?<(?P<start>\d+),(?P<duration>\d+),\d+>(?P<content>(?:.(?!\d+,\d+,\d+>))*)
```

### KRC 解密

1. 去掉前 **4 字节**（magic header）
2. 每个剩余字节 XOR `KRC_KEY[i % len(KRC_KEY)]`，key = `@Gaw^2tGQ61-\xce\xd2ni`（9 字节）
3. `zlib.decompress(...)` → UTF-8 KRC 文本

Go 实现：`encoding/hex` + 手动 XOR + `compress/zlib`。约 15 行。**必须带测试向量**。

---

## 7. LYS（Lyricify Syllable）

**来源**：`github.com/WXRIW/Lyricify-App/docs/Lyricify 4/Lyrics.md` + `amll.dev`

```
[0]Lately (358,1336)I've (1694,487)been, (2181,673)I've (2854,268)been (3122,280)losing (3402,345)sleep(3747,1186)
[0]Dreaming (5245,696)about (5941,471)the (6412,306)things (6718,458)that (7176,292)we (7468,511)could (7979,393)be(8372,737)
```

- `[property]` 整数（0-8）编码 **背景人声 + 二重唱侧**（见下表）
- 字：`text(start_ms, dur_ms)` — 时间戳在词**之后**（类似 QRC 但 `()` 尾随）。绝对毫秒。`wordEnd = start + dur`
- 行级 start = 首字 start；行 end = 末字 end

| property | 背景 | 二重唱 | | property | 背景 | 二重唱 |
|---|---|---|---|---|---|---|
| 0 | 否 | 无 | | 4 | 否 | 左 |
| 1 | 否 | 左 | | 5 | 否 | 右 |
| 2 | 否 | 右 | | 6 | 是 | 无 |
| 3 | 否 | 无 | | 7 | 是 | 左 |
| | | | | 8 | 是 | 右 |

**解析**：`^\[(\d+)\](.*)$` 取 property+content；content 内 `(?:([^(（]*)(?:\((\d+),(\d+)\)))` 反复匹配——文本然后 `(start,dur)`。

---

## 8. TTML（Apple Music 逐字）

**来源**：`amll.dev/en/guides/lyric/ttml.html` + `github.com/amll-dev/amll-ttml-db`

### 命名空间
```xml
<tt xmlns="http://www.w3.org/ns/ttml"
   xmlns:ttm="http://www.w3.org/ns/ttml#metadata"
   xmlns:itunes="http://music.apple.com/lyric-ttml-internal"
   xmlns:amll="http://www.example.com/ns/amll"
   xmlns:tts="http://www.w3.org/ns/ttml#styling"
   xml:lang="ja" itunes:timing="Word">
```
`itunes:timing="Word"` = 逐字；`"Line"` = 行级。

### 时间格式
`begin`/`end`/`dur` 属性。时钟形式 `MM:SS.fff` 或 `HH:MM:SS.fff`（0-3 位小数）**或** 秒形式 `12.3s`。Apple Music TTML 常用秒带小数（如 `10.000`）。

### 结构
```xml
<body><div itunes:song-part="Verse">
  <p begin="10.000" end="12.000" itunes:key="L1" ttm:agent="v1">
    <span begin="10.000" end="10.500">こ</span>
    <span begin="10.500" end="11.000">れ</span>
    <span begin="11.000" end="12.000">は</span>
  </p>
</div></body>
```
- `<p>` = 一行（`begin/end` = 行时间；`itunes:key="L1"` = 行 id；`ttm:agent="v1"` → 歌手引用）
- `<span begin end>` = 一个字/音节。`Word{Text, StartMs, EndMs}`
- 内联角色 via `ttm:role`：`x-translation`（翻译）、`x-roman`（音译）、`x-bg`（背景人声）
- 元数据：`<ttm:title>`、`<ttm:agent xml:id type="person"><ttm:name>`、`<amll:meta key="musicName|artists|album|ncmMusicId|qqMusicId|appleMusicId" value="..."/>`

### Apple Music vs 通用 W3C TTML
Apple 增加 `itunes:` 命名空间（`itunes:timing`、`itunes:key`、`itunes:song-part`）和 sidecar `<iTunesMetadata>` 块（翻译/音译按 `for="L1"` 关联）。AMLL 增加 `amll:` 命名空间（`amll:meta`、`amll:obscene`、`amll:empty-beat`）和 `tts:ruby`（振假名/拼音）。

### Sidecar 翻译
```xml
<iTunesMetadata xmlns="http://music.apple.com/lyric-ttml-internal">
  <translations><translation xml:lang="zh-Hans-CN" type="subtitle">
    <text for="L1">First line translation</text></translation></translations>
  <transliterations><transliteration xml:lang="ja-Latn">
    <text for="L1">dai ichi gyou</text></transliteration></transliterations>
</iTunesMetadata>
```

### 解析
`encoding/xml` decoder；遍历 `<body>/<div>/<p>`；每个 `<p>` 收集 `<span>` 的 `begin/end`。时间 `10.500`/`00:10.500`/`10.5s` → ms（`int(seconds*1000)`）。

---

## 9. LRCv2

**来源**：`github.com/marz1877/LRCv2`

**注意**：LRCv2 是尖括号标签格式，**不是 YAML**。YAML `words[]` 格式是另一个格式（lrcget 的 "Lyrics File"），常被混淆。

EBNF：`timestamp = "<", digit,digit, ":", digit,digit, ".", digit,digit, ">"`。用 `<mm:ss.xx>`（非 `[...]`），`<br>` 换行，`<c:Verse 1>` 段落，`<s:Singer>` 二重唱标签，`<m>…</m:meaning>` 含义，`<t:fin:eng>…</t>` 翻译，`<ch:Em>` 和弦。

```
<00:11.45>I <00:11.89>walk <00:12.33>a <00:12.77>lone-<00:13.43>ly <00:13.75>road</00:14.09><br>
```

逐字：`<start>word</end>` 配对（开标签 = 字开始，闭标签 = 字结束）。6 星草案提议，未广泛采用。

---

## 10. "Lyrics File"（lrcget YAML）— 非标准

**来源**：`github.com/tranxuanthang/lrcget`

常被误称为 "LRCv2"，但实际是独立格式：

```yaml
version: '1.0'
metadata:
  title: 'Your Shape'
  artist: 'Eddy'
  duration_ms: 235000
lines:
  - text: "The school isn't the best place to find a lover"
    start_ms: 12450
    end_ms: 18200
    words:
      - {text: 'The ', start_ms: 12450, end_ms: 12900}
      - {text: 'school ', start_ms: 12900, end_ms: 13500}
```

---

## 歌词爬取源（LDDC 参考）

LDDC 支持 4 个源（无酷我），默认优先级 QM → KG → NE。均无需真实登录。

### 源 1: QQ 音乐（musicu.fcg）
- 端点：`POST https://u.y.qq.com/cgi-bin/musicu.fcg`
- 先调 `GetSession` 获取 session，后续请求带 uid/sid/userip
- 搜索：method `DoSearchForQQMusicLite`, module `music.search.SearchCgiService`
- 歌词：method `GetPlayLyricInfo`, module `music.musichallSong.PlayLyricInfo`，`qrc=1` 请求逐字
- 返回的 lyric 字段是 **hex 编码 + 3DES 加密 + zlib 压缩** 的 QRC payload
- Header: `cookie: tmeLoginType=-1`, `user-agent: okhttp/3.14.9`, `content-type: application/json`

### 源 2: 网易云（公开 API）
- 搜索：`POST https://music.163.com/api/search/get`，form: `s=keyword&type=1&limit=20`
- 歌词：`GET https://music.163.com/api/song/lyric?os=pc&id=<songId>&lv=-1&kv=-1&tv=-1&rv=-1&yv=-1`
- `yv=-1` 返回 YRC 逐字（`yrc.lyric` 字段，纯文本）
- Header: `User-Agent: Mozilla/5.0 ... NeteaseMusicDesktop/...`, `Cookie: os=pc`

### 源 3: 酷狗（lyrics.kugou.com）
- 先注册 dfid：`POST https://userservice.kugou.com/risk/v1/r_register_dev`（30 分钟缓存）
- 搜索歌词：`GET https://lyrics.kugou.com/v1/search?album_audio_id=<id>&hash=<hash>&duration=<ms>&keyword=...`
- 下载：`GET http://lyrics.kugou.com/download?id=<id>&accesskey=<key>&fmt=krc`（KRC 加密）或 `fmt=lrc`（纯文本）

### 源 4: LRCLIB（首选，最简单）
- 公开 REST API，无 auth 无 crypto
- 返回纯 LRC（SPL 解析器直接处理）
- 端点：`GET https://lrclib.net/api/search?track_name=...&artist_name=...&album_name=...`

### 匹配启发式
- 关键词：`artist-title`（优先）或 `title` 或文件名
- 时长门控：`abs(local.duration - result.duration) > 4000` ms 则跳过
- 评分（0-100）：标题相似度 + 歌手相似度 + 专辑相似度加权组合
- cutoff：55 分以下不取
- 差 15 分内取歌词最丰富者（verbatim+ts+roma > verbatim+ts > ... ）
- 源优先级：QM → KG → NE（LRCLIB 首选因为最简单）

### 解密算法
```
QRC_KEY = "!@#)(*$%123ZXC!@!@#)(NHL"  # 24 bytes, 3DES
KRC_KEY = "@Gaw^2tGQ61-\xce\xd2ni"     # 9 bytes, XOR
```
- QRC: hex → 3DES ECB → zlib → UTF-8 XML
- KRC: strip 4 bytes → XOR → zlib → UTF-8
- YRC: 纯文本无需解密

### Go 参考库
- `MiChongs/karpov-gateway`（MIT，最干净，musicu.fcg + EAPI）← 优先参考
- `go-musicfox/go-musicfox`（MIT，NetEase EAPI + YRC）
- `winterssy/mxget`（GPL-3，多源，用旧 QQ 端点）

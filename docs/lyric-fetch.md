# Automatic Lyrics

musicli loads same-basename `.spl` and `.lrc` files, then embedded tags,
then its fetched-lyric cache. It only invokes an external lyric bridge when
all three are unavailable and `lyrics.auto_fetch` is enabled.

## LDDC Bridge

LDDC provides the `lddc-fetch` command used by musicli. Install a version of
LDDC containing this command into the same environment as musicli:

```sh
cd /path/to/LDDC
uv tool install .
```

Verify that `lddc-fetch` is available on `PATH` before enabling automatic
fetching:

```sh
lddc-fetch <<<'{"title":"example"}'
```

Set the following in `~/.config/musicli/config.toml`:

```toml
[lyrics]
auto_fetch = true
sources = ["qq", "netease", "kugou", "lrclib"]
fetch_command = "lddc-fetch"
fetch_timeout_seconds = 12
```

The source order breaks ties between lyrics of the same timing quality. LDDC
prefers word-timed lyrics over enhanced or line-timed lyrics before applying
that source order. Results are parsed by musicli and atomically cached in
`lyrics.save_dir`; invalid, timed-out, or failed bridge responses are never
cached and do not interrupt playback.

# FreeTokens

An unofficial, cross-platform Freebuff usage tracker for the menu bar, the system tray and the
command line. It shows your Freebuff hours, session timers and measured token usage, with
estimated dollar costs at public API rates.

> **Unofficial.** FreeTokens is not affiliated with, endorsed by, or supported by Freebuff,
> Codebuff or CodebuffAI. It only reads your own usage with your existing Freebuff login.

## What it shows

| Row | Source |
| --- | --- |
| **Daily Hours** – hours left in today's free pool and when it resets | Freebuff session endpoint (read-only GET) |
| **Hours Balance** – your separate wallet, in whole hours (5 Freebucks = 1 hour) | same |
| **Monthly Spend** – when Freebuff reports USD | same |
| **Session timers** – time left on each active session, in the menu bar title on macOS and in the tooltip elsewhere | same |
| Legacy **Daily / Weekly / Monthly / 5-Day / Premium** session windows on older accounts | same |
| **Today / Yesterday / Last 30 Days** tokens per model, with estimated cost | Hosted run history plus Freebuff Desktop's local history |

Token counts are measured receipts, never estimates. Dollar figures are **estimates at public API
prices** (the [OpenRouter](https://openrouter.ai/api/v1/models) model list and the OpenUsage
pricing supplement), not what Freebuff charges you. A total marked `≥` is a lower bound: some
tokens belong to a model with no public price, or some runs have no complete receipt yet.

## Install

Download the archive for your platform from
[Releases](https://github.com/tverma101/FreeTokens/releases), check it against `checksums.txt`, and
extract `freetokens` (`freetokens.exe` on Windows) somewhere on your `PATH`.

| OS | Architectures |
| --- | --- |
| macOS 12+ | Apple silicon (`arm64`), Intel (`amd64`) |
| Windows 10+ | `amd64`, `arm64`, `386` |
| Linux | `amd64`, `arm64`, `armv6`, `armv7`, `386`, `riscv64`, `ppc64le`, `s390x`, `loong64` |
| FreeBSD | `amd64`, `arm64` |
| OpenBSD | `amd64`, `arm64` |
| NetBSD | `amd64` |

**Windows:** extract the zip to a folder such as `%LOCALAPPDATA%\Programs\FreeTokens`. It holds two
copies of the same program: `freetokens.exe` for the command line, and `freetokensw.exe`, which
starts the tray without a console window. Double-click `freetokensw.exe` to start the tray, then
turn on **Settings → Start with Windows**. Windows may hide new tray icons under the **^** arrow;
drag the icon onto the taskbar to keep it visible. SmartScreen may warn on first launch because
releases aren't code-signed yet; choose **More info → Run anyway**.

Every build except macOS is pure Go with no system libraries, so the CLI works on servers too.
The Linux and BSD tray needs a desktop with StatusNotifierItem support: KDE, or GNOME with the
AppIndicator extension, Xfce, Cinnamon, and others.

macOS builds aren't notarized yet. After extracting, clear the quarantine flag once:

```bash
xattr -d com.apple.quarantine ./freetokens
```

Or build from source with Go 1.26+:

```bash
go install github.com/tverma101/FreeTokens/cmd/freetokens@latest
```

## Use

```bash
freetokens            # tray / menu bar (prints status instead when there is no desktop session)
freetokens status     # one-off report; --json for scripts
freetokens autostart on   # on Windows this registers freetokensw.exe
```

You need to have signed in once with the Freebuff CLI (`freebuff login`). FreeTokens reads the
CLI's existing `default` profile and never signs in, starts a session, or changes your account. It
looks for the token in these places, in order:

1. `authToken` in `~/.config/manicode/credentials.json`, or in `$FREEBUFF_CONFIG_DIR/credentials.json`.
2. Current Freebuff CLIs store the token in the OS credential store under service `freebuff-cli`:
   the macOS Keychain, Windows Credential Manager, or the Secret Service on Linux.
3. `CODEBUFF_API_KEY`.

**Credential prompts.** Background refreshes never show a password or keychain prompt. If the OS
needs your approval, the tray shows a warning. Choose **Refresh Now** (or run
`freetokens status --prompt`) and allow access.

On macOS, choose **Always Allow**. The Keychain ties that approval to the binary's code signature,
and releases are not Developer ID-signed yet, so macOS asks once more after each update.

Freebuff Desktop's local history (`~/.config/freebuff-desktop/projects/*/desktop-v2.db`) is
read-only, and FreeTokens only reads its usage counters and model fields, never conversation
text. Hosted history events other than metrics are skipped without being decoded.

## Updates (opt-in)

FreeTokens never contacts GitHub unless you turn updates on. In the tray, use **Settings → Updates
from GitHub Releases**:

- **Never Check** (default)
- **Check Daily and Notify** shows an **Install** item when a release is available
- **Check Daily and Install** downloads, verifies and restarts automatically

Or from the CLI:

```bash
freetokens config set auto-update notify   # off | notify | auto
freetokens update --check
freetokens update
```

Each update is checked against the release's `checksums.txt` (SHA-256) before the running binary
is replaced. If FreeTokens is installed somewhere you can't write, such as a system directory,
update it the way you installed it.

## Settings and files

| What | Where |
| --- | --- |
| Settings | `freetokens config get` (stored in your OS config directory under `FreeTokens/`) |
| Caches, tray log | the OS cache directory under `FreeTokens/` (e.g. `~/Library/Caches/FreeTokens`) |
| Diagnostics | `freetokens diagnose` prints paths, auth state and numeric quota fields, never tokens |

## Privacy

FreeTokens sends requests only to:

- `www.codebuff.com`: your session, model catalog and run history, with your token
- `openrouter.ai` and `robinebers.github.io`: public price lists, without your token
- `api.github.com`: release checks, only when you enable updates

Redirects are refused, so your token can't be forwarded to another host. Caches hold no tokens.
The hosted-history cache is keyed by a SHA-256 of the token and is readable only by you.

## Differences from the QuotaBar tracker

This is a port of the Freebuff provider from a QuotaBar fork. Behavior matches it, with two
exceptions:

- Pricing uses only exact Freebuff routes (the supplement, then OpenRouter). The fuzzy LiteLLM and
  models.dev fallbacks are left out, so some unrecognized models show as unpriced instead of
  getting an approximate price.
- One supplement alias rule uses a regex lookahead that Go can't compile. It covers a Gemini
  model, not a Freebuff route, and is skipped.

## Credits

- The tracker logic is ported from a fork of [OpenUsage](https://github.com/robinebers/openusage) (QuotaBar)
  by Robin Ebers (MIT). See [NOTICE](NOTICE).
- [fyne.io/systray](https://github.com/fyne-io/systray),
  [go-selfupdate](https://github.com/creativeprojects/go-selfupdate),
  [modernc.org/sqlite](https://gitlab.com/cznic/sqlite),
  [go-keyring](https://github.com/zalando/go-keyring), [wincred](https://github.com/danieljoos/wincred),
  [go-timezone-local](https://github.com/thlib/go-timezone-local).

## License

[MIT](LICENSE)

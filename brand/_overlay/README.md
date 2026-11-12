# Kestrel

Kestrel is a **Windows desktop app** for using one model across many coding
agents: it keeps Claude Code, Codex, Gemini CLI, OpenCode and the rest pointed
at a single local gateway, so a model or account can be switched from one
place.

It is a **rebranded build of [magpie](https://github.com/yetone/magpie)**. The
behaviour, protocol and code are upstream's; this repository only changes the
name, the icons, the wording of the interface and the default settings. The
original project is the work of **yetone and magpie's contributors** — see the
upstream repository and its history for the full list:
<https://github.com/yetone/magpie/graphs/contributors>.

## Download

Windows 10/11, amd64. Take `kestrel-windows-amd64.exe` from the
[latest release](https://github.com/zx5920/kestrel/releases/latest) and run
it. There is no installer and no separate runtime to add: WebView2 ships with
Windows.

## Updates

Updates check **your** release feed,
`https://github.com/<owner>/kestrel/releases/latest/download/update.json`, and
they are **off by default**. To turn the check on:

```
kestrel update auto on        # off again: kestrel update auto off
kestrel update check          # ask now
kestrel update                # download, verify and install the new build
```

The new build is started the next time the app opens; the old one is kept
beside it as `kestrel.exe.old`.

## Building from source

The repository tracks upstream's files **byte for byte**. Every Kestrel change
lives under [`brand/`](brand/README.md) and is applied to a throwaway worktree
at build time, which is why merging `upstream/main` never conflicts. One
command does the whole loop — sync, brand, build, `update.json`:

```powershell
pwsh -File .\sync.ps1 -Test            # build; add -Publish to make a release
```

Go 1.26+ is required. `-Publish` needs the [GitHub CLI](https://cli.github.com)
logged in. A scheduled workflow,
[`.github/workflows/follow-upstream.yml`](.github/workflows/follow-upstream.yml),
runs the same command every day and publishes the result.

## Migrating from magpie

The first start adopts an existing `magpie` setup: the config folder is moved
to `kestrel`, and the provider entries magpie wrote into agents' config files
(a model id, `base_url`, an API token) are rewritten in place. Each rewritten
file is copied to `<name>.pre-kestrel.bak` first, so the change can be undone
by copying it back. Files that don't parse are left untouched. Old
`MAGPIE_*` environment variables, `sk-magpie-key-` keys and `/v1/magpie/*`
gateway paths keep working.

## Licence and credits

MIT. Kestrel is a derivative of magpie, © 2026 yetone, and the original
licence is kept unchanged in [LICENSE](LICENSE); [NOTICE](NOTICE) and
[THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES) record the derivation and the other
projects involved. Upstream's documentation and marketing site are not part of
this fork; both live in [yetone/magpie](https://github.com/yetone/magpie).

# The brand layer

The repository tracks upstream [yetone/magpie](https://github.com/yetone/magpie)'s
files byte for byte. Nothing in the tree says "kestrel": every brand change
lives here and is applied to a throwaway worktree at build time. That is why
`git merge upstream/main` never conflicts — upstream edits files this fork has
not touched.

```
brand/
  brand.json          the names, the protect list, what the rename reads
  apply/              the applier: rename, then patches, then the overlay
  patches/            one unified diff per upstream file this fork edits
  overlay/            whole files this fork owns
sync.ps1              sync → apply → build → update.json (→ gh release)
.github/workflows/follow-upstream.yml   the same, on a schedule
```

## Three kinds of change

1. **Mechanical rename** — `magpie` → `kestrel`, `Magpie` → `Kestrel`,
   `MAGPIE_` → `KESTREL_`, `github.com/yetone/magpie` →
   `github.com/kestrel-app/kestrel`, over every text file. Nothing to
   maintain: it is one function, `rename()` in `apply/main.go`, driven by
   `brand.json`. `brand.json`'s `protect` list keeps the strings that must not
   move — upstream's site, the npm org and GitHub repo plugin specs resolve
   against, the catalog JSON keys that are a wire format (`magpie_price` and
   friends).

2. **Patches** (`brand/patches/*.patch`) — for an upstream file this fork has
   to change *beyond* the rename: the update default, the no-outbound-links
   pass, the `/v1/magpie` compatibility route, the migration. Each patch is a
   diff against the **renamed** upstream file, applied three-way
   (`git apply --3way`), so a hunk whose context upstream moved around still
   lands.

3. **Overlay** (`brand/overlay/**`) — files this fork owns outright: the
   icons, `NOTICE`, the migration's test. Copied last and never rewritten, so
   they are written in their final spelling.

`brand/apply` runs them in that order and writes `.brand-applied` into the
tree, so a tree cannot be branded twice.

## Day to day

```powershell
.\sync.ps1                # fetch upstream, apply, build, dist/update.json
.\sync.ps1 -Test          # also compile the whole tree and run the migration tests
.\sync.ps1 -Publish       # also `gh release create v<version>`
.\sync.ps1 -SkipSync      # build what is checked out
```

`go run ./brand/apply -root . -inventory` sorts a working tree's differences
from `HEAD` into *mechanical* (the rename explains it), *brand-only* (same file,
brand spelled another way), *hand* (edited beyond the brand) and *new*. That is
how the patch list was made, and how a new divergence is found.

## When a patch conflicts

`git apply --3way` leaves `<<<<<<<` markers in the file and `sync.ps1` stops
with "a patch no longer applies". Nothing is built, nothing is published. Then:

1. `git -C dist/src status` — the file is listed, markers are in it.
2. Read upstream's new version (`git show HEAD:<file>`) next to the patch.
   Upstream usually moved the code the patch edits; the change itself is still
   wanted.
3. Resolve the markers in `dist/src` the way the patch intended, then
   regenerate the patch so it is a clean diff against the new upstream text:

   ```powershell
   # in the throwaway tree, with the resolution in place
   git -C dist/src diff --output=brand/patches/<name>.patch -- <file>
   ```

   (The tree's index holds the renamed upstream text — that is the pre-image
   the patch is written against.)
4. `.\sync.ps1 -SkipSync -Test` and commit the regenerated patch.

If a patch is no longer wanted at all — upstream did the same thing — delete
it. If the change is bigger than a patch can carry, move the file to
`brand/overlay/` instead: same path, whole file, final spelling.

Rule of thumb: a patch stays a patch while its diff is small. Past ~150 lines,
or when the file is really this fork's, it belongs in the overlay.

## What the layer does not touch

`site/`, `docs/`, `.github/` and `brand/` itself. The marketing site and the
docs are upstream's; upstream's CI is left as it is (it will mostly fail in a
fork — disable it under Actions if the red crosses bother you, ours is
`follow-upstream.yml`). None of them end up in the binary.

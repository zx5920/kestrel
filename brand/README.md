# The brand layer

The repository tracks upstream [yetone/magpie](https://github.com/yetone/magpie)'s
files byte for byte (except `docs/` and `site/`, which this fork does not
ship). Nothing in the tree says "kestrel": every brand change lives here and is
applied to a throwaway worktree at build time. That is why `git merge
upstream/main` never conflicts on code — upstream edits files this fork has not
touched.

```
brand/
  brand.json          the names, the protect list, what the rename reads
  apply/              the applier: rename, then patches, then the overlay
  patches/            one unified diff per upstream file this fork edits
  _overlay/           whole files this fork owns (the "_" keeps Go out)
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

3. **Overlay** (`brand/_overlay/**`) — files this fork owns outright: the
   icons, `NOTICE`, the repository README, the migration's test. Copied last
   and never rewritten, so they are written in their final spelling. The
   leading underscore keeps `go build ./...` from treating the copies as
   packages of this module.

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
   regenerate the patch so it is a clean diff against the new upstream text.
   A three-way apply writes its result into the index, so reset first and
   rebuild the pre-image — a plain `git diff` now would be a diff against the
   final state, not against the renamed upstream:

   ```powershell
   git -C dist/src checkout --detach --force HEAD
   git -C dist/src clean -fdq
   go run ./brand/apply -root dist/src -brand brand -rename-only
   git -C dist/src add -A                      # index = renamed upstream text
   # copy the resolved file back into dist/src/<path>
   git -C dist/src diff --no-color --output=..\brand\patches\<name>.patch -- <path>
   ```

4. `.\sync.ps1 -SkipSync -Test` and commit the regenerated patch.

If a patch is no longer wanted at all — upstream did the same thing — delete
it. If the change is bigger than a patch can carry, move the file to
`brand/_overlay/` instead: same path, whole file, final spelling.

Rule of thumb: a patch stays a patch while its diff is small. Past ~150 lines,
or when the file is really this fork's, it belongs in the overlay.

## The former name is a wire value

The migration in `internal/agent/legacy.go` and `internal/appdir/former.go`
looks for what Magpie wrote — `magpie` as a provider id, `magpie` as Claude's
token, the `magpie` config folder. Those literals are spelled out (`formerID`,
`FormerName`) and must survive the rename: they are the thing being migrated
*from*. `formerID` lives in a patch, and the patches are applied after the
rename and are never renamed themselves, so it stays; do not "fix" it to
`kestrel`, and do not add the bare word to `protect` — that would stop the
rename everywhere else.

## What the layer does not touch

`.github/` and `brand/` itself. Upstream's CI is left as it is and disabled in
this repo under Actions; ours is `follow-upstream.yml`.

`site/` and `docs/` — upstream's marketing site and documentation — are not in
this fork at all (2.4 MB and 69 MB, neither in the binary). They were removed
in a commit, which makes every upstream edit there a modify/delete conflict;
`sync.ps1` resolves those by keeping the files deleted and finishing the merge,
and drops any file upstream adds back under those two paths. A conflict outside
`docs/`, `site/` and the README still stops the build, as it should.

The repository's `README.md` is this fork's own too (`brand/_overlay/README.md`
is the master copy, and the build overwrites it again). `README.zh-CN.md` was
removed. An upstream edit to either is resolved the same way: the README stays
this fork's, the Chinese one stays deleted.

#Requires -Version 5.1
<#
.SYNOPSIS
  Follow upstream magpie: sync, re-apply the Kestrel brand layer, build
  kestrel-windows-amd64.exe, and write dist/update.json.

.DESCRIPTION
  The repository tracks upstream's files unchanged; every brand change lives
  under brand/ and is applied to a throwaway worktree (dist/src), so merging
  upstream never conflicts. This script is the whole loop:

    1. git fetch upstream && git merge upstream/main
    2. reset dist/src to the new HEAD, apply brand/ (rename + patches + overlay)
    3. build the Windows amd64 binary and its launcher
    4. write dist/update.json (version, url, size, sha256)

  Run it with no arguments to just build what is checked out. Add -Publish to
  create the GitHub release (needs the gh CLI, logged in).

.PARAMETER Version
  Version to stamp. Default 0.1.<commit count>, which only ever grows.

.PARAMETER FeedUrl
  Update feed baked into the binary. Default: brand.json's feedURL, else
  https://github.com/<owner>/<repo>/releases/latest/download/update.json
  from the origin remote.

.PARAMETER Publish
  After building, `gh release create v<version>` with the exe and update.json.

.PARAMETER SkipSync
  Build what is checked out; do not fetch or merge upstream.

.EXAMPLE
  pwsh -File .\sync.ps1
.EXAMPLE
  pwsh -File .\sync.ps1 -Publish
#>
[CmdletBinding()]
param(
	[string]$UpstreamUrl = "https://github.com/yetone/magpie.git",
	[string]$UpstreamBranch = "main",
	[string]$Version = "",
	[string]$FeedUrl = "",
	[string]$Work = "dist/src",
	[switch]$SkipSync,
	[switch]$Test,
	[switch]$Publish
)

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot
$work = Join-Path $root $Work

function Step([string]$text) { Write-Host "`n==> $text" -ForegroundColor Cyan }
function Fail([string]$text) { Write-Host "error: $text" -ForegroundColor Red; exit 1 }
function Git([string[]]$args, [string]$dir = $root) {
	$out = & git -C $dir @args 2>&1
	if ($LASTEXITCODE -ne 0) { Fail ("git " + ($args -join " ") + "`n" + ($out -join "`n")) }
	return $out
}

# --- preflight -------------------------------------------------------------

if (-not (Get-Command git -ErrorAction SilentlyContinue)) { Fail "git is not on PATH" }
$go = Get-Command go -ErrorAction SilentlyContinue
if (-not $go) {
	# the workspace ships a toolchain beside the repository
	$local = Join-Path (Split-Path $root -Parent) "toolchain\go\bin\go.exe"
	if (Test-Path $local) { $go = Get-Item $local; $env:PATH = "$(Split-Path $local -Parent);$env:PATH" }
}
if (-not $go) { Fail "go is not on PATH (install Go 1.26+, or put it in ..\toolchain\go)" }

Push-Location $root
try {
	$dirty = & git status --porcelain
	if (-not $SkipSync) {
		Step "upstream: $UpstreamUrl ($UpstreamBranch)"
		if ($dirty) {
			Fail "the tree has uncommitted changes; commit or stash them first (the merge must be a fast-forward of upstream's files)"
		}
		$remotes = & git remote
		if ($remotes -notcontains "upstream") {
			Git @("remote", "add", "upstream", $UpstreamUrl) | Out-Null
		}
		Git @("fetch", "--quiet", "upstream", $UpstreamBranch) | Out-Null
		# a merge, not a fast-forward: this fork's commits (brand/, sync.ps1,
		# the workflow) are not upstream's descendants. It cannot conflict —
		# the commits here never touch upstream's files — and if it does,
		# something was committed that belongs in brand/ instead.
		$merge = & git merge --no-edit "upstream/$UpstreamBranch" 2>&1
		if ($LASTEXITCODE -ne 0) {
			Fail ("merging upstream/$UpstreamBranch conflicted — the fork must only commit brand/, sync.ps1 and .github/workflows:`n" + ($merge -join "`n"))
		}
		if ($merge -match "Already up to date") { Write-Host "   upstream: nothing new" }
	}

	$head = (Git @("rev-parse", "HEAD")).Trim()
	$short = (Git @("rev-parse", "--short", "HEAD")).Trim()
	$count = (Git @("rev-list", "--count", "HEAD")).Trim()
	if (-not $Version) { $Version = "0.1.$count" }
	if ($Version -notmatch '^\d+\.\d+\.\d+$') { Fail "version must be x.y.z, not '$Version'" }

	# the feed this build looks for updates at
	if (-not $FeedUrl) {
		$brand = Get-Content (Join-Path $root "brand\brand.json") -Raw | ConvertFrom-Json
		$FeedUrl = $brand.feedURL
	}
	if (-not $FeedUrl) {
		$origin = (& git remote get-url origin) -replace '^git@github\.com:', 'https://github.com/' -replace '\.git$', ''
		if ($origin -match '^https://github\.com/([^/]+)/([^/]+)$') {
			$FeedUrl = "$origin/releases/latest/download/update.json"
		}
	}
	if (-not $FeedUrl) {
		Write-Host "warning: no feed url (no origin remote and no brand.json feedURL); the binary falls back to the built-in default" -ForegroundColor Yellow
	}

	# --- brand layer into a throwaway worktree -----------------------------

	Step "brand layer -> $Work at $short"
	& git worktree prune
	if (-not (Test-Path (Join-Path $work ".git"))) {
		Git @("worktree", "add", "--detach", $Work, $head) | Out-Null
	}
	Git @("checkout", "--detach", "--force", $head) $work | Out-Null
	Git @("clean", "-fdq") $work | Out-Null
	Push-Location $root
	try {
		& go run ./brand/apply -root $Work -brand brand
		if ($LASTEXITCODE -ne 0) { Fail "brand layer failed (a patch no longer applies: see brand/README.md, fix it there)" }
	} finally { Pop-Location }

	# --- build -------------------------------------------------------------

	if ($Test) {
		Step "test the branded tree"
		Push-Location $work
		try {
			& go build ./...
			if ($LASTEXITCODE -ne 0) { Fail "the branded tree does not compile (an upstream change met a patch)" }
			& go test ./internal/agent -run "TestRename" -count=1
			if ($LASTEXITCODE -ne 0) { Fail "the migration tests fail in the branded tree" }
		} finally { Pop-Location }
	}

	Step "build kestrel $Version ($short)"
	$env:CGO_ENABLED = "0"
	$env:GOOS = "windows"
	$env:GOARCH = "amd64"
	Push-Location $work
	try {
		& go run ./build/assetpack internal/gui/assets internal/gui/assets.zip
		if ($LASTEXITCODE -ne 0) { Fail "assetpack failed" }
		& go run github.com/tc-hib/go-winres@v0.3.3 make --in build/windows/winres.json --arch amd64 --out rsrc --file-version $Version --product-version $Version
		if ($LASTEXITCODE -ne 0) { Fail "go-winres failed" }
		& go build -trimpath -buildvcs=false -ldflags "-s -w" -o internal/update/launcher-amd64.com ./internal/update/launcher
		if ($LASTEXITCODE -ne 0) { Fail "launcher build failed" }
		$ld = "-s -w -X main.version=$Version -H windowsgui"
		if ($FeedUrl) { $ld += " -X github.com/kestrel-app/kestrel/internal/update.FeedURL=$FeedUrl" }
		$out = Join-Path $root "dist\kestrel-windows-amd64.exe"
		& go build -tags "production,comlauncher,packedassets" -trimpath -buildvcs=false -ldflags $ld -o $out .
		if ($LASTEXITCODE -ne 0) { Fail "kestrel build failed" }
	} finally { Pop-Location }

	# --- update.json -------------------------------------------------------

	Step "update.json"
	$exe = Join-Path $root "dist\kestrel-windows-amd64.exe"
	$size = (Get-Item $exe).Length
	$sha = (Get-FileHash $exe -Algorithm SHA256).Hash.ToLower()
	$origin = (& git remote get-url origin) -replace '^git@github\.com:', 'https://github.com/' -replace '\.git$', ''
	$tag = "v$Version"
	$notes = "built from upstream $short"
	$assetUrl = if ($origin -match '^https://github\.com/[^/]+/[^/]+$') { "$origin/releases/download/$tag/kestrel-windows-amd64.exe" } else { "" }
	$pageUrl = if ($origin -match '^https://github\.com/[^/]+/[^/]+$') { "$origin/releases/tag/$tag" } else { "" }
	$feed = [ordered]@{
		version = $Version
		notes   = $notes
		url     = $pageUrl
		assets  = [ordered]@{
			"kestrel-windows-amd64.exe" = [ordered]@{ url = $assetUrl; size = $size; sha256 = $sha }
		}
	}
	$feedPath = Join-Path $root "dist\update.json"
	[IO.File]::WriteAllText($feedPath, ($feed | ConvertTo-Json -Depth 6), (New-Object System.Text.UTF8Encoding $false))

	Write-Host ""
	Write-Host "  version  $Version"
	Write-Host "  binary   dist\kestrel-windows-amd64.exe  $size bytes"
	Write-Host "  sha256   $sha"
	Write-Host "  feed     dist\update.json  ->  $FeedUrl"
	if ($FeedUrl -match '^https://github\.com/([^/]+)/([^/]+)/') {
		Write-Host "  release  https://github.com/$($Matches[1])/$($Matches[2])/releases (asset: kestrel-windows-amd64.exe)"
	}

	if ($Publish) {
		if (-not (Get-Command gh -ErrorAction SilentlyContinue)) { Fail "-Publish needs the gh CLI" }
		if (-not $assetUrl) { Fail "-Publish needs an origin remote on github.com (the asset url is what the feed points at)" }
		# a scheduled run with no upstream news lands on the same version
		& gh release view $tag *> $null
		if ($LASTEXITCODE -eq 0) {
			Write-Host "release $tag already exists; nothing to publish"
			return
		}
		Step "publish $tag"
		& gh release create $tag $exe $feedPath --title "Kestrel $Version" --notes $notes
		if ($LASTEXITCODE -ne 0) { Fail "gh release create failed" }
		Write-Host "published: $pageUrl"
	}
} finally {
	Pop-Location
}

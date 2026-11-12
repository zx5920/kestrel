// Command brand applies the Kestrel brand layer to a pristine upstream tree.
//
// The repository tracks upstream's files unchanged; every brand change lives
// under brand/ and is applied here, at build time. That is what keeps a merge
// from upstream free of conflicts: upstream edits files the repository has
// never touched, so git has nothing to disagree about.
//
// Three kinds of change, in this order:
//
//  1. the mechanical rename (magpie -> kestrel, module path, env prefix) over
//     text files, with brand.json's protect list put aside first;
//  2. brand/patches/*.patch, unified diffs against upstream's own text,
//     applied three-way so a moved hunk still lands;
//  3. brand/_overlay/**, whole files this fork owns, copied last and never
//     rewritten — they are written in their final form. The leading
//     underscore keeps the Go tool from walking the copies as packages.
//
// Every step is idempotent: running it twice writes the same bytes.
//
//	go run ./brand/apply -root .
//	go run ./brand/apply -root . -check      # exit 1 if anything would change
//	go run ./brand/apply -root . -inventory  # which files are hand-edited
//
// -inventory compares the tree against the brand layer run over git HEAD and
// sorts the differences into: mechanical (the layer explains it), hand
// (someone edited a file the layer only renames), new (a file upstream does
// not have). Run it with a clean, unapplied tree.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Brand struct {
	Name               string   `json:"name"`
	NameLower          string   `json:"nameLower"`
	Module             string   `json:"module"`
	UpstreamModule     string   `json:"upstreamModule"`
	EnvPrefix          string   `json:"envPrefix"`
	UpstreamEnvPrefix  string   `json:"upstreamEnvPrefix"`
	ProviderID         string   `json:"providerID"`
	UpstreamProviderID string   `json:"upstreamProviderID"`
	KeyPrefix          string   `json:"keyPrefix"`
	AssetName          string   `json:"assetName"`
	FeedURL            string   `json:"feedURL"`
	Site               string   `json:"site"`
	ReleasesPage       string   `json:"releasesPage"`
	// Protect are the strings the rename must never touch: upstream's own
	// domains, the catalog JSON keys that are a wire format, referral codes.
	Protect []string `json:"protect"`
	// Exts are the text files the rename reads. Binary assets (icons, the
	// .exe) are never rewritten; they come from brand/_overlay.
	Exts []string `json:"exts"`
	// Skip are directories neither the rename nor the walk enters.
	Skip []string `json:"skip"`
}

// brandDir is where the layer lives, relative to the repository root.
const brandDir = "brand"

func main() {
	root := flag.String("root", ".", "repository root to apply the layer to")
	brandPath := flag.String("brand", "", "brand layer directory (default <root>/brand)")
	check := flag.Bool("check", false, "report what would change, write nothing")
	renameOnly := flag.Bool("rename-only", false, "stop after the mechanical rename (used to regenerate patches)")
	inventory := flag.Bool("inventory", false, "sort the tree's differences from git HEAD into mechanical, hand and new")
	ref := flag.String("ref", "HEAD", "git ref -inventory compares against")
	flag.Parse()

	if *brandPath == "" {
		*brandPath = filepath.Join(*root, brandDir)
	}
	b := readBrand(*brandPath)
	if *inventory {
		os.Exit(inventoryReport(*root, b, *ref))
	}
	n, err := apply(*root, *brandPath, b, *check, *renameOnly)
	if err != nil {
		fmt.Fprintln(os.Stderr, "brand:", err)
		os.Exit(1)
	}
	verb := "applied"
	if *check {
		verb = "would change"
	}
	fmt.Printf("brand: %s %d files\n", verb, n)
	if *check && n > 0 {
		os.Exit(1)
	}
}

func readBrand(dir string) *Brand {
	p := filepath.Join(dir, "brand.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		fail(err)
	}
	var b Brand
	if err := json.Unmarshal(raw, &b); err != nil {
		fail(fmt.Errorf("%s: %w", p, err))
	}
	if len(b.Exts) == 0 {
		b.Exts = []string{
			".go", ".mod", ".sum", ".json", ".jsonc", ".js", ".mjs", ".ts",
			".html", ".css", ".svg", ".plist", ".sh", ".ps1", ".md",
		}
	}
	if len(b.Skip) == 0 {
		b.Skip = []string{".git", "node_modules", "dist", "toolchain", brandDir, ".brand-work"}
	}
	return &b
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "brand:", err)
	os.Exit(1)
}

// appliedMarker is written to the root once the layer has run. The rename is
// text-based and the patched files carry the brand in their final spelling,
// so a second pass over the same tree would rename those too: the marker is
// how a tree is known to be a build tree rather than upstream's.
const appliedMarker = ".brand-applied"

// apply runs the three steps over root and returns how many files changed.
func apply(root, brandPath string, b *Brand, check, renameOnly bool) (int, error) {
	if _, err := os.Stat(filepath.Join(root, appliedMarker)); err == nil {
		return 0, fmt.Errorf("%s is already branded (found %s); reset it first: git reset --hard && git clean -fd", root, appliedMarker)
	}
	changed := 0

	// 1. the mechanical rename, upstream's files only
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if fi.IsDir() {
			// our own layer, and other people's build droppings
			if rel != "." && skipDir(b, rel, fi.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !textFile(b, fi.Name()) {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out := rename(b, string(src))
		if out == string(src) {
			return nil
		}
		changed++
		if check {
			fmt.Println("would rewrite", rel)
			return nil
		}
		return os.WriteFile(p, []byte(out), fi.Mode())
	})
	if err != nil {
		return changed, err
	}
	if check {
		// patches and the overlay are files too; count them the blunt way
		if n := countLayerFiles(root, brandPath); n > 0 {
			changed += n
		}
		return changed, nil
	}
	if renameOnly {
		return changed, nil
	}

	// 2. patches against the renamed text, three-way so a moved hunk lands
	patches, _ := filepath.Glob(filepath.Join(brandPath, "patches", "*.patch"))
	for i, p := range patches {
		if abs, err := filepath.Abs(p); err == nil {
			patches[i] = abs
		}
	}
	sort.Strings(patches)
	if len(patches) > 0 {
		// three-way apply needs the pre-image blob, which is the renamed
		// file: staging writes it into the repository's object store
		stage := exec.Command("git", "add", "-A")
		stage.Dir = root
		_ = stage.Run()
	}
	for _, p := range patches {
		cmd := exec.Command("git", "apply", "--3way", "--whitespace=nowarn", p)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			return changed, fmt.Errorf("%s: %v\n%s", filepath.Base(p), err, out)
		}
		changed++
	}

	// 3. the overlay, last, as it is
	overlay := filepath.Join(brandPath, "_overlay")
	if _, err := os.Stat(overlay); err == nil {
		err = filepath.Walk(overlay, func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(overlay, p)
			src, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			dst := filepath.Join(root, rel)
			if old, err := os.ReadFile(dst); err == nil && string(old) == string(src) {
				return nil
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			changed++
			return os.WriteFile(dst, src, fi.Mode())
		})
		if err != nil {
			return changed, err
		}
	}
	if renameOnly {
		return changed, nil
	}
	return changed, os.WriteFile(filepath.Join(root, appliedMarker), []byte("brand layer applied; reset the tree to apply again\n"), 0o644)
}

// countLayerFiles is the -check estimate of steps 2 and 3.
func countLayerFiles(root, brandPath string) int {
	n := 0
	patches, _ := filepath.Glob(filepath.Join(brandPath, "patches", "*.patch"))
	n += len(patches)
	overlay := filepath.Join(brandPath, "_overlay")
	filepath.Walk(overlay, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(overlay, p)
		src, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		if old, err := os.ReadFile(filepath.Join(root, rel)); err != nil || string(old) != string(src) {
			fmt.Println("would write", filepath.ToSlash(rel))
			n++
		}
		return nil
	})
	return n
}

func skipDir(b *Brand, rel, name string) bool {
	for _, s := range b.Skip {
		if name == s || filepath.ToSlash(rel) == filepath.ToSlash(s) {
			return true
		}
	}
	return false
}

func textFile(b *Brand, name string) bool {
	for _, e := range b.Exts {
		if strings.HasSuffix(strings.ToLower(name), e) {
			return true
		}
	}
	// extensionless files the rename reads
	return name == "Makefile" || name == "Dockerfile" || name == "NOTICE"
}

// rename is the mechanical rename, with the protect list put aside first so
// "usemagpie.ai" survives an "magpie" -> "kestrel" pass.
func rename(b *Brand, s string) string {
	const sent = "\x00protect%d\x00"
	for i, k := range b.Protect {
		s = strings.ReplaceAll(s, k, fmt.Sprintf(sent, i))
	}
	if b.UpstreamModule != "" && b.Module != "" {
		s = strings.ReplaceAll(s, b.UpstreamModule, b.Module)
	}
	if b.UpstreamEnvPrefix != "" && b.EnvPrefix != "" {
		s = strings.ReplaceAll(s, b.UpstreamEnvPrefix, b.EnvPrefix)
	}
	// bare MAGPIE, as it is spelled in a doc sentence or a UI label
	if b.UpstreamEnvPrefix != "" && b.EnvPrefix != "" {
		s = strings.ReplaceAll(s, strings.TrimSuffix(b.UpstreamEnvPrefix, "_"), strings.TrimSuffix(b.EnvPrefix, "_"))
	}
	if b.UpstreamProviderID != "" && b.ProviderID != "" {
		s = strings.ReplaceAll(s, strings.ToUpper(b.UpstreamProviderID[:1])+b.UpstreamProviderID[1:], strings.ToUpper(b.ProviderID[:1])+b.ProviderID[1:])
		s = strings.ReplaceAll(s, b.UpstreamProviderID, b.ProviderID)
	}
	for i, k := range b.Protect {
		s = strings.ReplaceAll(s, fmt.Sprintf(sent, i), k)
	}
	return s
}

// inventoryReport sorts the tree into mechanical, hand and new: files the
// rename alone explains, files someone edited past it, and files upstream
// does not have. It reads only what differs from ref, through one
// git cat-file --batch, so it stays quick on a tree of ten thousand files.
func inventoryReport(root string, b *Brand, ref string) int {
	var mech, brandOnly, hand, gone, newFiles []string
	var changed []string
	status := git(root, "diff", "--name-status", "-z", "--no-renames", ref, "--")
	fields := strings.Split(status, "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		code, rel := fields[i], filepath.ToSlash(fields[i+1])
		if rel == "" || strings.HasPrefix(rel, brandDir+"/") || strings.HasPrefix(rel, ".github/") {
			continue
		}
		if strings.HasPrefix(code, "D") {
			gone = append(gone, rel)
			continue
		}
		changed = append(changed, rel)
	}
	specs := make([]string, len(changed))
	for i, rel := range changed {
		specs[i] = ref + ":" + rel
	}
	upstream := batchContents(root, specs)
	for i, rel := range changed {
		disk, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		up, ok := upstream[i]
		if !ok {
			// added by the rename (a swapped icon): binary work by hand
			hand = append(hand, rel+"  (added)")
			continue
		}
		if !textFile(b, filepath.Base(rel)) {
			hand = append(hand, rel+"  (binary)")
			continue
		}
		if string(disk) == rename(b, string(up)) {
			mech = append(mech, rel)
		} else if normalize(b, string(disk)) == normalize(b, string(up)) {
			// the same file with the brand spelled differently: the rename
			// rule (a new protect entry, say) accounts for it, not a hand
			brandOnly = append(brandOnly, rel)
		} else {
			hand = append(hand, rel)
		}
	}
	others := git(root, "ls-files", "--others", "--exclude-standard", "-z")
	for _, rel := range strings.Split(others, "\x00") {
		rel = filepath.ToSlash(rel)
		if rel == "" || strings.HasPrefix(rel, brandDir+"/") {
			continue
		}
		newFiles = append(newFiles, rel)
	}
	sort.Strings(mech)
	sort.Strings(brandOnly)
	sort.Strings(hand)
	sort.Strings(gone)
	sort.Strings(newFiles)
	fmt.Printf("mechanical (the rename explains these): %d\n", len(mech))
	fmt.Printf("brand-only (same file, brand spelled another way): %d\n", len(brandOnly))
	for _, f := range brandOnly {
		fmt.Println("  B", f)
	}
	fmt.Printf("hand (edited beyond the brand):         %d\n", len(hand))
	for _, f := range hand {
		fmt.Println("  H", f)
	}
	fmt.Printf("new (upstream has no such file):        %d\n", len(newFiles))
	for _, f := range newFiles {
		fmt.Println("  N", f)
	}
	fmt.Printf("deleted/changed-type:                   %d\n", len(gone))
	for _, f := range gone {
		fmt.Println("  D", f)
	}
	return 0
}

// normalize drops every spelling of the brand, upstream's and ours, so two
// files that differ only in which name they carry compare equal.
func normalize(b *Brand, s string) string {
	tokens := []string{
		b.UpstreamModule, b.Module,
		b.UpstreamEnvPrefix, b.EnvPrefix,
		strings.TrimSuffix(b.UpstreamEnvPrefix, "_"), strings.TrimSuffix(b.EnvPrefix, "_"),
	}
	for _, id := range []string{b.UpstreamProviderID, b.ProviderID} {
		if id == "" {
			continue
		}
		tokens = append(tokens, id, strings.ToUpper(id[:1])+id[1:], strings.ToUpper(id))
	}
	for _, t := range tokens {
		if t != "" {
			s = strings.ReplaceAll(s, t, "")
		}
	}
	return s
}

// batchContents reads many git objects in one process: specs are
// "<rev>:<path>" names, the result is indexed as the specs were given, a
// missing object is absent from the map.
func batchContents(root string, specs []string) map[int][]byte {
	out := map[int][]byte{}
	if len(specs) == 0 {
		return out
	}
	cmd := exec.Command("git", "cat-file", "--batch")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(strings.Join(specs, "\n") + "\n")
	raw, err := cmd.Output()
	if err != nil {
		return out
	}
	r := bufio.NewReader(bytes.NewReader(raw))
	for i := range specs {
		line, err := r.ReadString('\n')
		if err != nil {
			break
		}
		f := strings.Fields(line)
		if len(f) < 3 {
			continue // "<spec> missing"
		}
		n, err := strconv.Atoi(f[2])
		if err != nil {
			continue
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(r, buf); err != nil {
			break
		}
		r.ReadByte() // the newline cat-file puts after each object
		out[i] = buf
	}
	return out
}

// git runs a git command in root and returns its stdout, or "" on error.
func git(root string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

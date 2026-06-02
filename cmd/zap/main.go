// Command zap bundles the full restic CLI in a single binary and adds
// per-git-repo fast snapshot helpers (a Go port of the original "rundo" tool).
//
// restic is compiled directly into zap; there is no external restic dependency.
// Every invocation defaults to restic's --fast mode (see README-FAST.zh-CN.md)
// unless it is explicitly disabled with --fast=false.
//
// Usage:
//
//	zap init                       initialize the snapshot repo under .git/zap-restic
//	zap save [label]               snapshot the working tree (default label: manual)
//	zap list [-n N]                list recent snapshots (default 20)
//	zap diff [snapshot]            diff a snapshot against the working tree (default: latest)
//	zap diff2 <snap_a> <snap_b>    diff two snapshots
//	zap restore <snapshot> [paths] restore the whole tree, or only the given paths
//	zap check                      verify repository integrity
//	zap restic <args...>           run the full restic CLI (also defaults to --fast)
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/restic/restic/internal/app"
	"github.com/restic/restic/internal/global"
)

// defaultExcludeNames are directories excluded from snapshots by default.
var defaultExcludeNames = []string{
	".git",
	"node_modules",
	".venv",
	"venv",
	"__pycache__",
	".pytest_cache",
	".mypy_cache",
	".ruff_cache",
	".next",
	".turbo",
	"dist",
	"build",
	"target",
}

func die(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "zap: "+format+"\n", args...)
	os.Exit(2)
}

// fastPreExec turns restic's --fast flag on by default. The user can still
// disable it explicitly with --fast=false.
func fastPreExec(root *cobra.Command, _ *global.Options) {
	if f := root.PersistentFlags().Lookup("fast"); f != nil {
		_ = f.Value.Set("true")
		f.DefValue = "true"
	}
}

// runRestic executes the embedded restic CLI with the given args (no binary
// name), inheriting stdio. It returns restic's exit code.
func runRestic(args []string) int {
	return app.Run(args, os.Stdin, os.Stdout, os.Stderr, fastPreExec)
}

// mustRestic runs the embedded restic CLI and exits the process on failure.
func mustRestic(args []string) {
	if code := runRestic(args); code != 0 {
		os.Exit(code)
	}
}

// captureRestic runs the embedded restic CLI capturing stdout (used for
// machine-readable output like `snapshots --json`).
func captureRestic(args []string) (string, int) {
	var buf bytes.Buffer
	code := app.Run(args, os.Stdin, &buf, os.Stderr, fastPreExec)
	return buf.String(), code
}

// runExternal runs an external command (git, diff, rsync, cp), optionally
// capturing stdout.
func runExternal(args []string, capture bool) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	if capture {
		var out strings.Builder
		cmd.Stdout = &out
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		return out.String(), err
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return "", cmd.Run()
}

func mustExternal(args []string) {
	if _, err := runExternal(args, false); err != nil {
		die("command failed: %s: %v", strings.Join(args, " "), err)
	}
}

func gitRoot() string {
	out, err := runExternal([]string{"git", "rev-parse", "--show-toplevel"}, true)
	if err != nil {
		die("not inside a git repository")
	}
	root, err := filepath.Abs(strings.TrimSpace(out))
	if err != nil {
		die("cannot resolve git root: %v", err)
	}
	return root
}

// gitPath resolves a name relative to the repository's git directory, e.g.
// "zap-restic" -> /path/to/repo/.git/zap-restic.
func gitPath(name string) string {
	root := gitRoot()
	out, err := runExternal([]string{"git", "-C", root, "rev-parse", "--git-path", name}, true)
	if err != nil {
		die("git rev-parse --git-path %s failed: %v", name, err)
	}
	p := strings.TrimSpace(out)
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		die("cannot resolve git path: %v", err)
	}
	return abs
}

func metaDir() string      { return gitPath("zap-restic") }
func resticRepo() string   { return filepath.Join(metaDir(), "repo") }
func passwordFile() string { return filepath.Join(metaDir(), "password") }
func excludeFile() string  { return filepath.Join(metaDir(), "excludes.txt") }

// repoHostID derives a stable restic host identifier from the repo path so that
// snapshots from different working trees do not collide.
func repoHostID(root string) string {
	sum := sha1.Sum([]byte(root))
	h := hex.EncodeToString(sum[:])[:12]
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	return fmt.Sprintf("zap-%s-%s", host, h)
}

// resticBaseArgs are the global restic flags shared by every zap subcommand.
func resticBaseArgs() []string {
	return []string{"-r", resticRepo(), "--password-file", passwordFile()}
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func ensureInitialized() {
	if !exists(resticRepo()) || !exists(passwordFile()) {
		die("not initialized. Run: zap init")
	}
}

func cmdInit() {
	if err := os.MkdirAll(metaDir(), 0o700); err != nil {
		die("cannot create meta dir: %v", err)
	}

	if !exists(passwordFile()) {
		buf := make([]byte, 48)
		if _, err := rand.Read(buf); err != nil {
			die("cannot generate password: %v", err)
		}
		pw := base64.RawURLEncoding.EncodeToString(buf) + "\n"
		if err := os.WriteFile(passwordFile(), []byte(pw), 0o600); err != nil {
			die("cannot write password file: %v", err)
		}
	}

	if !exists(excludeFile()) {
		root := gitRoot()
		var lines []string
		for _, name := range defaultExcludeNames {
			lines = append(lines, filepath.Join(root, name))
		}
		content := strings.Join(lines, "\n") + "\n"
		if err := os.WriteFile(excludeFile(), []byte(content), 0o644); err != nil {
			die("cannot write exclude file: %v", err)
		}
	}

	if !exists(resticRepo()) {
		mustRestic(append(resticBaseArgs(), "init"))
	} else {
		fmt.Printf("zap: restic repo already exists: %s\n", resticRepo())
	}

	fmt.Println("zap initialized:")
	fmt.Printf("  repo:      %s\n", resticRepo())
	fmt.Printf("  password:  %s\n", passwordFile())
	fmt.Printf("  excludes:  %s\n", excludeFile())
}

func cmdSave(args []string) {
	ensureInitialized()
	root := gitRoot()
	label := "manual"
	if len(args) > 0 && args[0] != "" {
		label = args[0]
	}

	cmd := append(resticBaseArgs(),
		"backup",
		root,
		"--host", repoHostID(root),
		"--tag", "zap",
		"--tag", label,
		"--skip-if-unchanged",
		"--exclude-file", excludeFile(),
	)
	mustRestic(cmd)

	if latest := latestSnapshotID(); latest != "" {
		fmt.Printf("zap snapshot saved: %s\n", latest)
	}
}

type snapshot struct {
	ID      string   `json:"id"`
	Time    string   `json:"time"`
	Tags    []string `json:"tags"`
	Summary *struct {
		FilesNew     *int `json:"files_new"`
		FilesChanged *int `json:"files_changed"`
	} `json:"summary"`
}

func snapshotsJSON() []snapshot {
	ensureInitialized()
	root := gitRoot()
	out, code := captureRestic(append(resticBaseArgs(),
		"snapshots",
		"--json",
		"--host", repoHostID(root),
		"--path", root,
		"--tag", "zap",
	))
	if code != 0 {
		os.Exit(code)
	}
	if strings.TrimSpace(out) == "" {
		return nil
	}
	var snaps []snapshot
	if err := json.Unmarshal([]byte(out), &snaps); err != nil {
		die("cannot parse snapshots json: %v", err)
	}
	return snaps
}

func latestSnapshotID() string {
	snaps := snapshotsJSON()
	if len(snaps) == 0 {
		return ""
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Time < snaps[j].Time })
	return snaps[len(snaps)-1].ID
}

func resolveSnapshot(s string) string {
	if s == "latest" {
		sid := latestSnapshotID()
		if sid == "" {
			die("no snapshots")
		}
		return sid
	}
	return s
}

func cmdList(limit int) {
	snaps := snapshotsJSON()
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Time > snaps[j].Time })

	for i, s := range snaps {
		if i >= limit {
			break
		}
		sid := s.ID
		if len(sid) > 12 {
			sid = sid[:12]
		}
		tags := strings.Join(s.Tags, ",")
		filesNew, filesChanged := "?", "?"
		if s.Summary != nil {
			if s.Summary.FilesNew != nil {
				filesNew = fmt.Sprintf("%d", *s.Summary.FilesNew)
			}
			if s.Summary.FilesChanged != nil {
				filesChanged = fmt.Sprintf("%d", *s.Summary.FilesChanged)
			}
		}
		fmt.Printf("%02d  %s  %s  tags=[%s]  new=%s changed=%s\n",
			i+1, sid, s.Time, tags, filesNew, filesChanged)
	}
}

func mkTempDir(prefix string) string {
	tmp, err := os.MkdirTemp("", prefix)
	if err != nil {
		die("cannot create temp dir: %v", err)
	}
	return tmp
}

func restoreSnapshotToTmp(snap string) string {
	ensureInitialized()
	root := gitRoot()
	sid := resolveSnapshot(snap)
	tmp := mkTempDir("zap-restore-")
	// Restore only the project root contents, not the full absolute path hierarchy.
	mustRestic(append(resticBaseArgs(), "restore", sid+":"+root, "--target", tmp))
	return tmp
}

func cmdDiff(snap string) {
	ensureInitialized()
	root := gitRoot()
	tmp := restoreSnapshotToTmp(snap)
	defer os.RemoveAll(tmp)

	diffArgs := []string{"diff", "-ruN"}
	for _, name := range []string{
		".git", "node_modules", ".venv", "venv",
		"__pycache__", ".pytest_cache", ".mypy_cache", ".ruff_cache",
	} {
		diffArgs = append(diffArgs, "-x", name)
	}
	diffArgs = append(diffArgs, tmp, root)

	cmd := exec.Command(diffArgs[0], diffArgs[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.RemoveAll(tmp)
			os.Exit(exitErr.ExitCode())
		}
		die("diff failed: %v", err)
	}
}

func cmdDiff2(a, b string) {
	ensureInitialized()
	sid1 := resolveSnapshot(a)
	sid2 := resolveSnapshot(b)
	mustRestic(append(resticBaseArgs(), "diff", sid1, sid2))
}

func safeRelPath(p string) string {
	if filepath.IsAbs(p) {
		die("unsafe path: %s", p)
	}
	clean := filepath.Clean(p)
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		if part == ".." {
			die("unsafe path: %s", p)
		}
	}
	return clean
}

func removeDst(dst string) {
	info, err := os.Lstat(dst)
	if err != nil {
		return
	}
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		os.RemoveAll(dst)
	} else {
		os.Remove(dst)
	}
}

func copyPath(src, dst string) {
	removeDst(dst)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		die("cannot create dir %s: %v", filepath.Dir(dst), err)
	}
	// cp -a preserves symlinks, modes and timestamps, matching rundo's behavior.
	mustExternal([]string{"cp", "-a", src, dst})
}

func restorePartial(snap string, paths []string) {
	ensureInitialized()
	root := gitRoot()
	sid := resolveSnapshot(snap)

	rels := make([]string, 0, len(paths))
	for _, p := range paths {
		rels = append(rels, safeRelPath(p))
	}

	tmp := mkTempDir("zap-partial-")
	defer os.RemoveAll(tmp)

	cmd := append(resticBaseArgs(), "restore", sid+":"+root, "--target", tmp)
	for _, rel := range rels {
		// After restoring snapshot:/project-root, include paths are relative to
		// that subfolder using an absolute-looking form.
		cmd = append(cmd, "--include", "/"+filepath.ToSlash(rel))
	}
	mustRestic(cmd)

	for _, rel := range rels {
		src := filepath.Join(tmp, rel)
		dst := filepath.Join(root, rel)

		if exists(src) {
			copyPath(src, dst)
			fmt.Printf("restored: %s\n", rel)
		} else {
			// The snapshot does not contain this file, so restoring to that
			// state means deleting it.
			if exists(dst) {
				removeDst(dst)
				fmt.Printf("removed:  %s\n", rel)
			} else {
				fmt.Printf("absent:   %s\n", rel)
			}
		}
	}
}

func restoreFull(snap string) {
	ensureInitialized()
	root := gitRoot()
	tmp := restoreSnapshotToTmp(snap)
	defer os.RemoveAll(tmp)

	// Mirror restore via rsync; keep .git and common cache dirs intact.
	cmd := []string{"rsync", "-a", "--delete"}
	for _, name := range []string{
		".git", "node_modules", ".venv", "venv",
		"__pycache__", ".pytest_cache", ".mypy_cache", ".ruff_cache",
	} {
		cmd = append(cmd, "--exclude", name)
	}
	cmd = append(cmd, tmp+"/", root+"/")
	mustExternal(cmd)
	fmt.Printf("restored full workspace from snapshot: %.12s\n", resolveSnapshot(snap))
}

func cmdRestore(snap string, paths []string) {
	if len(paths) > 0 {
		restorePartial(snap, paths)
	} else {
		restoreFull(snap)
	}
}

func cmdCheck() {
	ensureInitialized()
	mustRestic(append(resticBaseArgs(), "check"))
}

func usage() {
	fmt.Fprint(os.Stderr, `zap - per-git-repo fast snapshots with restic built in

usage:
  zap init                       initialize the snapshot repo under .git/zap-restic
  zap save [label]               snapshot the working tree (default label: manual)
  zap list [-n N]                list recent snapshots (default 20)
  zap diff [snapshot]            diff a snapshot against the working tree (default: latest)
  zap diff2 <snap_a> <snap_b>    diff two snapshots
  zap restore <snapshot> [paths] restore the whole tree, or only the given paths
  zap check                      verify repository integrity
  zap restic <args...>           run the full restic CLI

restic is compiled in and defaults to --fast mode; disable with --fast=false.
`)
	os.Exit(2)
}

// parseLimit extracts -n/--limit from list args, defaulting to 20.
func parseLimit(args []string) int {
	limit := 20
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-n" || a == "--limit":
			if i+1 >= len(args) {
				die("missing value for %s", a)
			}
			fmt.Sscanf(args[i+1], "%d", &limit)
			i++
		case strings.HasPrefix(a, "-n"):
			fmt.Sscanf(a[2:], "%d", &limit)
		case strings.HasPrefix(a, "--limit="):
			fmt.Sscanf(a[len("--limit="):], "%d", &limit)
		}
	}
	return limit
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "init":
		cmdInit()
	case "save":
		cmdSave(args)
	case "list":
		cmdList(parseLimit(args))
	case "diff":
		snap := "latest"
		if len(args) > 0 {
			snap = args[0]
		}
		cmdDiff(snap)
	case "diff2":
		if len(args) < 2 {
			die("diff2 requires two snapshots")
		}
		cmdDiff2(args[0], args[1])
	case "restore":
		if len(args) < 1 {
			die("restore requires a snapshot")
		}
		cmdRestore(args[0], args[1:])
	case "check":
		cmdCheck()
	case "restic":
		// Passthrough to the full restic CLI (also defaults to --fast).
		os.Exit(runRestic(args))
	case "-h", "--help", "help":
		usage()
	default:
		die("unknown command: %s", cmd)
	}
}

package sync

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rileyhilliard/rr/internal/errors"
)

// PullInPlace is Pull for a command that ran in place (on a local host, or
// locally with --local or a fallback): its files are already on this
// machine, in srcDir, so pull patterns are copied from there with a local
// rsync. A destination that is srcDir itself is skipped, since the files are
// already there. It returns how many destinations got a copy, so the caller
// can report a pull that copied nothing as skipped.
func PullInPlace(srcDir string, opts PullOptions, progress io.Writer) (int, error) {
	if len(opts.Patterns) == 0 {
		return 0, nil
	}
	src, err := filepath.Abs(srcDir)
	if err != nil {
		return 0, errors.WrapWithCode(err, errors.ErrSync,
			fmt.Sprintf("Couldn't resolve %s", srcDir),
			"Check that the project directory exists.")
	}

	groups := groupByDest(opts.Patterns, opts.DefaultDest)
	dests := inPlaceDests(src, groups)
	if len(dests) == 0 {
		return 0, nil
	}

	rsyncPath, err := FindRsync()
	if err != nil {
		return 0, err
	}

	for _, dest := range dests {
		var sources []string
		for _, pattern := range groups[dest] {
			matches, err := inPlaceSources(src, pattern)
			if err != nil {
				return 0, err
			}
			sources = append(sources, matches...)
		}
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return 0, errors.WrapWithCode(err, errors.ErrSync,
				fmt.Sprintf("Couldn't create destination directory %s", dest),
				"Check file permissions.")
		}
		args := append([]string{"-a"}, opts.Flags...)
		args = append(args, sources...)
		args = append(args, filepath.Clean(dest)+string(filepath.Separator))
		if err := runRsyncPull(rsyncPath, args, "local", progress); err != nil {
			return 0, err
		}
	}
	return len(dests), nil
}

// InPlaceCopyNeeded reports whether PullInPlace would copy anything: whether
// any of the pull's destinations is somewhere other than srcDir.
func InPlaceCopyNeeded(srcDir string, opts PullOptions) bool {
	src, err := filepath.Abs(srcDir)
	if err != nil {
		return true
	}
	return len(inPlaceDests(src, groupByDest(opts.Patterns, opts.DefaultDest))) > 0
}

// inPlaceDests returns the destinations in groups other than src, sorted.
func inPlaceDests(src string, groups map[string][]string) []string {
	dests := make([]string, 0, len(groups))
	for dest := range groups {
		if !sameDir(dest, src) {
			dests = append(dests, dest)
		}
	}
	sort.Strings(dests)
	return dests
}

// inPlaceSources expands pattern against dir the way the remote shell
// expands it for a remote pull. A trailing slash is kept, so "dir/" copies
// the directory's contents, as it does remotely.
func inPlaceSources(dir, pattern string) ([]string, error) {
	trailing := strings.HasSuffix(pattern, "/")
	full := filepath.Join(dir, pattern)
	matches, err := filepath.Glob(full)
	if err != nil || len(matches) == 0 {
		return nil, errors.New(errors.ErrSync,
			fmt.Sprintf("Nothing matches %s in %s", pattern, dir),
			"Check that the command wrote the file, and that the path is relative to the project directory.")
	}
	if trailing {
		for i := range matches {
			matches[i] += string(filepath.Separator)
		}
	}
	return matches, nil
}

// sameDir reports whether dest (relative to the cwd) is dir.
func sameDir(dest, dir string) bool {
	abs, err := filepath.Abs(dest)
	if err != nil {
		return false
	}
	return resolvePath(abs) == resolvePath(dir)
}

func resolvePath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

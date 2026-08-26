package sources

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
)

// maxEntrySize caps a single decompressed member. Every source here is a
// release tarball of known shape; a member in the gigabytes means a zip bomb or
// a corrupt archive, not a Python release.
const maxEntrySize = 4 << 30

// Extract unpacks archive into dst, stripping s.TopDir so the tree is flat.
//
// Pure Go on purpose: shelling out to tar makes the build depend on which tar
// the host happens to have, and busybox tar and GNU tar disagree on enough
// flags to matter. static-python's sources are all gzip or zip, so xz is
// deliberately not supported rather than pulling in a decoder for it.
//
// dst must be absent or an empty directory (the job stage is one). On any
// failure it is emptied again, so a half-extracted tree is never left for a
// later run to mistake for a good one.
func Extract(ctx context.Context, archive, dst string, s config.Source) error {
	created := false
	switch ents, err := os.ReadDir(dst); {
	case err == nil && len(ents) > 0:
		return fmt.Errorf("sources: extract destination %s is not empty", dst)
	case err == nil:
	case os.IsNotExist(err):
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return fmt.Errorf("sources: creating %s: %w", dst, err)
		}
		created = true
	default:
		return err
	}
	ok := false
	defer func() {
		if ok {
			return
		}
		if created {
			os.RemoveAll(dst)
			return
		}
		if ents, err := os.ReadDir(dst); err == nil {
			for _, e := range ents {
				os.RemoveAll(filepath.Join(dst, e.Name()))
			}
		}
	}()

	var n int
	var err error
	switch {
	case hasSuffix(archive, ".tar.gz", ".tgz"):
		n, err = extractTar(ctx, archive, dst, s, true)
	case hasSuffix(archive, ".tar"):
		n, err = extractTar(ctx, archive, dst, s, false)
	case hasSuffix(archive, ".zip"):
		n, err = extractZip(ctx, archive, dst, s)
	default:
		err = fmt.Errorf("sources: %s: unsupported archive format (only .tar.gz, .tgz, .tar and .zip are supported)", filepath.Base(archive))
	}
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("sources: extracting %s into %s produced an empty tree (wrong topdir %q?)", archive, dst, s.TopDir)
	}
	ok = true
	return nil
}

func hasSuffix(name string, suffixes ...string) bool {
	lower := strings.ToLower(name)
	for _, sfx := range suffixes {
		if strings.HasSuffix(lower, sfx) {
			return true
		}
	}
	return false
}

func extractTar(ctx context.Context, archive, dst string, s config.Source, compressed bool) (int, error) {
	f, err := os.Open(archive)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var r io.Reader = f
	if compressed {
		zr, err := gzip.NewReader(f)
		if err != nil {
			return 0, fmt.Errorf("sources: gzip %s: %w", archive, err)
		}
		defer zr.Close()
		r = zr
	}

	tr := tar.NewReader(r)
	written := 0
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("sources: reading %s: %w", archive, err)
		}
		switch hdr.Typeflag {
		case tar.TypeXGlobalHeader, tar.TypeXHeader:
			continue
		}
		name, keep, err := strip(hdr.Name, s.TopDir)
		if err != nil {
			return 0, fmt.Errorf("sources: %s: %w", archive, err)
		}
		if !keep {
			continue
		}
		target, err := resolve(dst, name)
		if err != nil {
			return 0, fmt.Errorf("sources: %s: %w", archive, err)
		}
		mode := hdr.FileInfo().Mode()
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return 0, err
			}
		case tar.TypeReg:
			if hdr.Size > maxEntrySize {
				return 0, fmt.Errorf("sources: %s: entry %s is %d bytes, refusing", archive, hdr.Name, hdr.Size)
			}
			if err := writeFile(target, tr, filePerm(mode)); err != nil {
				return 0, err
			}
		case tar.TypeSymlink:
			if err := writeSymlink(dst, target, hdr.Linkname); err != nil {
				return 0, fmt.Errorf("sources: %s: %w", archive, err)
			}
		case tar.TypeLink:
			linkName, keep, err := strip(hdr.Linkname, s.TopDir)
			if err != nil || !keep {
				return 0, fmt.Errorf("sources: %s: hard link %s points outside the tree", archive, hdr.Name)
			}
			linkTarget, err := resolve(dst, linkName)
			if err != nil {
				return 0, fmt.Errorf("sources: %s: %w", archive, err)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return 0, err
			}
			os.Remove(target)
			if err := os.Link(linkTarget, target); err != nil {
				return 0, fmt.Errorf("sources: linking %s: %w", target, err)
			}
		default:
			// Devices, fifos and sockets have no business in a source release.
			continue
		}
		written++
	}
	return written, nil
}

func extractZip(ctx context.Context, archive, dst string, s config.Source) (int, error) {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return 0, fmt.Errorf("sources: opening %s: %w", archive, err)
	}
	defer zr.Close()

	written := 0
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		name, keep, err := strip(f.Name, s.TopDir)
		if err != nil {
			return 0, fmt.Errorf("sources: %s: %w", archive, err)
		}
		if !keep {
			continue
		}
		target, err := resolve(dst, name)
		if err != nil {
			return 0, fmt.Errorf("sources: %s: %w", archive, err)
		}
		mode := f.Mode()
		switch {
		case f.FileInfo().IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return 0, err
			}
		case mode&os.ModeSymlink != 0:
			link, err := readZipEntry(f)
			if err != nil {
				return 0, err
			}
			if err := writeSymlink(dst, target, string(link)); err != nil {
				return 0, fmt.Errorf("sources: %s: %w", archive, err)
			}
		case mode.IsRegular():
			if f.UncompressedSize64 > maxEntrySize {
				return 0, fmt.Errorf("sources: %s: entry %s is %d bytes, refusing", archive, f.Name, f.UncompressedSize64)
			}
			rc, err := f.Open()
			if err != nil {
				return 0, err
			}
			err = writeFile(target, rc, filePerm(mode))
			rc.Close()
			if err != nil {
				return 0, err
			}
		default:
			continue
		}
		written++
	}
	return written, nil
}

func readZipEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, 4096))
}

// An entry outside topDir is reported rather than silently skipped: a pin
// whose topdir is wrong should fail, not produce a tree missing half its files.
func strip(name, topDir string) (string, bool, error) {
	clean := path.Clean(strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(name, `\`, "/")), "/"))
	if clean == "." || clean == "" {
		return "", false, nil
	}
	if isPaxMetadata(clean) {
		return "", false, nil
	}
	if topDir == "" {
		return clean, true, nil
	}
	top := path.Clean(topDir)
	if clean == top {
		return "", false, nil
	}
	rest, ok := strings.CutPrefix(clean, top+"/")
	if !ok {
		return "", false, fmt.Errorf("entry %q is outside the declared topdir %q", name, topDir)
	}
	return rest, true, nil
}

// Extended-header entries that tar implementations write next to the real
// tree. They are archive metadata, not content, and would otherwise trip the
// outside-topdir check on tarballs produced by git archive or bsdtar.
func isPaxMetadata(clean string) bool {
	for _, part := range strings.Split(clean, "/") {
		if part == "pax_global_header" || part == "@PaxHeader" || strings.HasPrefix(part, "PaxHeaders") {
			return true
		}
	}
	return false
}

// An archive that escapes its destination -- via "../" or an absolute name --
// is rejected outright rather than sanitised: such an entry is hostile or
// corrupt, and neither should be allowed to write a single byte.
func resolve(dst, name string) (string, error) {
	if path.IsAbs(name) || filepath.IsAbs(name) {
		return "", fmt.Errorf("entry %q has an absolute path", name)
	}
	target := filepath.Join(dst, filepath.FromSlash(name))
	if !within(dst, target) {
		return "", fmt.Errorf("entry %q escapes the destination directory", name)
	}
	return target, nil
}

func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// writeSymlink refuses a link that would point out of the tree. Even a link
// that is never followed by us is followed by configure and by make, which
// would then read or clobber a host path.
func writeSymlink(dst, target, link string) error {
	if link == "" {
		return fmt.Errorf("symlink %s has an empty target", target)
	}
	if filepath.IsAbs(link) {
		return fmt.Errorf("symlink %s points at absolute path %q", target, link)
	}
	if !within(dst, filepath.Join(filepath.Dir(target), filepath.FromSlash(link))) {
		return fmt.Errorf("symlink %s points outside the tree at %q", target, link)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	os.Remove(target)
	return os.Symlink(filepath.FromSlash(link), target)
}

func writeFile(target string, r io.Reader, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("sources: creating %s: %w", target, err)
	}
	if _, err := io.Copy(f, io.LimitReader(r, maxEntrySize+1)); err != nil {
		f.Close()
		return fmt.Errorf("sources: writing %s: %w", target, err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	// O_CREATE honours umask, so set the mode explicitly to keep the
	// executable bit that configure scripts depend on.
	return os.Chmod(target, perm)
}

// Only the executable bit is carried over; group and other write bits from an
// upstream archive are not something we want on disk.
func filePerm(m os.FileMode) os.FileMode {
	if m&0o100 != 0 {
		return 0o755
	}
	return 0o644
}

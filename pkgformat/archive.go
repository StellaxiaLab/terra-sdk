package pkgformat

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// PackageExtension is the .tmod file suffix (D-1).
const PackageExtension = ".tmod"

// ArchiveLimits caps what an archive may claim before extraction refuses it.
type ArchiveLimits struct {
	// MaxEntries caps the number of files in the archive.
	MaxEntries int
	// MaxTotalBytes caps the total uncompressed payload size.
	MaxTotalBytes int64
	// MaxManifestBytes caps the module.json entry alone, so judging a package
	// never requires unbounded memory.
	MaxManifestBytes int64
	// MaxCompressionRatio flags a zip bomb: extraction fails once the bytes
	// written exceed ratio × archive-file-size (checked above a small floor so
	// legitimately tiny, highly compressible packages are not punished).
	MaxCompressionRatio int64
}

// DefaultArchiveLimits are deliberately generous for real modules and
// hopeless for bombs.
func DefaultArchiveLimits() ArchiveLimits {
	return ArchiveLimits{
		MaxEntries:          10_000,
		MaxTotalBytes:       2 << 30, // 2 GiB
		MaxManifestBytes:    4 << 20, // 4 MiB
		MaxCompressionRatio: 200,
	}
}

// ratioCheckFloor is the uncompressed size below which the compression-ratio
// check does not apply.
const ratioCheckFloor int64 = 32 << 20

// WritePackage archives the payload files under root into a .tmod at dest,
// with manifestRaw as the module.json entry (first in the archive). The
// source directory's own module.json is not archived — manifestRaw replaces
// it. Entry timestamps are fixed so identical content yields identical bytes:
// the package digest is its identity , and a wall clock would
// break that.
func WritePackage(root string, manifestRaw []byte, dest string) (err error) {
	if len(manifestRaw) == 0 {
		return errors.New("manifest bytes are required")
	}
	payloadPaths, err := collectPackagePaths(root)
	if err != nil {
		return err
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(dest)
		}
	}()

	writer := zip.NewWriter(out)
	epoch := time.Unix(0, 0).UTC()

	manifestHeader := &zip.FileHeader{Name: ManifestFileName, Method: zip.Deflate, Modified: epoch}
	manifestHeader.SetMode(0o644)
	manifestEntry, err := writer.CreateHeader(manifestHeader)
	if err != nil {
		return err
	}
	if _, err := manifestEntry.Write(manifestRaw); err != nil {
		return err
	}

	for _, relative := range payloadPaths {
		source := filepath.Join(root, filepath.FromSlash(relative))
		info, err := os.Stat(source)
		if err != nil {
			return err
		}
		header := &zip.FileHeader{Name: relative, Method: zip.Deflate, Modified: epoch}
		header.SetMode(info.Mode().Perm())
		entry, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		file, err := os.Open(source)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(entry, file)
		if closeErr := file.Close(); copyErr == nil {
			copyErr = closeErr
		}
		if copyErr != nil {
			return copyErr
		}
	}
	return writer.Close()
}

// OpenPackageManifest reads and validates just the manifest from a .tmod,
// without extracting anything. It returns the decoded manifest and the raw
// manifest bytes — the raw form is what a signature verifier needs.
func OpenPackageManifest(packagePath string, limits ArchiveLimits) (Manifest, []byte, error) {
	reader, err := zip.OpenReader(packagePath)
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("open package: %w", err)
	}
	defer reader.Close()

	if limits.MaxEntries > 0 && len(reader.File) > limits.MaxEntries {
		return Manifest{}, nil, fmt.Errorf("package has %d entries (limit %d)", len(reader.File), limits.MaxEntries)
	}
	for _, entry := range reader.File {
		if entry.Name != ManifestFileName {
			continue
		}
		if limits.MaxManifestBytes > 0 && entry.UncompressedSize64 > uint64(limits.MaxManifestBytes) {
			return Manifest{}, nil, fmt.Errorf("%s exceeds the manifest size limit", ManifestFileName)
		}
		opened, err := entry.Open()
		if err != nil {
			return Manifest{}, nil, err
		}
		limit := limits.MaxManifestBytes
		if limit <= 0 {
			limit = DefaultArchiveLimits().MaxManifestBytes
		}
		raw, err := io.ReadAll(io.LimitReader(opened, limit+1))
		opened.Close()
		if err != nil {
			return Manifest{}, nil, err
		}
		if int64(len(raw)) > limit {
			return Manifest{}, nil, fmt.Errorf("%s exceeds the manifest size limit", ManifestFileName)
		}
		manifest, _, err := DecodeManifest(raw)
		if err != nil {
			return Manifest{}, nil, err
		}
		return manifest, raw, nil
	}
	return Manifest{}, nil, fmt.Errorf("package has no %s", ManifestFileName)
}

// ArchiveInspection is what one judgment pass learned about an archive. The
// two size fields are deliberately separate: DeclaredBytes is what the zip
// central directory claims, MeasuredBytes is what the decompressors actually
// produced. On a successful inspection they are equal — anything else is an
// error, never a quiet result — so the pair is a statement about the archive
// having been checked, not a discrepancy to interpret.
type ArchiveInspection struct {
	// Entries is every zip entry, directories and manifest included.
	Entries int
	// ArchiveBytes is the .tmod's own file size — the ratio denominator.
	ArchiveBytes int64
	// DeclaredBytes is the claimed total uncompressed payload size.
	DeclaredBytes int64
	// MeasuredBytes is what actually came out of the decompressors.
	MeasuredBytes int64
}

// InspectPackage judges an archive against limits without writing a byte of
// payload anywhere. It exists so a registry can refuse a bomb at the door
// instead of storing it, mirroring it into every child tree, and letting each
// node in the subtree discover it at install time.
//
// Two passes:
//
//  1. The central directory's own account of itself — entry count, names,
//     duplicate and symlink refusal, and the declared total against the size
//     and ratio caps. No decompression at all.
//  2. Every entry streamed into io.Discard through copyWithBudget, which caps
//     against bytes ACTUALLY produced.
//
// Pass 1 is sound, not merely a cheap pre-filter, and the reason is worth
// stating because it is easy to assume the opposite: an archive cannot
// decompress to more than it declares. Go's zip reader refuses to hand out
// the overrun byte — archive/zip reader.go, checksumReader.Read returns
// ErrFormat once nread exceeds UncompressedSize64. So an understating header
// is not a way past the size cap; it is a way to make the archive unreadable.
//
// Pass 2 therefore proves a different thing than "the declaration was true":
// it proves the payload is really there and really decompresses. Corrupt,
// truncated, CRC-broken, and understating archives all die here rather than
// on a node. Its cost is bounded twice over — by the budget and by pass 1
// having already capped the declaration — but bounded is not free: it moves
// cost from disk to CPU, and what bounds the CPU across many uploads is the
// publish permission (U1) and the uploader quota (U3), not this function.
//
// The judgment deliberately mirrors ExtractPackage's — same rules in the same
// order — so nothing inspect accepts can fail extraction for a reason inspect
// could have seen.
func InspectPackage(packagePath string, limits ArchiveLimits) (ArchiveInspection, error) {
	archiveInfo, err := os.Stat(packagePath)
	if err != nil {
		return ArchiveInspection{}, err
	}
	reader, err := zip.OpenReader(packagePath)
	if err != nil {
		return ArchiveInspection{}, fmt.Errorf("open package: %w", err)
	}
	defer reader.Close()

	inspection := ArchiveInspection{Entries: len(reader.File), ArchiveBytes: archiveInfo.Size()}
	if limits.MaxEntries > 0 && len(reader.File) > limits.MaxEntries {
		return inspection, fmt.Errorf("package has %d entries (limit %d)", len(reader.File), limits.MaxEntries)
	}

	// Pass 1 — what the archive says about itself.
	var declared uint64
	seen := map[string]bool{}
	for _, entry := range reader.File {
		name := entry.Name
		if name != ManifestFileName {
			if err := validatePackagePath(name); err != nil {
				return inspection, fmt.Errorf("package entry: %w", err)
			}
		}
		if seen[name] {
			return inspection, fmt.Errorf("package entry %q appears twice", name)
		}
		seen[name] = true
		if entry.Mode()&fs.ModeSymlink != 0 {
			return inspection, fmt.Errorf("package entry %q is a symlink — packages must not contain links", name)
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		// Cap per entry before summing: one entry may claim up to 2^64-1, and
		// checking each against the total keeps the running sum from wrapping.
		if limits.MaxTotalBytes > 0 && entry.UncompressedSize64 > uint64(limits.MaxTotalBytes) {
			return inspection, fmt.Errorf("package entry %q declares %d bytes (total size limit %d)", name, entry.UncompressedSize64, limits.MaxTotalBytes)
		}
		declared += entry.UncompressedSize64
		if limits.MaxTotalBytes > 0 && declared > uint64(limits.MaxTotalBytes) {
			return inspection, fmt.Errorf("package declares %d bytes (total size limit %d)", declared, limits.MaxTotalBytes)
		}
	}
	if declared <= uint64(math.MaxInt64) {
		inspection.DeclaredBytes = int64(declared)
	} else {
		inspection.DeclaredBytes = math.MaxInt64
	}
	if limits.MaxCompressionRatio > 0 && inspection.DeclaredBytes > ratioCheckFloor &&
		inspection.DeclaredBytes > limits.MaxCompressionRatio*inspection.ArchiveBytes {
		return inspection, errors.New("package declares an implausible compression ratio — refusing as a zip bomb")
	}

	// Pass 2 — what actually comes out. The budget is the real defense; the
	// declared numbers above only make the common case free.
	var measured int64
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		opened, err := entry.Open()
		if err != nil {
			return inspection, err
		}
		copyErr := copyWithBudget(io.Discard, opened, limits, inspection.ArchiveBytes, &measured)
		opened.Close()
		inspection.MeasuredBytes = measured
		if copyErr != nil {
			return inspection, fmt.Errorf("inspect %q: %w", entry.Name, copyErr)
		}
	}
	inspection.MeasuredBytes = measured
	return inspection, nil
}

// ExtractPackage unpacks a .tmod into dest (which must be empty or absent —
// extraction never merges into existing content). Every defense lives here:
// entry-name rules, symlink refusal, entry/size caps, and the compression
// ratio check against the archive's real file size.
func ExtractPackage(packagePath, dest string, limits ArchiveLimits) error {
	archiveInfo, err := os.Stat(packagePath)
	if err != nil {
		return err
	}
	if entries, err := os.ReadDir(dest); err == nil && len(entries) > 0 {
		return fmt.Errorf("extraction target %s is not empty", dest)
	}
	reader, err := zip.OpenReader(packagePath)
	if err != nil {
		return fmt.Errorf("open package: %w", err)
	}
	defer reader.Close()

	if limits.MaxEntries > 0 && len(reader.File) > limits.MaxEntries {
		return fmt.Errorf("package has %d entries (limit %d)", len(reader.File), limits.MaxEntries)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}

	var totalWritten int64
	seen := map[string]bool{}
	for _, entry := range reader.File {
		name := entry.Name
		if name != ManifestFileName {
			if err := validatePackagePath(name); err != nil {
				return fmt.Errorf("package entry: %w", err)
			}
		}
		if seen[name] {
			return fmt.Errorf("package entry %q appears twice", name)
		}
		seen[name] = true
		mode := entry.Mode()
		if mode&fs.ModeSymlink != 0 {
			return fmt.Errorf("package entry %q is a symlink — packages must not contain links", name)
		}
		if entry.FileInfo().IsDir() {
			continue // directories materialize from their files' paths
		}

		target := filepath.Join(dest, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		perm := mode.Perm()
		if perm == 0 {
			perm = 0o644
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		if err != nil {
			return err
		}
		opened, err := entry.Open()
		if err != nil {
			out.Close()
			return err
		}
		copyErr := copyWithBudget(out, opened, limits, archiveInfo.Size(), &totalWritten)
		opened.Close()
		if closeErr := out.Close(); copyErr == nil {
			copyErr = closeErr
		}
		if copyErr != nil {
			return fmt.Errorf("extract %q: %w", name, copyErr)
		}
	}
	return nil
}

// copyWithBudget copies while enforcing the total-size and compression-ratio
// caps against bytes actually written — never against header claims.
func copyWithBudget(dst io.Writer, src io.Reader, limits ArchiveLimits, archiveSize int64, totalWritten *int64) error {
	buffer := make([]byte, 32<<10)
	for {
		n, readErr := src.Read(buffer)
		if n > 0 {
			*totalWritten += int64(n)
			if limits.MaxTotalBytes > 0 && *totalWritten > limits.MaxTotalBytes {
				return fmt.Errorf("package exceeds the total size limit (%d bytes)", limits.MaxTotalBytes)
			}
			if limits.MaxCompressionRatio > 0 && *totalWritten > ratioCheckFloor &&
				*totalWritten > limits.MaxCompressionRatio*archiveSize {
				return errors.New("package compression ratio is implausible — refusing as a zip bomb")
			}
			if _, err := dst.Write(buffer[:n]); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

// collectPackagePaths walks root and returns the sorted relative slash paths
// of every payload file (the manifest excluded — WritePackage archives the
// packer's bytes instead). Symlinks are refused for the same reason
// collectPayloadFiles refuses them.
func collectPackagePaths(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(walkPath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("symlink %s: packages must not contain links", walkPath)
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, walkPath)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == ManifestFileName {
			return nil
		}
		paths = append(paths, relative)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

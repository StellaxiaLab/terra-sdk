package pkgformat

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

var partNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// driveLetterPattern rejects Windows absolute paths ("C:...") that path.Clean
// would not catch — manifest paths are package-relative slash paths, always.
var driveLetterPattern = regexp.MustCompile(`^[a-zA-Z]:`)

// validateIntegrityParts checks the static shape of Integrity.Parts: part
// names, path form, digest form, uniqueness, part-digest self-consistency, and
// agreement with Integrity.Process. It normalizes digests to lowercase in
// place, mirroring what Validate does for Process.
func (m *Manifest) validateIntegrityParts() error {
	if m.Integrity == nil || len(m.Integrity.Parts) == 0 {
		return nil
	}
	partNames := map[string]bool{}
	fileOwners := map[string]string{}
	for index := range m.Integrity.Parts {
		part := &m.Integrity.Parts[index]
		part.Name = strings.TrimSpace(part.Name)
		if !partNamePattern.MatchString(part.Name) {
			return fmt.Errorf("integrity.parts[%d]: invalid part name %q", index, part.Name)
		}
		if partNames[part.Name] {
			return fmt.Errorf("integrity.parts: duplicate part name %q", part.Name)
		}
		partNames[part.Name] = true
		if len(part.Files) == 0 {
			return fmt.Errorf("integrity.parts[%q] has no files", part.Name)
		}
		for filePath, digest := range part.Files {
			if err := validatePackagePath(filePath); err != nil {
				return fmt.Errorf("integrity.parts[%q]: %w", part.Name, err)
			}
			if owner, taken := fileOwners[filePath]; taken {
				return fmt.Errorf("integrity.parts: %q is listed in both %q and %q — a file belongs to exactly one part", filePath, owner, part.Name)
			}
			fileOwners[filePath] = part.Name
			normalized := strings.ToLower(strings.TrimSpace(digest))
			if !sha256HexPattern.MatchString(normalized) {
				return fmt.Errorf("integrity.parts[%q].files[%q] is not a lowercase hex sha256", part.Name, filePath)
			}
			part.Files[filePath] = normalized
		}
		part.SHA256 = strings.ToLower(strings.TrimSpace(part.SHA256))
		if part.SHA256 != "" && part.SHA256 != CanonicalPartDigest(part.Files) {
			return fmt.Errorf("integrity.parts[%q].sha256 does not match its files", part.Name)
		}
	}
	// A file both chains cover must carry one truth. Letting either side win
	// would leave it ambiguous which digest the signature vouched for.
	for target, digest := range m.Integrity.Process {
		entryPath := m.Entrypoints.Process[target]
		if partDigest, listed := lookupPartFile(m.Integrity.Parts, entryPath); listed && partDigest != digest {
			return fmt.Errorf("integrity.process[%q] and integrity.parts disagree about %q", target, entryPath)
		}
	}
	return nil
}

// validatePackagePath enforces the package-relative slash-path form: clean, no
// traversal, no absolute or drive-lettered paths, never the manifest itself
// (it cannot carry its own digest — the signature covers it).
func validatePackagePath(p string) error {
	if p == "" {
		return errors.New("empty file path")
	}
	if p == ManifestFileName {
		return fmt.Errorf("%s must not be listed — the signature covers it", ManifestFileName)
	}
	if strings.Contains(p, "\\") {
		return fmt.Errorf("file path %q must use forward slashes", p)
	}
	if strings.HasPrefix(p, "/") || driveLetterPattern.MatchString(p) {
		return fmt.Errorf("file path %q must be package-relative", p)
	}
	if cleaned := path.Clean(p); cleaned != p || p == ".." || strings.HasPrefix(p, "../") {
		return fmt.Errorf("file path %q must be a clean relative path", p)
	}
	return nil
}

func lookupPartFile(parts []IntegrityPart, filePath string) (string, bool) {
	for _, part := range parts {
		if digest, ok := part.Files[filePath]; ok {
			return digest, true
		}
	}
	return "", false
}

// CanonicalPartDigest is a part's stable content address: the SHA-256 of its
// file list serialized as sorted "digest  path" lines (sha256sum's shape). It
// exists so a part can be referenced for partial transfer without inventing a
// second serialization of the part's bytes.
func CanonicalPartDigest(files map[string]string) string {
	lines := make([]string, 0, len(files))
	for filePath, digest := range files {
		lines = append(lines, digest+"  "+filePath+"\n")
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "")))
	return hex.EncodeToString(sum[:])
}

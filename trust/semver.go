package trust

import (
	"strconv"
	"strings"
)

// semver is a parsed semantic version (major.minor.patch with an optional
// prerelease). Build metadata is parsed but ignored for ordering (semver §10).
// This is a dependency-free subset sufficient for module/capability version
// range matching; it is not a general-purpose semver library.
type semver struct {
	major      int
	minor      int
	patch      int
	prerelease []string
}

// parseSemver accepts "X", "X.Y", or "X.Y.Z" with an optional "-prerelease" and
// "+build", and an optional leading "v". Omitted minor/patch default to 0.
func parseSemver(text string) (semver, bool) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "v")
	if text == "" {
		return semver{}, false
	}
	if index := strings.IndexByte(text, '+'); index >= 0 {
		text = text[:index]
	}
	var prerelease string
	if index := strings.IndexByte(text, '-'); index >= 0 {
		prerelease = text[index+1:]
		text = text[:index]
	}
	parts := strings.Split(text, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return semver{}, false
	}
	numbers := [3]int{}
	for index := 0; index < len(parts); index++ {
		number, err := strconv.Atoi(parts[index])
		if err != nil || number < 0 {
			return semver{}, false
		}
		numbers[index] = number
	}
	version := semver{major: numbers[0], minor: numbers[1], patch: numbers[2]}
	if prerelease != "" {
		version.prerelease = strings.Split(prerelease, ".")
	}
	return version, true
}

// compareSemver returns -1, 0 or +1 as a is less than, equal to, or greater
// than b. A version with a prerelease has lower precedence than the same
// version without one (semver §11).
func compareSemver(a, b semver) int {
	if c := compareInt(a.major, b.major); c != 0 {
		return c
	}
	if c := compareInt(a.minor, b.minor); c != 0 {
		return c
	}
	if c := compareInt(a.patch, b.patch); c != 0 {
		return c
	}
	return comparePrerelease(a.prerelease, b.prerelease)
}

func comparePrerelease(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0: // release outranks prerelease
		return 1
	case len(b) == 0:
		return -1
	}
	for index := 0; index < len(a) && index < len(b); index++ {
		if c := compareIdentifier(a[index], b[index]); c != 0 {
			return c
		}
	}
	return compareInt(len(a), len(b))
}

func compareIdentifier(a, b string) int {
	aNumber, aNumeric := numericIdentifier(a)
	bNumber, bNumeric := numericIdentifier(b)
	switch {
	case aNumeric && bNumeric:
		return compareInt(aNumber, bNumber)
	case aNumeric: // numeric identifiers have lower precedence than alphanumeric
		return -1
	case bNumeric:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

func numericIdentifier(value string) (int, bool) {
	number, err := strconv.Atoi(value)
	if err != nil || number < 0 {
		return 0, false
	}
	return number, true
}

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

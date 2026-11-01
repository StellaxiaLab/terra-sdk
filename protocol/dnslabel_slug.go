package protocol

// The default DNS label a node gets from its display name.
//
// A `.terra` name needs a label that DNS accepts, and a display name is not one:
// "박사님 데스크탑" is a real answer to "what is this machine" and nothing a
// resolver will carry. So the node record has a separate dns_label, and this is
// the value it starts with when nobody chose one.
//
// The slug is a convenience, not an identity. It is derived once, when the label
// is empty, and never again — a display-name edit must not move a name, because
// a name that moves leaves a tombstone behind that nobody can use for a while.

import (
	"strconv"
	"strings"
)

// dnsLabelMaxLength is the longest label DNS allows (RFC 1035 §2.3.4).
const dnsLabelMaxLength = 63

// DNSLabelFromDisplayName derives the default dns_label for a display name.
//
// Lower-cased ASCII letters and digits are kept, every other run of characters
// becomes one hyphen, and hyphens are trimmed from both ends after cutting to 63
// characters — so the result is always a valid label or empty. Empty is the
// honest answer for a name with no ASCII in it: inventing a label from a hash
// would publish a name nobody recognises, and an unnamed node is simply not
// published until a person picks one.
func DNSLabelFromDisplayName(displayName string) string {
	var builder strings.Builder
	pendingHyphen := false
	for _, r := range strings.ToLower(displayName) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if pendingHyphen && builder.Len() > 0 {
				builder.WriteByte('-')
			}
			pendingHyphen = false
			builder.WriteRune(r)
			continue
		}
		pendingHyphen = true
	}
	label := builder.String()
	if len(label) > dnsLabelMaxLength {
		label = label[:dnsLabelMaxLength]
	}
	return strings.Trim(label, "-")
}

// DNSLabelWithSuffix answers the n-th alternative for a label already taken in
// the cluster: "desk" → "desk-2", "desk-3" …. The base is shortened so the
// suffix still fits in 63 characters, and n below 2 answers the base itself.
func DNSLabelWithSuffix(base string, n int) string {
	if n < 2 {
		return base
	}
	suffix := "-" + strconv.Itoa(n)
	if len(base)+len(suffix) > dnsLabelMaxLength {
		base = strings.TrimRight(base[:dnsLabelMaxLength-len(suffix)], "-")
	}
	return base + suffix
}

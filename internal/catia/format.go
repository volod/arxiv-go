package catia

import (
	"bytes"
	"unicode"
)

// Format tokens from the leading bytes, independent of kind.
const (
	FormatV5      = "V5_CFV2"
	FormatZIP     = "zip"
	FormatXML     = "xml"
	FormatUnknown = "unknown"
)

// ReleaseUnknown is the release token when no property or 3dxml header supplied one.
const ReleaseUnknown = "unknown"

const (
	sidecarCap     = 1 << 20
	windowCap      = 4 << 20
	propValueMax   = 4 << 10
	detectPeek     = 4096
	readChunk      = 64 << 10
	zipMaxMembers  = 1024
	zipMaxMember   = 64 << 20
	zipMaxTotal    = 256 << 20
	xmlFileCap     = zipMaxMember
	minStringRun   = 6
	minStringAlpha = 3
)

var (
	magicV5  = []byte("V5_CFV2\x00")
	magicZIP = []byte{'P', 'K', 0x03, 0x04}
	utf8BOM  = []byte{0xEF, 0xBB, 0xBF}
)

// DetectFormat classifies a file from its leading bytes.
func DetectFormat(head []byte) string {
	switch {
	case bytes.HasPrefix(head, magicV5):
		return FormatV5
	case bytes.HasPrefix(head, magicZIP):
		return FormatZIP
	case xmlStart(head):
		return FormatXML
	default:
		return FormatUnknown
	}
}

func xmlStart(head []byte) bool {
	head = bytes.TrimPrefix(head, utf8BOM)
	i := 0
	for i < len(head) {
		r, n := rune(head[i]), 1
		if head[i] >= 0x80 {
			return false
		}
		if !unicode.IsSpace(r) {
			return head[i] == '<'
		}
		i += n
	}
	return false
}

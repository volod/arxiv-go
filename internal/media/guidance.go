package media

import (
	"fmt"
	"strings"
)

// DownloadLinks returns the ffmpeg download pages recommended for a GOOS/GOARCH pair.
func DownloadLinks(goos, goarch string) []string {
	switch goos + "/" + goarch {
	case "linux/amd64":
		return []string{"https://johnvansickle.com/ffmpeg/", "https://ffmpeg.org/download.html#build-linux"}
	case "windows/amd64":
		return []string{"https://www.gyan.dev/ffmpeg/builds/", "https://github.com/BtbN/FFmpeg-Builds/releases"}
	default:
		return []string{"https://ffmpeg.org/download.html"}
	}
}

// Guidance is the operator message for missing tools on a platform: what to download, where from
// and where to put it. It ends with a newline.
func Guidance(missing []Requirement, goos, goarch string) string {
	var names []string
	for _, r := range missing {
		names = append(names, ExecutableName(r.Tool, goos))
	}
	exe := ExecutableName("arxgo", goos)
	var b strings.Builder
	fmt.Fprintf(&b, "Download ffmpeg for %s/%s (it includes %s):\n", goos, goarch, strings.Join(names, " and "))
	for _, link := range DownloadLinks(goos, goarch) {
		fmt.Fprintf(&b, "  %s\n", link)
	}
	fmt.Fprintf(&b, "Then place %s next to %s or add it to PATH.\n", strings.Join(names, " and "), exe)
	return b.String()
}

package catia

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"path"
	"strings"
)

func extractZIP(ctx context.Context, info Info, ra io.ReaderAt, size int64) Info {
	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return fail(info, ErrorKindZip, err)
	}
	var manifest *zip.File
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return fail(info, ErrorKindCancel, err)
		}
		if strings.EqualFold(path.Base(slashName(f.Name)), "Manifest.xml") && !zipNameUnsafe(f.Name) {
			manifest = f
			break
		}
	}
	budget := zipBudget{bytesLeft: zipMaxTotal}
	var rootName string
	if manifest != nil {
		data, ok, err := readZipMember(manifest, &budget)
		if err != nil {
			return fail(info, ErrorKindZip, err)
		}
		if !ok {
			info.Truncated = true
		} else {
			rootName = slashName(parseManifestRoot(data))
		}
	}

	seen := 0
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return fail(info, ErrorKindCancel, err)
		}
		seen++
		if seen > zipMaxMembers {
			info.Truncated = true
			break
		}
		name := slashName(f.Name)
		if zipNameUnsafe(f.Name) {
			continue
		}
		if strings.EqualFold(path.Base(name), "Manifest.xml") {
			continue
		}
		if !xmlMember(name) && name != rootName {
			continue
		}
		isRoot := rootName != "" && (name == rootName || path.Base(name) == path.Base(rootName))
		data, ok, err := readZipMember(f, &budget)
		if err != nil {
			if isRoot {
				return fail(info, ErrorKindZip, err)
			}
			logMemberSkip(err)
			continue
		}
		if !ok {
			info.Truncated = true
			break
		}
		part, err := parse3DXML(ctx, Info{}, bytes.NewReader(data))
		if err != nil {
			if isRoot {
				return textFailed(info, err)
			}
			logMemberSkip(err)
			continue
		}
		info = mergeXML(info, part, isRoot)
	}
	return finishXML(info)
}

type zipBudget struct {
	bytesLeft int64
}

func readZipMember(f *zip.File, b *zipBudget) ([]byte, bool, error) {
	n := int64(f.UncompressedSize64)
	if n > zipMaxMember || n > b.bytesLeft {
		return nil, false, nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil, true, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, n+1))
	if err != nil {
		return nil, true, err
	}
	if int64(len(data)) > n {
		return nil, false, nil
	}
	b.bytesLeft -= int64(len(data))
	return data, true, nil
}

func mergeXML(dst, src Info, prefer bool) Info {
	dst.Components = append(dst.Components, src.Components...)
	dst.Strings = append(dst.Strings, src.Strings...)
	dst.Truncated = dst.Truncated || src.Truncated
	if prefer || dst.SchemaVersion == "" {
		if src.SchemaVersion != "" {
			dst.SchemaVersion = src.SchemaVersion
		}
		if src.Title != "" {
			dst.Title = src.Title
		}
		if src.Author != "" {
			dst.Author = src.Author
		}
		if src.Generator != "" {
			dst.Generator = src.Generator
		}
		if src.Created != "" {
			dst.Created = src.Created
		}
	}
	return dst
}

func xmlMember(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	return ext == ".xml" || ext == ".3dxml"
}

func zipNameUnsafe(name string) bool {
	n := slashName(name)
	if strings.Contains(n, "..") {
		return true
	}
	if strings.HasPrefix(n, "/") || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") {
		return true
	}
	if len(n) >= 2 && n[1] == ':' {
		return true
	}
	return false
}

func slashName(name string) string {
	return strings.ReplaceAll(name, "\\", "/")
}

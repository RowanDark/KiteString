package ksfile

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"

	"github.com/RowanDark/kitestring/pkg/proute"
	"google.golang.org/protobuf/proto"
)

const CurrentVersion uint32 = 1

// KSFileMeta holds metadata fields written into a .ks file header.
type KSFileMeta struct {
	Name        string
	Description string
	Source      string
	CreatedAt   string
}

// Write serializes kf to a gzip-compressed protobuf file at path.
func Write(path string, kf *KSFile) error {
	data, err := proto.Marshal(kf)
	if err != nil {
		return fmt.Errorf("ksfile: marshal: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("ksfile: create %s: %w", path, err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	if _, err := gz.Write(data); err != nil {
		return fmt.Errorf("ksfile: gzip write: %w", err)
	}
	return gz.Close()
}

// Read decompresses and deserializes a .ks file, returning an error if the
// file's version exceeds CurrentVersion.
func Read(path string) (*KSFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("ksfile: open %s: %w", path, err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("ksfile: gzip reader: %w", err)
	}
	defer gz.Close()

	data, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("ksfile: gzip read: %w", err)
	}

	var kf KSFile
	if err := proto.Unmarshal(data, &kf); err != nil {
		return nil, fmt.Errorf("ksfile: unmarshal: %w", err)
	}

	if kf.Version > CurrentVersion {
		return nil, fmt.Errorf("ksfile: unsupported version %d (max supported: %d)", kf.Version, CurrentVersion)
	}

	return &kf, nil
}

// ToRoutes converts a KSFile to the internal Route slice.
func ToRoutes(kf *KSFile) ([]proute.Route, error) {
	routes := make([]proute.Route, 0, len(kf.Routes))
	for _, r := range kf.Routes {
		routes = append(routes, proute.Route{
			Method:      r.Method,
			Path:        r.Path,
			ContentType: r.ContentType,
			KSUID:       r.Ksuid,
			Headers:     toProuteCrumbs(r.Headers),
			QueryParams: toProuteCrumbs(r.QueryParams),
			BodyParams:  toProuteCrumbs(r.BodyParams),
		})
	}
	return routes, nil
}

// FromRoutes converts internal Routes to a KSFile with the provided metadata.
func FromRoutes(routes []proute.Route, meta KSFileMeta) *KSFile {
	kf := &KSFile{
		Version:     CurrentVersion,
		Name:        meta.Name,
		Description: meta.Description,
		Source:      meta.Source,
		CreatedAt:   meta.CreatedAt,
		Routes:      make([]*KSRoute, 0, len(routes)),
	}
	for _, r := range routes {
		kf.Routes = append(kf.Routes, &KSRoute{
			Method:      r.Method,
			Path:        r.Path,
			ContentType: r.ContentType,
			Ksuid:       r.KSUID,
			Headers:     toKSCrumbs(r.Headers),
			QueryParams: toKSCrumbs(r.QueryParams),
			BodyParams:  toKSCrumbs(r.BodyParams),
		})
	}
	return kf
}

func toProuteCrumbs(cs []*KSCrumb) []proute.Crumb {
	out := make([]proute.Crumb, 0, len(cs))
	for _, c := range cs {
		out = append(out, proute.Crumb{
			Key:      c.Key,
			Type:     proute.CrumbType(c.Type),
			Required: c.Required,
			Example:  c.Example,
		})
	}
	return out
}

func toKSCrumbs(cs []proute.Crumb) []*KSCrumb {
	out := make([]*KSCrumb, 0, len(cs))
	for _, c := range cs {
		out = append(out, &KSCrumb{
			Key:      c.Key,
			Type:     int32(c.Type),
			Required: c.Required,
			Example:  c.Example,
		})
	}
	return out
}

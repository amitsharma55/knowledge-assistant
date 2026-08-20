package storage

import (
	"context"
	"os"
	"path/filepath"
)

// Store snapshots raw + normalized content. In prod this is S3; the local
// implementation writes to disk so devs can inspect ingestion output.
type Store interface {
	PutRaw(ctx context.Context, spaceKey, pageID, version, body string) error
	PutNormalized(ctx context.Context, spaceKey, pageID, markdown string) error
}

type LocalDisk struct{ Root string }

func (l LocalDisk) PutRaw(_ context.Context, spaceKey, pageID, version, body string) error {
	p := filepath.Join(l.Root, "raw", spaceKey, pageID, version+".html")
	return write(p, body)
}

func (l LocalDisk) PutNormalized(_ context.Context, spaceKey, pageID, markdown string) error {
	p := filepath.Join(l.Root, "norm", spaceKey, pageID+".md")
	return write(p, markdown)
}

func write(p, body string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(body), 0o644)
}

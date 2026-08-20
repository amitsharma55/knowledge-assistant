package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/example/knowledge-assistant/internal/chunker"
	"github.com/example/knowledge-assistant/internal/embed"
	"github.com/example/knowledge-assistant/services/ingestion/internal/confluence"
	"github.com/example/knowledge-assistant/internal/index"
	"github.com/example/knowledge-assistant/services/ingestion/internal/storage"
)

func main() {
	source := flag.String("source", "confluence", "confluence | fixtures")
	space := flag.String("space", "", "Confluence space key (or fixture space label)")
	fixturesDir := flag.String("fixtures", "fixtures", "directory of *.md files when source=fixtures")
	osURL := flag.String("opensearch", envOr("KA_OPENSEARCH_URL", "http://localhost:9200"), "OpenSearch URL")
	osIdx := flag.String("index", envOr("KA_OPENSEARCH_INDEX", "kb-chunks"), "OpenSearch index")
	dataDir := flag.String("data", envOr("KA_DATA_DIR", ".data"), "local snapshot dir (stand-in for S3)")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx := context.Background()

	store := storage.LocalDisk{Root: *dataDir}
	embedder := embed.Mock{Dim: 1024}
	idxr := &index.Indexer{BaseURL: *osURL, Index: *osIdx, HTTP: &http.Client{Timeout: 30 * time.Second}}
	if err := idxr.EnsureIndex(ctx, 1024); err != nil {
		log.Warn("ensure index failed (OpenSearch running?)", "err", err)
	}

	var pages []confluence.Page
	var err error

	switch *source {
	case "fixtures":
		pages, err = loadFixtures(*fixturesDir, *space)
	case "confluence":
		cli := &confluence.Client{
			BaseURL: os.Getenv("KA_CONFLUENCE_URL"),
			Token:   os.Getenv("KA_CONFLUENCE_TOKEN"),
			HTTP:    &http.Client{Timeout: 30 * time.Second},
		}
		pages, err = cli.ListChangedPages(ctx, *space, time.Time{})
	default:
		log.Error("unknown source", "source", *source)
		os.Exit(2)
	}
	if err != nil {
		log.Error("load pages failed", "err", err)
		os.Exit(1)
	}

	var docs []index.Doc
	for _, p := range pages {
		md := p.BodyHTML
		if *source == "confluence" {
			md = chunker.HTMLToMarkdown(p.BodyHTML)
		}
		_ = store.PutRaw(ctx, p.SpaceKey, p.ID, itoa(p.Version), p.BodyHTML)
		_ = store.PutNormalized(ctx, p.SpaceKey, p.ID, md)

		for _, c := range chunker.Split(md, 800, 100) {
			vec, err := embedder.Embed(ctx, c.Text)
			if err != nil {
				log.Error("embed failed", "err", err, "page", p.ID)
				continue
			}
			docs = append(docs, index.Doc{
				ID:          docID(p.ID, c.SectionPath, c.Text),
				SpaceKey:    p.SpaceKey,
				PageID:      p.ID,
				PageTitle:   p.Title,
				SectionPath: c.SectionPath,
				URL:         p.WebURL,
				Text:        c.Text,
				Embedding:   vec,
				UpdatedAt:   p.UpdatedAt.Format(time.RFC3339),
				ACLGroups:   p.ACLGroups,
			})
		}
	}

	if err := idxr.Bulk(ctx, docs); err != nil {
		log.Warn("bulk index failed (OpenSearch running?)", "err", err)
	}
	log.Info("ingest complete", "pages", len(pages), "chunks", len(docs))
}

func loadFixtures(dir, space string) ([]confluence.Page, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var pages []confluence.Page
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		id := strings.TrimSuffix(e.Name(), ".md")
		pages = append(pages, confluence.Page{
			ID:        id,
			Title:     strings.ReplaceAll(id, "-", " "),
			SpaceKey:  space,
			BodyHTML:  string(body),
			WebURL:    "file://" + filepath.Join(dir, e.Name()),
			UpdatedAt: time.Now(),
			Version:   1,
		})
	}
	return pages, nil
}

func docID(pageID, section, text string) string {
	h := sha1.New()
	h.Write([]byte(pageID))
	h.Write([]byte{0})
	h.Write([]byte(section))
	h.Write([]byte{0})
	h.Write([]byte(text))
	return pageID + ":" + hex.EncodeToString(h.Sum(nil))[:12]
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [16]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}

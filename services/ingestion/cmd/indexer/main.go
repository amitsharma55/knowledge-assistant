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
	"github.com/example/knowledge-assistant/internal/index"
	"github.com/example/knowledge-assistant/internal/team"
	"github.com/example/knowledge-assistant/services/ingestion/internal/gitlab"
	"github.com/example/knowledge-assistant/services/ingestion/internal/storage"
)

func main() {
	source := flag.String("source", "gitlab", "gitlab | fixtures")
	space := flag.String("space", "", "fixture space label (fixtures mode only)")
	fixturesDir := flag.String("fixtures", "fixtures", "directory of *.md files when source=fixtures")
	teamSlug := flag.String("team", "", "team to stamp on every indexed doc (required; one of coupa, star, hr)")
	osURL := flag.String("opensearch", envOr("KA_OPENSEARCH_URL", "http://localhost:9200"), "OpenSearch URL")
	osIdx := flag.String("index", envOr("KA_OPENSEARCH_INDEX", "kb-chunks"), "OpenSearch index")
	dataDir := flag.String("data", envOr("KA_DATA_DIR", ".data"), "local snapshot dir (stand-in for S3)")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx := context.Background()

	registry := team.NewRegistry(team.DefaultInfos())
	tm, err := registry.Parse(*teamSlug)
	if err != nil {
		log.Error("missing or unknown -team flag; team is required and is never inferred from file content or path", "team", *teamSlug, "err", err)
		os.Exit(2)
	}

	store := storage.LocalDisk{Root: *dataDir}
	// Same constructor as chat-api: the vectors written here and the query
	// vectors there have to come from one model.
	embedOpts := embed.OptionsFromEnv()
	embedder, embedDim, err := embed.New(embedOpts)
	if err != nil {
		log.Error("embedder config invalid", "err", err)
		os.Exit(1)
	}
	log.Info("using embedder", "mode", embedOpts.Mode, "model", embedOpts.Model, "dim", embedDim)

	idxr := &index.Indexer{BaseURL: *osURL, Index: *osIdx, HTTP: &http.Client{Timeout: 30 * time.Second}}
	if err := idxr.EnsureIndex(ctx, embedDim); err != nil {
		log.Error("ensure index failed; refusing to ingest into a stale/invalid index", "err", err)
		os.Exit(1)
	}

	var pages []page

	switch *source {
	case "fixtures":
		pages, err = loadFixtures(*fixturesDir, *space)
	case "gitlab":
		cli := gitlab.New(gitlab.Config{
			BaseURL:     os.Getenv("KA_GITLAB_URL"),
			Token:       os.Getenv("KA_GITLAB_TOKEN"),
			ProjectID:   os.Getenv("KA_GITLAB_PROJECT"),
			RepoRef:     envOr("KA_GITLAB_REF", ""),
			RepoPath:    envOr("KA_GITLAB_PATH", "docs"),
			IncludeRepo: envOr("KA_GITLAB_INCLUDE_REPO", "true") == "true",
			IncludeWiki: envOr("KA_GITLAB_INCLUDE_WIKI", "true") == "true",
		})
		var gps []gitlab.Page
		gps, err = cli.FetchAll(ctx)
		for _, p := range gps {
			pages = append(pages, page{
				SpaceKey: p.ProjectID + ":" + p.Source, ID: p.ID, Title: p.Title,
				Markdown: p.Markdown, WebURL: p.WebURL, UpdatedAt: p.UpdatedAt,
			})
		}
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
		_ = store.PutNormalized(ctx, p.SpaceKey, p.ID, p.Markdown)

		for _, c := range chunker.Split(p.Markdown, 800, 100) {
			vec, err := embedder.Embed(ctx, chunker.EmbedText(p.Title, c.SectionPath, c.Text))
			if err != nil {
				log.Error("embed failed", "err", err, "page", p.ID)
				continue
			}
			docs = append(docs, index.Doc{
				ID:          docID(tm.Slug(), p.ID, c.SectionPath, c.Text),
				Team:        tm.Slug(),
				SpaceKey:    p.SpaceKey,
				PageID:      p.ID,
				PageTitle:   p.Title,
				SectionPath: c.SectionPath,
				URL:         p.WebURL,
				Text:        c.Text,
				Embedding:   vec,
				UpdatedAt:   p.UpdatedAt.Format(time.RFC3339),
			})
		}
	}

	if err := idxr.Bulk(ctx, docs); err != nil {
		log.Error("bulk index failed; job must not report success after indexing nothing", "err", err)
		os.Exit(1)
	}
	log.Info("ingest complete", "source", *source, "pages", len(pages), "chunks", len(docs))
}

// page is the ingester's internal normalized shape; both GitLab and fixture
// sources fan in to it before chunking.
type page struct {
	SpaceKey  string
	ID        string
	Title     string
	Markdown  string
	WebURL    string
	UpdatedAt time.Time
}

func loadFixtures(dir, space string) ([]page, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	if space == "" {
		space = "FIXTURES"
	}
	var pages []page
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		id := strings.TrimSuffix(e.Name(), ".md")
		pages = append(pages, page{
			SpaceKey:  space,
			ID:        id,
			Title:     strings.ReplaceAll(id, "-", " "),
			Markdown:  string(body),
			WebURL:    "file://" + filepath.Join(dir, e.Name()),
			UpdatedAt: time.Now(),
		})
	}
	return pages, nil
}

// docID scopes a document's identity to its team, so byte-identical content
// (shared boilerplate, a copied policy section) in two different teams
// produces two different ids rather than colliding and overwriting each
// other in the shared OpenSearch index.
func docID(team, pageID, section, text string) string {
	h := sha1.New()
	h.Write([]byte(team))
	h.Write([]byte{0})
	h.Write([]byte(pageID))
	h.Write([]byte{0})
	h.Write([]byte(section))
	h.Write([]byte{0})
	h.Write([]byte(text))
	return team + ":" + pageID + ":" + hex.EncodeToString(h.Sum(nil))[:12]
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

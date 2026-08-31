package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/example/knowledge-assistant/internal/chunker"
	"github.com/example/knowledge-assistant/internal/embed"
	"github.com/example/knowledge-assistant/internal/index"
	"github.com/example/knowledge-assistant/internal/rag"
	"github.com/example/knowledge-assistant/internal/team"
	"github.com/example/knowledge-assistant/services/chat-api/internal/anthropic"
	"github.com/example/knowledge-assistant/services/chat-api/internal/bedrock"
	"github.com/example/knowledge-assistant/services/chat-api/internal/config"
	"github.com/example/knowledge-assistant/services/chat-api/internal/handler"
	"github.com/example/knowledge-assistant/services/chat-api/internal/middleware"
	"github.com/example/knowledge-assistant/services/chat-api/internal/opensearch"
	"github.com/example/knowledge-assistant/services/chat-api/internal/repo"
	"github.com/example/knowledge-assistant/services/chat-api/internal/session"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

func main() {
	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	embedder := rag.Embedder(embed.Mock{Dim: 1024})

	var retriever rag.Retriever
	switch {
	case cfg.FixturesDir != "":
		memStore := opensearch.NewMemoryStore(embedder)
		n, err := preloadFixtures(context.Background(), memStore, cfg.FixturesDir)
		if err != nil {
			log.Error("preload fixtures failed", "err", err)
			os.Exit(1)
		}
		log.Info("preloaded fixtures", "chunks", n, "dir", cfg.FixturesDir)
		retriever = memStore
	default:
		retriever = &opensearch.Client{
			BaseURL:  cfg.OpenSearchURL,
			Index:    cfg.OpenSearchIdx,
			HTTP:     &http.Client{Timeout: 10 * time.Second},
			Embedder: embedder,
		}
		log.Info("using opensearch", "url", cfg.OpenSearchURL, "index", cfg.OpenSearchIdx)
	}

	var llm rag.LLM
	switch cfg.LLMMode {
	case "anthropic":
		if cfg.AnthropicAPIKey == "" {
			log.Error("KA_LLM_MODE=anthropic but ANTHROPIC_API_KEY is empty")
			os.Exit(1)
		}
		llm = &anthropic.Client{
			APIKey:    cfg.AnthropicAPIKey,
			Model:     cfg.AnthropicModel,
			MaxTokens: 1024,
			HTTP:      &http.Client{Timeout: 120 * time.Second},
		}
		log.Info("using anthropic", "model", cfg.AnthropicModel)
	case "bedrock":
		log.Warn("bedrock mode not wired yet; falling back to mock")
		llm = bedrock.MockLLM{}
	default:
		llm = bedrock.MockLLM{}
		log.Info("using mock LLM")
	}

	orch := &rag.Orchestrator{
		Retriever:      retriever,
		LLM:            llm,
		TopK:           cfg.TopK,
		RerankN:        cfg.RerankTopN,
		RelevanceFloor: cfg.RelevanceFloor,
	}
	if c, ok := retriever.(rag.Counter); ok {
		orch.Counter = c
	}

	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Recoverer, chimw.Timeout(180*time.Second))
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })

	sessions := session.NewStore(embedder, 30*time.Minute)

	var uploadIndexer *index.Indexer
	if cfg.FixturesDir == "" { // real OpenSearch mode
		uploadIndexer = &index.Indexer{
			BaseURL: cfg.OpenSearchURL, Index: cfg.OpenSearchIdx,
			HTTP: &http.Client{Timeout: 30 * time.Second},
		}
	}

	var repository *repo.Repo
	if cfg.PostgresDSN != "" {
		var err error
		repository, err = repo.New(context.Background(), cfg.PostgresDSN)
		if err != nil {
			log.Warn("postgres unavailable; chats will not persist", "err", err)
		} else {
			log.Info("postgres connected")
			defer repository.Close()
		}
	}

	registry := team.NewRegistry(team.DefaultInfos())
	resolver := middleware.StaticResolver{Registry: registry, Members: middleware.DemoMembers()}

	authed := r.With(middleware.Auth(true /* dev */), middleware.WithScope(registry, resolver))
	authed.Method(http.MethodGet, "/v1/teams", &handler.TeamsHandler{Registry: registry})
	authed.Method(http.MethodPost, "/v1/chat/messages", &handler.ChatHandler{
		Orchestrator: orch, Sessions: sessions, Repo: repository, Log: log,
	})
	authed.Method(http.MethodPost, "/v1/uploads", &handler.UploadHandler{
		Sessions: sessions, Indexer: uploadIndexer, Embedder: embedder, Log: log,
	})
	if repository != nil {
		ch := &handler.ChatsHandler{Repo: repository, Log: log}
		authed.Get("/v1/chats", ch.List)
		authed.Post("/v1/chats", ch.Create)
		authed.Get("/v1/chats/{id}/messages", ch.Messages)
		authed.Patch("/v1/chats/{id}", ch.Rename)
		authed.Delete("/v1/chats/{id}", ch.Delete)
	}

	srv := &http.Server{Addr: cfg.Addr, Handler: r, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Info("chat-api listening", "addr", cfg.Addr, "llm", cfg.LLMMode)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// preloadFixtures walks one subdirectory per team under dir (e.g.
// fixtures/coupa, fixtures/star, fixtures/hr), deriving the team from the
// directory name and stamping it on every chunk. Team is never inferred
// from file content, only from the directory it lives in.
func preloadFixtures(ctx context.Context, store *opensearch.MemoryStore, dir string) (int, error) {
	teamDirs, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, td := range teamDirs {
		if !td.IsDir() {
			continue
		}
		teamSlug := td.Name()
		teamPath := filepath.Join(dir, teamSlug)
		entries, err := os.ReadDir(teamPath)
		if err != nil {
			return total, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			body, err := os.ReadFile(filepath.Join(teamPath, e.Name()))
			if err != nil {
				return total, err
			}
			pageID := strings.TrimSuffix(e.Name(), ".md")
			title := strings.ReplaceAll(pageID, "-", " ")
			for i, c := range chunker.Split(string(body), 800, 100) {
				err := store.Upsert(ctx, rag.Chunk{
					ID:          teamSlug + ":" + pageID + ":" + itoa(i),
					Team:        teamSlug,
					SpaceKey:    "DEMO",
					PageID:      pageID,
					PageTitle:   title,
					SectionPath: c.SectionPath,
					URL:         "file://" + filepath.Join(teamPath, e.Name()),
					Text:        c.Text,
				})
				if err != nil {
					return total, err
				}
				total++
			}
		}
	}
	return total, nil
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [8]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}

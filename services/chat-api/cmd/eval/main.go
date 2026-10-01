// Command eval scores retrieval over a golden set and prints recall@K /
// precision@K / MRR / NDCG@K, broken out per category, with the reranker off
// and on. It runs the pipeline in-process against the fixture corpus (the
// MemoryStore retriever) with the real embedder -- no chat-api, no SSE, no LLM.
//
// It answers two questions the 36-question fact-coverage check could not:
// how high is the dense-retrieval recall ceiling per category, and does simply
// enabling the reranker fix the near-twin case before we reach for hybrid.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/example/knowledge-assistant/internal/awsx"
	"github.com/example/knowledge-assistant/internal/chunker"
	"github.com/example/knowledge-assistant/internal/embed"
	"github.com/example/knowledge-assistant/internal/eval"
	"github.com/example/knowledge-assistant/internal/rag"
	"github.com/example/knowledge-assistant/internal/rerank"
	"github.com/example/knowledge-assistant/internal/team"
	"github.com/example/knowledge-assistant/services/chat-api/internal/opensearch"
)

func main() {
	golden := flag.String("golden", "fixtures/eval/golden.json", "golden set JSON")
	fixtures := flag.String("fixtures", "fixtures", "fixture corpus dir (team subdirs of *.md)")
	topK := flag.Int("topk", 8, "retrieval pool size (matches KA_TOP_K)")
	k := flag.Int("k", 10, "K for recall@K / precision@K / NDCG@K")
	mode := flag.String("rerank", "both", "reranker: off | on | both")
	backend := flag.String("rerank-backend", "ollama", "reranker backend when on: ollama | bedrock (bedrock = the real serving path)")
	flag.Parse()

	if err := run(*golden, *fixtures, *topK, *k, *mode, *backend); err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(1)
	}
}

func run(goldenPath, fixturesDir string, topK, k int, mode, backend string) error {
	entries, err := eval.Load(goldenPath)
	if err != nil {
		return err
	}
	// embed.New(OptionsFromEnv()) -- the one constructor both binaries use, so
	// the harness embeds queries with the same model the index was built with.
	// The mock embedder is never a fallback; a wrong model makes every number
	// meaningless, so a bad config must fail loudly here.
	embedder, _, err := embed.New(embed.OptionsFromEnv())
	if err != nil {
		return fmt.Errorf("embedder: %w", err)
	}
	ctx := context.Background()
	store := opensearch.NewMemoryStore(embedder)
	n, err := loadFixtures(ctx, store, fixturesDir)
	if err != nil {
		return err
	}

	reg := team.NewRegistry(team.DefaultInfos())
	raw, rerankedOff, err := eval.Run(ctx, entries, reg, store, nil, topK, k)
	if err != nil {
		return fmt.Errorf("baseline run: %w", err)
	}
	var rerankedOn *eval.Result
	if mode == "on" || mode == "both" {
		rr, err := newReranker(ctx, backend)
		if err != nil {
			return err
		}
		_, on, err := eval.Run(ctx, entries, reg, store, rr, topK, k)
		if err != nil {
			return fmt.Errorf("rerank-on run: %w", err)
		}
		rerankedOn = &on
	}

	printHeader(goldenPath, fixturesDir, entries, n, topK, k, mode, backend)
	printStage1(raw, k)
	printStage2(rerankedOff, rerankedOn, k, mode)
	return nil
}

// newReranker builds the reranker for the "on" run. bedrock is the path we would
// actually serve from (KA_RERANK_MODE=bedrock in deploy), so its latency is the
// decision-grade number; ollama is the local convenience backend.
func newReranker(ctx context.Context, backend string) (rag.Reranker, error) {
	switch backend {
	case "ollama":
		url := envOr("KA_RERANK_URL", envOr("KA_OLLAMA_URL", "http://localhost:11434"))
		model := envOr("KA_RERANK_MODEL", "gpt-oss:20b")
		return rerank.Ollama{BaseURL: url, Model: model, HTTP: &http.Client{Timeout: 45 * time.Second}}, nil
	case "bedrock":
		client, err := awsx.BedrockRuntime(ctx)
		if err != nil {
			return nil, fmt.Errorf("bedrock reranker: %w", err)
		}
		model := envOr("KA_RERANK_MODEL", "openai.gpt-oss-20b-1:0")
		return rerank.NewBedrock(model, client), nil
	default:
		return nil, fmt.Errorf("unknown -rerank-backend %q; want ollama or bedrock", backend)
	}
}

// loadFixtures mirrors services/chat-api preloadFixtures: team subdirs of *.md,
// pageId = filename, title = pageId with dashes spaced, embedded via the same
// EmbedText path as production. Kept in step with that loader by hand; the ids
// differ harmlessly (the eval grades on pageId, not id).
func loadFixtures(ctx context.Context, store *opensearch.MemoryStore, dir string) (int, error) {
	teamDirs, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("read fixtures dir: %w", err)
	}
	total := 0
	for _, td := range teamDirs {
		if !td.IsDir() {
			continue
		}
		teamSlug := td.Name()
		teamPath := filepath.Join(dir, teamSlug)
		files, err := os.ReadDir(teamPath)
		if err != nil {
			return total, err
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			body, err := os.ReadFile(filepath.Join(teamPath, f.Name()))
			if err != nil {
				return total, err
			}
			pageID := strings.TrimSuffix(f.Name(), ".md")
			title := strings.ReplaceAll(pageID, "-", " ")
			for i, c := range chunker.Split(string(body), 800, 100) {
				err := store.Upsert(ctx, rag.Chunk{
					ID:          fmt.Sprintf("%s:%s:%d", teamSlug, pageID, i),
					Team:        teamSlug,
					SpaceKey:    "DEMO",
					PageID:      pageID,
					PageTitle:   title,
					SectionPath: c.SectionPath,
					Text:        c.Text,
				})
				if err != nil {
					return total, fmt.Errorf("index %s/%s: %w", teamSlug, f.Name(), err)
				}
				total++
			}
		}
	}
	if total == 0 {
		return 0, fmt.Errorf("no fixtures found under %s", dir)
	}
	return total, nil
}

// catsInOrder is the fixed print order; overall is appended by the callers.
var catsInOrder = []string{eval.CategoryLiteralToken, eval.CategoryNearTwin, eval.CategoryNormal}

func printHeader(goldenPath, fixturesDir string, entries []eval.Entry, chunks, topK, k int, mode, backend string) {
	counts := eval.CountByCategory(entries)
	fmt.Printf("Golden set: %s — %d entries (literal_token %d, near_twin %d, normal %d)\n",
		goldenPath, len(entries), counts[eval.CategoryLiteralToken], counts[eval.CategoryNearTwin], counts[eval.CategoryNormal])
	fmt.Printf("Fixtures:   %s — %d chunks   TopK=%d   metrics@%d", fixturesDir, chunks, topK, k)
	if mode == "on" || mode == "both" {
		fmt.Printf("   rerank=%s", backend)
	}
	fmt.Print("\n\n")
}

func printStage1(raw eval.Result, k int) {
	fmt.Println("STAGE 1 — raw retrieval (the recall ceiling; the reranker cannot exceed it)")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintf(w, "category\tn\trecall@%d\tP@1\tP@3\tmrr\n", k)
	for _, cat := range catsInOrder {
		writeRow(w, cat, raw.ByCat[cat])
	}
	writeRow(w, "overall", raw.Overall)
	w.Flush()
	fmt.Println()
}

func writeRow(w *tabwriter.Writer, label string, m eval.Metrics) {
	fmt.Fprintf(w, "%s\t%d\t%.3f\t%.3f\t%.3f\t%.3f\n", label, m.Queries, m.Recall, m.P1, m.P3, m.MRR)
}

func printStage2(off eval.Result, on *eval.Result, k int, mode string) {
	if on == nil {
		fmt.Printf("STAGE 2 — reranker %s\n", mode)
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintf(w, "category\tn\tNDCG@%d\tmrr\n", k)
		for _, cat := range catsInOrder {
			m := off.ByCat[cat]
			fmt.Fprintf(w, "%s\t%d\t%.3f\t%.3f\n", cat, m.Queries, m.NDCG, m.MRR)
		}
		m := off.Overall
		fmt.Fprintf(w, "%s\t%d\t%.3f\t%.3f\n", "overall", m.Queries, m.NDCG, m.MRR)
		w.Flush()
		return
	}
	fmt.Printf("STAGE 2 — NDCG@%d and MRR, reranker OFF vs ON (near_twin is the number to watch)\n", k)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintf(w, "category\tn\tNDCG off\tNDCG on\tΔ\tMRR off\tMRR on\n")
	for _, cat := range catsInOrder {
		writeCompareRow(w, cat, off.ByCat[cat], on.ByCat[cat])
	}
	writeCompareRow(w, "overall", off.Overall, on.Overall)
	w.Flush()

	if on.Fallbacks > 0 {
		fmt.Printf("\n⚠  reranker fell back to dense order on %d/%d queries — it returned an error or the\n"+
			"   wrong chunk count, so its ranking was discarded. A near-zero NDCG delta above is\n"+
			"   this, not a weak reranker. Check the model id, the backend, and parseOrder.\n",
			on.Fallbacks, on.Overall.Queries)
	}

	l := on.Latency
	fmt.Printf("\nRerank latency (per query, listwise one round-trip): p50 %s  p95 %s  p99 %s  max %s  (n=%d)\n",
		round(l.P50), round(l.P95), round(l.P99), round(l.Max), l.N)
	if l.N < 20 {
		fmt.Printf("  p95/p99 are not meaningful at n=%d — they collapse onto max until the set grows.\n", l.N)
	}
}

func round(d time.Duration) time.Duration { return d.Round(time.Millisecond) }

func writeCompareRow(w *tabwriter.Writer, label string, off, on eval.Metrics) {
	fmt.Fprintf(w, "%s\t%d\t%.3f\t%.3f\t%+.3f\t%.3f\t%.3f\n",
		label, off.Queries, off.NDCG, on.NDCG, on.NDCG-off.NDCG, off.MRR, on.MRR)
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

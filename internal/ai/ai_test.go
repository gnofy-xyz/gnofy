package ai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gnofy-xyz/gnofy/internal/ai"
	"github.com/gnofy-xyz/gnofy/internal/ir"
)

func TestOpenAICompatResolverAndStripFences(t *testing.T) {
	var authHeader string
	var gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		authHeader = r.Header.Get("Authorization")
		var payload struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(payload.Messages) >= 2 {
			gotPrompt = payload.Messages[1].Content
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "qwen2.5-coder:7b",
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": "```gno\nRequire(len(recipients) == len(amounts), \"LengthMismatch\")\n```",
					},
				},
			},
		})
	}))
	defer srv.Close()

	res, err := ai.NewOpenAICompatResolver(ai.OpenAICompatConfig{
		Endpoint: srv.URL + "/v1",
		Model:    "qwen2.5-coder:7b",
		APIKey:   "test-secret",
	}).Resolve(context.Background(), ai.HoleRequest{
		ContractName:      "BatchDisperse",
		FunctionSignature: "func DisperseNative(cur realm, recipients []address, amounts []U256)",
		Hole: &ir.Hole{
			ID:               "abc123",
			Reason:           "UNSUPPORTED_YUL",
			OriginalSolidity: "assembly { if iszero(eq(recipients.length, amounts.length)) { revert(0, 0) } }",
		},
		PreviousError: "undefined: foo",
	})
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if authHeader != "Bearer test-secret" {
		t.Fatalf("expected Bearer auth header, got %q", authHeader)
	}
	if !strings.Contains(gotPrompt, "undefined: foo") {
		t.Fatalf("expected PreviousError in prompt, got:\n%s", gotPrompt)
	}
	if strings.Contains(res.GnoCode, "```") {
		t.Fatalf("expected markdown fences stripped, got: %q", res.GnoCode)
	}
	if res.GnoCode != `Require(len(recipients) == len(amounts), "LengthMismatch")` {
		t.Fatalf("unexpected GnoCode: %q", res.GnoCode)
	}
	if res.Source != ai.SourceAIFresh {
		t.Fatalf("expected SourceAIFresh, got %q", res.Source)
	}
}

func TestLockfileRoundTripAndCacheHit(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "gnofy.lock")

	var calls atomic.Int32
	mockInner := ai.HoleResolverFunc(func(_ context.Context, req ai.HoleRequest) (ai.HoleResolution, error) {
		calls.Add(1)
		return ai.HoleResolution{
			GnoCode: "id = U256FromU64(1)",
			Model:   "mock-local",
			Source:  ai.SourceAIFresh,
		}, nil
	})

	hole := &ir.Hole{ID: "h1", Reason: "UNSUPPORTED_YUL", ScopeSummary: "getChainIdAsm"}

	// 1. Primera ejecución: miss en lockfile -> llama al modelo y confirma tras verificación
	lf, err := ai.LoadLockfile(lockPath)
	if err != nil {
		t.Fatalf("LoadLockfile: %v", err)
	}
	cached := ai.NewCachedResolver(mockInner, lf)
	res1, err := cached.Resolve(context.Background(), ai.HoleRequest{Hole: hole})
	if err != nil {
		t.Fatalf("Resolve #1: %v", err)
	}
	if res1.Source != ai.SourceAIFresh || calls.Load() != 1 {
		t.Fatalf("expected 1 fresh AI call, got source=%s calls=%d", res1.Source, calls.Load())
	}
	cached.CommitVerified()
	if err := ai.SaveLockfile(lockPath, lf); err != nil {
		t.Fatalf("SaveLockfile: %v", err)
	}

	// 2. Segunda ejecución: hit en gnofy.lock -> 0 llamadas al modelo IA
	lf2, err := ai.LoadLockfile(lockPath)
	if err != nil {
		t.Fatalf("LoadLockfile #2: %v", err)
	}
	cached2 := ai.NewCachedResolver(mockInner, lf2)
	res2, err := cached2.Resolve(context.Background(), ai.HoleRequest{Hole: hole})
	if err != nil {
		t.Fatalf("Resolve #2: %v", err)
	}
	if res2.Source != ai.SourceLock || !res2.Cached || calls.Load() != 1 {
		t.Fatalf("expected lock cache hit with 0 extra calls, got source=%s calls=%d", res2.Source, calls.Load())
	}
}

package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAICompatConfig configura el cliente HTTP ligero compatible con /v1/chat/completions
// (Ollama, LM Studio, llama.cpp server, vLLM, OpenAI, Gemini OpenAI-compat, etc.).
type OpenAICompatConfig struct {
	Endpoint    string        // ej. "http://localhost:11434/v1"
	Model       string        // ej. "qwen2.5-coder:7b"
	APIKey      string        // opcional para servidores locales
	Timeout     time.Duration // por defecto 60s
	Temperature float64       // por defecto 0 (máximo determinismo)
	HTTPClient  *http.Client
}

// OpenAICompatResolver resuelve nodos HoleIR usando un modelo ligero vía HTTP estándar.
type OpenAICompatResolver struct {
	cfg    OpenAICompatConfig
	client *http.Client
}

// NewOpenAICompatResolver construye un OpenAICompatResolver con defaults seguros.
func NewOpenAICompatResolver(cfg OpenAICompatConfig) *OpenAICompatResolver {
	if cfg.Endpoint == "" {
		cfg.Endpoint = "http://localhost:11434/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "qwen2.5-coder:7b"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	return &OpenAICompatResolver{cfg: cfg, client: client}
}

type chatRequest struct {
	Model       string        `json:"model"`
	Temperature float64       `json:"temperature"`
	Messages    []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

const gnoSystemPrompt = `You are gnofy's surgical Solidity-to-Gno compiler assistant.
Translate ONLY the provided Solidity/Yul snippet into valid Gno statements (strictly Go 1.17 specification + Gno v1.8 stdlib).

STRICT RULES:
1. Output ONLY raw Gno statements that fit inside the enclosing function body. Do NOT output package declarations, imports, function signatures, explanations, or markdown fences.
2. Strictly Go 1.17: NEVER use generics, type parameters, min/max/clear builtins, goroutines (go), or channels (chan).
3. Available Gno v1.8 APIs:
   - Caller inside crossing func (cur realm): cur.Previous().Address()
   - Current realm address inside crossing func: cur.Address()
   - Attached native coins in tx: unsafe.OriginSend().AmountOf("ugnot") (returns int64)
   - Send native coins from realm/origin:
     b := banker.NewBanker(banker.BankerTypeOriginSend, cur)
     b.SendCoins(cur.Previous().Address(), recipient, chain.NewCoins(chain.NewCoin("ugnot", amount)))
   - Readonly balance: banker.NewReadonlyBanker().GetCoin(addr, "ugnot")
   - Emit event: chain.Emit("Name", "k", "v")
4. Available embedded solrt helpers (in same package, no prefix needed):
   - Require(cond bool, msg string), Revert(msg string), Assert(cond bool)
   - U256, U256Zero(), U256FromU64(uint64), U256FromString(string), MulDiv(x, y, denom U256)
   - U256 methods: Add, Sub, Mul, Div, Mod, Eq, Neq, Lt, Lte, Gt, Gte, Uint64, String, IsZero
   - ERC20Token, ResolveERC20(addr address), SafeERC20Transfer(tok, to, amt), SafeERC20TransferFrom(tok, from, to, amt)
   - OrderedMap, KeyJoin(parts ...string)`

// Resolve solicita al modelo la traducción quirúrgica de un único HoleRequest.
func (r *OpenAICompatResolver) Resolve(ctx context.Context, req HoleRequest) (HoleResolution, error) {
	userPrompt := buildUserPrompt(req)

	body, err := json.Marshal(chatRequest{
		Model:       r.cfg.Model,
		Temperature: r.cfg.Temperature,
		Messages: []chatMessage{
			{Role: "system", Content: gnoSystemPrompt},
			{Role: "user", Content: userPrompt},
		},
	})
	if err != nil {
		return HoleResolution{}, fmt.Errorf("serializando petición IA: %w", err)
	}

	url := strings.TrimRight(r.cfg.Endpoint, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return HoleResolution{}, fmt.Errorf("creando petición HTTP IA: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if r.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+r.cfg.APIKey)
	}

	resp, err := r.client.Do(httpReq)
	if err != nil {
		return HoleResolution{}, fmt.Errorf("llamando endpoint IA %s: %w", url, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return HoleResolution{}, fmt.Errorf("leyendo respuesta IA: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return HoleResolution{}, fmt.Errorf("endpoint IA respondió HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return HoleResolution{}, fmt.Errorf("parseando JSON de IA: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return HoleResolution{}, fmt.Errorf("error devuelto por modelo IA: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return HoleResolution{}, fmt.Errorf("respuesta IA sin choices")
	}

	code := StripCodeFences(parsed.Choices[0].Message.Content)
	if code == "" {
		return HoleResolution{}, fmt.Errorf("respuesta IA vacía para hueco %s", req.Hole.ID)
	}

	model := parsed.Model
	if model == "" {
		model = r.cfg.Model
	}

	return HoleResolution{
		GnoCode: code,
		Model:   model,
		Source:  SourceAIFresh,
		Cached:  false,
	}, nil
}

func buildUserPrompt(req HoleRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Contract: %s\n", req.ContractName)
	if req.FunctionSignature != "" {
		fmt.Fprintf(&b, "Enclosing Gno function: %s\n", req.FunctionSignature)
	}
	if req.StateVarsSummary != "" {
		fmt.Fprintf(&b, "State variables in scope:\n%s\n", req.StateVarsSummary)
	}
	if req.Hole != nil {
		fmt.Fprintf(&b, "Diagnostic reason: %s (hole_id=%s)\n", req.Hole.Reason, req.Hole.ID)
		fmt.Fprintf(&b, "Original Solidity snippet to replace:\n```solidity\n%s\n```\n", req.Hole.OriginalSolidity)
	}
	if req.PreviousError != "" {
		fmt.Fprintf(&b, "\nPREVIOUS ATTEMPT FAILED `gno test` WITH ERROR:\n%s\nFix the code so it compiles cleanly in Gno v1.8 (Go 1.17).\n", req.PreviousError)
	}
	b.WriteString("\nReturn ONLY the replacement Gno statements:")
	return b.String()
}

// StripCodeFences limpia bloques ```go / ```gno si el modelo los incluyó pese al prompt.
func StripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = s[nl+1:]
		}
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
	}
	return strings.TrimSpace(s)
}

package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/nextlevelbuilder/goclaw/internal/providers/acp"
)

// acpSessionEntry tracks a live ACP session for one goclaw conversation.
type acpSessionEntry struct {
	id       string          // ACP session ID returned by session/new or session/load
	proc     *acp.ACPProcess // process that owns this session (for respawn detection)
	lastUsed time.Time
	mcpKey   string
	model    string // model last applied via session/set_model
}

// ACPProvider implements Provider by orchestrating ACP-compatible agent subprocesses.
// One shared Gemini process is used; each goclaw conversation gets its own ACP session.
type ACPProvider struct {
	name          string
	pool          *acp.ProcessPool
	bridge        *acp.ToolBridge
	defaultModel  string
	permMode      string
	poolKey       string // key for the shared process in the pool (binary + args)
	sessionModel  string
	forceModel    bool // when true, sessionModel always wins over a per-request model override
	sessionMeta   json.RawMessage
	noClientTools bool
	bridgeURL     string
	bridgeToken   string

	acpSessions sync.Map // goclawSessionKey → *acpSessionEntry
	sessionMu   sync.Map // goclawSessionKey → *sync.Mutex (prevents concurrent session creation)

	done      chan struct{}
	closeOnce sync.Once
}

// ACPOption configures an ACPProvider.
type ACPOption func(*ACPProvider)

// WithACPName overrides the provider name (default: "acp").
func WithACPName(name string) ACPOption {
	return func(p *ACPProvider) {
		if name != "" {
			p.name = name
		}
	}
}

// WithACPModel sets the default model/agent name.
func WithACPModel(model string) ACPOption {
	return func(p *ACPProvider) {
		if model != "" {
			p.defaultModel = model
		}
	}
}

// WithACPPermMode sets the permission mode for the tool bridge.
func WithACPPermMode(mode string) ACPOption {
	return func(p *ACPProvider) {
		if mode != "" {
			p.permMode = mode
		}
	}
}

func WithACPSessionMeta(meta json.RawMessage) ACPOption {
	return func(p *ACPProvider) { p.sessionMeta = meta }
}

func WithACPSessionModel(model string) ACPOption {
	return func(p *ACPProvider) { p.sessionModel = model }
}

// WithACPForceModel makes the provider's own session model always win over a
// per-request model override (e.g. the agent's configured model).
func WithACPForceModel(force bool) ACPOption {
	return func(p *ACPProvider) { p.forceModel = force }
}

func WithACPClientToolsDisabled() ACPOption {
	return func(p *ACPProvider) { p.noClientTools = true }
}

// NewACPProvider creates a provider that orchestrates ACP agents as subprocesses.
func NewACPProvider(binary string, args []string, workDir string, idleTTL time.Duration, denyPatterns []*regexp.Regexp, opts ...ACPOption) *ACPProvider {
	// Pool key identifies the shared process: binary + args combination
	poolKey := binary
	if len(args) > 0 {
		poolKey += "|" + strings.Join(args, " ")
	}

	p := &ACPProvider{
		name:         "acp",
		defaultModel: "claude",
		poolKey:      poolKey,
		done:         make(chan struct{}),
	}
	for _, opt := range opts {
		opt(p)
	}

	var bridgeOpts []acp.ToolBridgeOption
	if len(denyPatterns) > 0 {
		bridgeOpts = append(bridgeOpts, acp.WithDenyPatterns(denyPatterns))
	}
	if p.permMode != "" {
		bridgeOpts = append(bridgeOpts, acp.WithPermMode(p.permMode))
	}
	if p.noClientTools {
		bridgeOpts = append(bridgeOpts, acp.WithClientToolsDisabled())
	}
	p.bridge = acp.NewToolBridge(workDir, bridgeOpts...)

	p.pool = acp.NewProcessPool(binary, args, workDir, idleTTL)
	p.pool.SetToolHandler(p.bridge.Handle)

	go p.sessionReaper()
	return p
}

// sessionReaper removes ACP sessions idle for more than 30 minutes.
// Sends session/cancel to release resources on the agent side before purging locally.
func (p *ACPProvider) sessionReaper() {
	const sessionIdleTTL = 30 * time.Minute
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.acpSessions.Range(func(key, value any) bool {
				entry := value.(*acpSessionEntry)
				if time.Since(entry.lastUsed) > sessionIdleTTL {
					slog.Info("acp: expiring idle session", "goclaw_session", key, "sid", entry.id)
					if entry.proc != nil {
						_ = entry.proc.Cancel(entry.id)
					}
					p.acpSessions.Delete(key)
				}
				return true
			})
		case <-p.done:
			return
		}
	}
}

// resolveSession returns the ACP session ID for a goclaw session key.
// It creates a new session if none exists, or reloads it after a process respawn.
// A per-key mutex prevents concurrent creation races for the same session.
// model is the caller's requested model (e.g. the agent's configured model);
// if empty, or if the provider forces its own model, the provider's own
// default session model is used instead.
func (p *ACPProvider) resolveSession(ctx context.Context, proc *acp.ACPProcess, goclawKey string, servers []acp.MCPServer, model string) (string, bool, error) {
	actual, _ := p.sessionMu.LoadOrStore(goclawKey, &sync.Mutex{})
	mu := actual.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	effModel := model
	if p.forceModel || effModel == "" {
		effModel = p.sessionModel
	}

	mcpKey := mcpServersKey(servers)
	if val, ok := p.acpSessions.Load(goclawKey); ok {
		entry := val.(*acpSessionEntry)
		if entry.proc == proc && entry.mcpKey == mcpKey {
			// Same process instance: session is still live, just update last-used
			entry.lastUsed = time.Now()
			if effModel != "" && effModel != entry.model {
				p.applySessionModel(ctx, proc, entry.id, effModel)
				entry.model = effModel
			}
			return entry.id, false, nil
		}
		slog.Info("acp: restoring session",
			"goclaw_session", goclawKey, "old_sid", entry.id, "respawned", entry.proc != proc)
		if proc.AgentCaps().LoadSession {
			sid, err := proc.LoadSessionWithMeta(ctx, entry.id, p.sessionMeta, servers)
			if err == nil {
				p.applySessionModel(ctx, proc, sid, effModel)
				p.acpSessions.Store(goclawKey, &acpSessionEntry{id: sid, proc: proc, lastUsed: time.Now(), mcpKey: mcpKey, model: effModel})
				return sid, false, nil
			}
			slog.Warn("acp: session/load failed, creating new session", "old_sid", entry.id, "error", err)
		}
		// session/load not supported or failed — fall through to create new
	}

	slog.Info("acp: creating new session", "goclaw_session", goclawKey, "pool_key", p.poolKey)
	sid, err := proc.NewSessionWithMeta(ctx, p.sessionMeta, servers)
	if err != nil && len(servers) > 0 {
		slog.Warn("acp: session/new with MCP servers failed, retrying without tools", "provider", p.name, "error", err)
		sid, err = proc.NewSessionWithMeta(ctx, p.sessionMeta, nil)
	}
	if err != nil {
		return "", false, err
	}
	p.applySessionModel(ctx, proc, sid, effModel)
	p.acpSessions.Store(goclawKey, &acpSessionEntry{id: sid, proc: proc, lastUsed: time.Now(), mcpKey: mcpKey, model: effModel})
	return sid, true, nil
}

func (p *ACPProvider) applySessionModel(ctx context.Context, proc *acp.ACPProcess, sid, model string) {
	if model == "" {
		return
	}
	if err := proc.SetModel(ctx, sid, model); err != nil {
		slog.Warn("acp: set model failed", "provider", p.name, "model", model, "error", err)
	}
}

func (p *ACPProvider) Name() string         { return p.name }
func (p *ACPProvider) DefaultModel() string { return p.defaultModel }

// Capabilities implements CapabilitiesAware for pipeline code-path selection.
func (p *ACPProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		Streaming:        true,
		ToolCalling:      true,
		StreamWithTools:  true,
		Thinking:         true,
		Vision:           false,
		CacheControl:     false,
		MaxContextWindow: 200_000,
		TokenizerID:      "cl100k_base",
	}
}

// Chat sends a prompt and returns the complete response (non-streaming).
func (p *ACPProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	sessionKey := extractStringOpt(req.Options, OptSessionKey)
	if sessionKey == "" {
		sessionKey = fmt.Sprintf("temp-%d", time.Now().UnixNano())
	}

	proc, err := p.pool.GetOrSpawn(ctx, p.poolKey)
	if err != nil {
		return nil, fmt.Errorf("acp: spawn failed: %w", err)
	}

	acpSessionID, fresh, err := p.resolveSession(ctx, proc, sessionKey, p.mcpServers(req, sessionKey), req.Model)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(sessionKey, "temp-") {
		defer p.purgeSession(sessionKey)
	}

	content := extractACPContent(req, fresh)
	if len(content) == 0 {
		return nil, fmt.Errorf("acp: no user message in request")
	}

	ctx = acp.WithGoclawSession(ctx, sessionKey)

	var buf strings.Builder
	var updateCount int
	promptResp, err := proc.Prompt(ctx, acpSessionID, content, func(update acp.SessionUpdate) {
		if update.Message != nil {
			for _, block := range update.Message.Content {
				if block.Type == "text" {
					buf.WriteString(block.Text)
					updateCount++
				}
			}
		}
	})
	if err != nil {
		slog.Error("acp: chat error", "session", sessionKey, "sid", acpSessionID, "error", err)
		return &ChatResponse{
			Content:      fmt.Sprintf("[ACP Error] %v", err),
			FinishReason: "error",
		}, err
	}

	slog.Info("acp: chat completed", "session", sessionKey, "sid", acpSessionID,
		"stopReason", mapStopReason(promptResp), "updates", updateCount, "contentLen", buf.Len())
	inputTokens := estimateACPTokens(acpContentText(content))
	outputTokens := estimateACPTokens(buf.String())
	return &ChatResponse{
		Content:      buf.String(),
		FinishReason: mapStopReason(promptResp),
		Usage:        &Usage{PromptTokens: inputTokens, CompletionTokens: outputTokens, TotalTokens: inputTokens + outputTokens},
	}, nil
}

// ChatStream sends a prompt and streams response chunks via onChunk callback.
func (p *ACPProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	sessionKey := extractStringOpt(req.Options, OptSessionKey)
	if sessionKey == "" {
		sessionKey = fmt.Sprintf("temp-%d", time.Now().UnixNano())
	}

	proc, err := p.pool.GetOrSpawn(ctx, p.poolKey)
	if err != nil {
		return nil, fmt.Errorf("acp: spawn failed: %w", err)
	}

	acpSessionID, fresh, err := p.resolveSession(ctx, proc, sessionKey, p.mcpServers(req, sessionKey), req.Model)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(sessionKey, "temp-") {
		defer p.purgeSession(sessionKey)
	}

	content := extractACPContent(req, fresh)
	if len(content) == 0 {
		return nil, fmt.Errorf("acp: no user message in request")
	}

	ctx = acp.WithGoclawSession(ctx, sessionKey)

	// done channel ensures the cancel goroutine exits cleanly on normal completion,
	// preventing it from sending a spurious session/cancel after the prompt finishes.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				_ = proc.Cancel(acpSessionID)
			}
		case <-done:
		}
	}()

	var buf strings.Builder
	var updateCount int
	var hasContent bool
	stripThinking, _ := req.Options[OptStripThinking].(bool)
	lastToolStatus := make(map[string]string) // toolCallId → last status surfaced, "__acp_plan__" → last rendered plan
	promptResp, err := proc.Prompt(ctx, acpSessionID, content, func(update acp.SessionUpdate) {
		if update.Message != nil {
			for _, block := range update.Message.Content {
				if block.Type == "text" {
					hasContent = true
					onChunk(StreamChunk{Content: block.Text})
					buf.WriteString(block.Text)
					updateCount++
				}
			}
		}
		// Progress as thinking is only emitted before answer content starts,
		// preventing late thinking from overwriting streamed answer text.
		if !hasContent {
			if text := acpProgressThinking(&update, lastToolStatus, stripThinking); text != "" {
				onChunk(StreamChunk{Thinking: text})
			}
		}
	})
	if err != nil {
		slog.Error("acp: chat error", "session", sessionKey, "sid", acpSessionID, "error", err)
		return &ChatResponse{
			Content:      fmt.Sprintf("[ACP Error] %v", err),
			FinishReason: "error",
		}, err
	}

	onChunk(StreamChunk{Done: true})
	slog.Info("acp: chat stream completed", "session", sessionKey, "sid", acpSessionID,
		"stopReason", mapStopReason(promptResp), "updates", updateCount, "contentLen", buf.Len())

	inputTokens := estimateACPTokens(acpContentText(content))
	outputTokens := estimateACPTokens(buf.String())
	return &ChatResponse{
		Content:      buf.String(),
		FinishReason: mapStopReason(promptResp),
		Usage:        &Usage{PromptTokens: inputTokens, CompletionTokens: outputTokens, TotalTokens: inputTokens + outputTokens},
	}, nil
}

// acpProgressThinking turns non-text ACP session updates into a short progress
// string for StreamChunk.Thinking, so the UI shows what the agent is doing
// instead of a black box. Returns "" when there is nothing to surface.
// lastStatus dedupes tool_call_update by toolCallId and plans by content;
// session/update notifications arrive sequentially from the conn read loop,
// so it needs no lock.
func acpProgressThinking(u *acp.SessionUpdate, lastStatus map[string]string, stripThinking ...bool) string {
	shouldStrip := len(stripThinking) > 0 && stripThinking[0]
	switch u.Update.SessionUpdate {
	case "agent_thought_chunk":
		if shouldStrip {
			return ""
		}
		return u.ContentText()
	case "tool_call", "tool_call_update":
		id := strings.TrimSpace(u.Update.ToolCallID)
		label := strings.TrimSpace(u.Update.Title)
		if label == "" {
			label = strings.TrimSpace(u.Update.Kind)
		}
		if label == "" {
			label = id
		}
		if label == "" {
			return ""
		}
		status := strings.TrimSpace(u.Update.Status)
		if status == "" {
			status = "pending"
		}
		key := id
		if key == "" {
			key = label
		}
		if lastStatus[key] == status {
			return ""
		}
		lastStatus[key] = status
		return fmt.Sprintf("\n🔧 %s [%s]\n", label, status)
	case "plan":
		entries := u.Update.Entries
		if len(entries) == 0 {
			return ""
		}
		const maxPlanEntries = 10
		var sb strings.Builder
		count := len(entries)
		if count > maxPlanEntries {
			count = maxPlanEntries
		}
		for i := 0; i < count; i++ {
			e := entries[i]
			fmt.Fprintf(&sb, "\n- [%s] %s", e.Status, e.Content)
		}
		if len(entries) > maxPlanEntries {
			fmt.Fprintf(&sb, "\n- ... (+%d more)", len(entries)-maxPlanEntries)
		}
		rendered := "\n📋 Plan:" + sb.String() + "\n"
		if lastStatus["__acp_plan__"] == rendered {
			return ""
		}
		lastStatus["__acp_plan__"] = rendered
		return rendered
	}
	return ""
}

// purgeSession removes a session entry from both tracking maps.
// Sends session/cancel to release resources on the agent side before purging locally.
// Used to immediately discard one-shot (temp-) sessions after completion.
func (p *ACPProvider) purgeSession(key string) {
	if val, ok := p.acpSessions.Load(key); ok {
		entry := val.(*acpSessionEntry)
		if entry.proc != nil {
			_ = entry.proc.Cancel(entry.id)
		}
	}
	p.acpSessions.Delete(key)
	p.sessionMu.Delete(key)
	slog.Info("acp: purged temp session", "goclaw_session", key)
}

// Close shuts down all subprocesses and cleans up terminals.
func (p *ACPProvider) Close() error {
	p.closeOnce.Do(func() {
		close(p.done)
	})
	_ = p.bridge.Close()
	return p.pool.Close()
}

// estimateACPTokens roughly estimates token count from text length.
// shortcut: the ACP protocol (session/prompt, session/update) carries no token
// usage at all, so Chat/ChatStream can't report real Usage like HTTP-based
// providers (OpenRouter etc.) do. ~4 chars/token is a standard English/code
// approximation; upgrade to a real count if agy/ACP ever adds usage reporting.
func estimateACPTokens(text string) int {
	if text == "" {
		return 0
	}
	if n := utf8.RuneCountInString(text) / 4; n > 0 {
		return n
	}
	return 1
}

// acpContentText joins the text blocks of an ACP content payload, for token estimation.
func acpContentText(blocks []acp.ContentBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

// extractACPContent extracts user message + images from ChatRequest into ACP ContentBlocks.
func extractACPContent(req ChatRequest, replayHistory bool) []acp.ContentBlock {
	systemPrompt, userMsg, images := extractFromMessages(req.Messages)
	if userMsg == "" {
		return nil
	}

	var blocks []acp.ContentBlock

	// Prepend system prompt to user message (ACP agents have no separate system prompt API)
	text := userMsg
	if replayHistory {
		if history := buildACPHistoryReplay(req.Messages); history != "" {
			text = history + "\n\n" + text
		}
	}
	if systemPrompt != "" {
		text = systemPrompt + "\n\n" + text
	}
	blocks = append(blocks, acp.ContentBlock{Type: "text", Text: text})

	for _, img := range images {
		blocks = append(blocks, acp.ContentBlock{
			Type:     "image",
			Data:     img.Data,
			MimeType: img.MimeType,
		})
	}

	return blocks
}

// mapStopReason converts ACP stopReason to GoClaw finish reason.
func mapStopReason(resp *acp.PromptResponse) string {
	if resp == nil {
		return "stop"
	}
	switch resp.StopReason {
	case "max_tokens", "maxContextLength":
		return "length"
	case "cancelled":
		return "stop"
	default:
		return "stop"
	}
}

package pageindex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Clarit-AI/Plexium/internal/plugins"
)

// stubRetrievalPlugin implements plugins.RetrievalPlugin for server-level
// tool-aggregation tests, so we can assert wiring without pulling in the
// markedup package and its on-disk index loader.
type stubRetrievalPlugin struct {
	name  string
	tools []plugins.MCPToolDef
	// callResults maps tool name -> response value returned by HandleMCPCall.
	callResults map[string]any
}

func (s *stubRetrievalPlugin) Name() string                  { return s.name }
func (s *stubRetrievalPlugin) Version() string               { return "stub-v0" }
func (s *stubRetrievalPlugin) Description() string           { return "stub" }
func (s *stubRetrievalPlugin) Type() plugins.PluginType      { return plugins.PluginTypeRetrieval }
func (s *stubRetrievalPlugin) Initialize(_ context.Context, _ string) error { return nil }
func (s *stubRetrievalPlugin) Search(_ context.Context, _ string, _ plugins.SearchOpts) ([]plugins.PluginSearchResult, error) {
	return nil, nil
}
func (s *stubRetrievalPlugin) MCPTools() []plugins.MCPToolDef { return s.tools }
func (s *stubRetrievalPlugin) HandleMCPCall(_ context.Context, tool string, _ json.RawMessage) (any, error) {
	if v, ok := s.callResults[tool]; ok {
		return v, nil
	}
	return nil, fmt.Errorf("stub: unknown tool %q", tool)
}

func TestHandleRequest_ToolsList(t *testing.T) {
	wikiDir := setupTestWiki(t)
	s := NewServer(wikiDir)
	if err := s.Index.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}

	resp := s.handleRequest(req)

	if resp.JSONRPC != "2.0" {
		t.Errorf("JSONRPC = %q, want %q", resp.JSONRPC, "2.0")
	}

	if resp.ID != 1 {
		t.Errorf("ID = %v, want 1", resp.ID)
	}

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	// Result should contain tools list
	resultMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %T", resp.Result)
	}

	tools, ok := resultMap["tools"].([]ToolDefinition)
	if !ok {
		t.Fatalf("tools field is not []ToolDefinition: %T", resultMap["tools"])
	}

	if len(tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(tools))
	}

	// Verify tool names
	expectedTools := map[string]bool{
		"pageindex_search":     false,
		"pageindex_get_page":   false,
		"pageindex_list_pages": false,
	}

	for _, tool := range tools {
		if _, ok := expectedTools[tool.Name]; ok {
			expectedTools[tool.Name] = true
		}
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("expected tool %q not found", name)
		}
	}
}

func TestHandleRequest_ToolsCallSearch(t *testing.T) {
	wikiDir := setupTestWiki(t)
	s := NewServer(wikiDir)
	if err := s.Index.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	args, _ := json.Marshal(map[string]string{"query": "authentication"})
	params, _ := json.Marshal(ToolCallParams{
		Name:      "pageindex_search",
		Arguments: args,
	})

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/call",
		Params:  params,
	}

	resp := s.handleRequest(req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	// Result should be a ToolResult
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}

	var toolResult ToolResult
	if err := json.Unmarshal(resultBytes, &toolResult); err != nil {
		t.Fatalf("unmarshal ToolResult: %v", err)
	}

	if len(toolResult.Content) == 0 {
		t.Fatal("expected content in tool result")
	}

	if toolResult.Content[0].Type != "text" {
		t.Errorf("content type = %q, want %q", toolResult.Content[0].Type, "text")
	}

	// Parse the search results from the text
	var results []SearchResult
	if err := json.Unmarshal([]byte(toolResult.Content[0].Text), &results); err != nil {
		t.Fatalf("unmarshal search results: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected search results, got none")
	}
}

func TestHandleRequest_ToolsCallGetPage(t *testing.T) {
	wikiDir := setupTestWiki(t)
	s := NewServer(wikiDir)
	if err := s.Index.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	args, _ := json.Marshal(map[string]string{"path": "modules/auth.md"})
	params, _ := json.Marshal(ToolCallParams{
		Name:      "pageindex_get_page",
		Arguments: args,
	})

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "tools/call",
		Params:  params,
	}

	resp := s.handleRequest(req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}

	var toolResult ToolResult
	if err := json.Unmarshal(resultBytes, &toolResult); err != nil {
		t.Fatalf("unmarshal ToolResult: %v", err)
	}

	if toolResult.IsError {
		t.Error("expected success, got error result")
	}

	if len(toolResult.Content) == 0 {
		t.Fatal("expected content in tool result")
	}

	// Parse the page content
	var page PageContent
	if err := json.Unmarshal([]byte(toolResult.Content[0].Text), &page); err != nil {
		t.Fatalf("unmarshal PageContent: %v", err)
	}

	if page.Info.Title != "Authentication Module" {
		t.Errorf("page title = %q, want %q", page.Info.Title, "Authentication Module")
	}
}

func TestHandleRequest_ToolsCallListPages(t *testing.T) {
	wikiDir := setupTestWiki(t)
	s := NewServer(wikiDir)
	if err := s.Index.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	params, _ := json.Marshal(ToolCallParams{
		Name:      "pageindex_list_pages",
		Arguments: json.RawMessage("{}"),
	})

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      4,
		Method:  "tools/call",
		Params:  params,
	}

	resp := s.handleRequest(req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}

	var toolResult ToolResult
	if err := json.Unmarshal(resultBytes, &toolResult); err != nil {
		t.Fatalf("unmarshal ToolResult: %v", err)
	}

	var pages []PageInfo
	if err := json.Unmarshal([]byte(toolResult.Content[0].Text), &pages); err != nil {
		t.Fatalf("unmarshal pages: %v", err)
	}

	if len(pages) == 0 {
		t.Error("expected pages in list, got none")
	}
}

func TestHandleRequest_UnknownMethod(t *testing.T) {
	wikiDir := setupTestWiki(t)
	s := NewServer(wikiDir)
	if err := s.Index.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      5,
		Method:  "nonexistent/method",
	}

	resp := s.handleRequest(req)

	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}

	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want %d", resp.Error.Code, -32601)
	}
}

func TestHandleRequest_UnknownTool(t *testing.T) {
	wikiDir := setupTestWiki(t)
	s := NewServer(wikiDir)
	if err := s.Index.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	params, _ := json.Marshal(ToolCallParams{
		Name:      "nonexistent_tool",
		Arguments: json.RawMessage("{}"),
	})

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      6,
		Method:  "tools/call",
		Params:  params,
	}

	resp := s.handleRequest(req)

	if resp.Error == nil {
		t.Fatal("expected error for unknown tool")
	}

	if resp.Error.Code != -32602 {
		t.Errorf("error code = %d, want %d", resp.Error.Code, -32602)
	}
}

func TestHandleRequest_Initialize(t *testing.T) {
	wikiDir := setupTestWiki(t)
	s := NewServer(wikiDir)
	if err := s.Index.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      7,
		Method:  "initialize",
	}

	resp := s.handleRequest(req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	resultMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %T", resp.Result)
	}

	if _, ok := resultMap["protocolVersion"]; !ok {
		t.Error("expected protocolVersion in initialize response")
	}

	serverInfo, ok := resultMap["serverInfo"].(map[string]interface{})
	if !ok {
		t.Fatal("expected serverInfo map")
	}

	if serverInfo["name"] != "plexium-pageindex" {
		t.Errorf("server name = %v, want %q", serverInfo["name"], "plexium-pageindex")
	}
}

func TestJSONRPCResponseFormat(t *testing.T) {
	wikiDir := setupTestWiki(t)
	s := NewServer(wikiDir)
	if err := s.Index.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      42,
		Method:  "tools/list",
	}

	resp := s.handleRequest(req)

	// Verify it serializes to valid JSON
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}

	// Should have required JSON-RPC fields
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	if parsed["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v, want %q", parsed["jsonrpc"], "2.0")
	}

	// ID should be preserved as number
	if id, ok := parsed["id"].(float64); !ok || id != 42 {
		t.Errorf("id = %v, want 42", parsed["id"])
	}

	// Should have result, not error
	if _, ok := parsed["result"]; !ok {
		t.Error("expected result field in response")
	}
}

func TestServer_StdioLoop(t *testing.T) {
	wikiDir := setupTestWiki(t)
	s := NewServer(wikiDir)

	// Create a request
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}
	reqData, _ := json.Marshal(req)

	// Set up stdin/stdout
	var output bytes.Buffer
	s.reader = strings.NewReader(string(reqData) + "\n")
	s.writer = &output

	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Parse the output
	outStr := strings.TrimSpace(output.String())
	if outStr == "" {
		t.Fatal("expected output from server, got empty")
	}

	var resp JSONRPCResponse
	if err := json.Unmarshal([]byte(outStr), &resp); err != nil {
		t.Fatalf("output is not valid JSON-RPC: %v (output: %s)", err, outStr)
	}

	if resp.Error != nil {
		t.Errorf("unexpected error in response: %v", resp.Error)
	}
}

// TestHandleRequest_ToolsList_WithRegistry verifies that retrieval-plugin
// MCP tools are appended to the builtin set when a Registry is attached.
// Asserts: 3 builtin + 3 stub = 6 tools, and that the stub names appear.
func TestHandleRequest_ToolsList_WithRegistry(t *testing.T) {
	wikiDir := setupTestWiki(t)
	s := NewServer(wikiDir)
	if err := s.Index.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	stub := &stubRetrievalPlugin{
		name: "stub-retrieval",
		tools: []plugins.MCPToolDef{
			{Name: "stub_search", Description: "stub semantic search",
				InputSchema: map[string]any{"type": "object"}},
			{Name: "stub_traverse", Description: "stub graph walk",
				InputSchema: map[string]any{"type": "object"}},
			{Name: "stub_graph", Description: "stub graph summary",
				InputSchema: map[string]any{"type": "object"}},
		},
		callResults: map[string]any{
			"stub_search": []map[string]any{{"page": "x.md", "score": 0.9}},
		},
	}
	reg := plugins.NewRegistry()
	if err := reg.Register(stub); err != nil {
		t.Fatalf("register stub: %v", err)
	}
	s.Registry = reg

	resp := s.handleRequest(JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	resultMap := resp.Result.(map[string]interface{})
	tools := resultMap["tools"].([]ToolDefinition)
	if len(tools) != 6 {
		t.Errorf("expected 6 tools (3 builtin + 3 stub), got %d", len(tools))
	}

	want := map[string]bool{
		"pageindex_search": false, "pageindex_get_page": false, "pageindex_list_pages": false,
		"stub_search": false, "stub_traverse": false, "stub_graph": false,
	}
	for _, td := range tools {
		if _, ok := want[td.Name]; ok {
			want[td.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("expected tool %q in tools/list response", name)
		}
	}
}

// TestHandleRequest_ToolsCall_DelegatesToPlugin verifies that an unknown
// builtin tool is dispatched to a registered retrieval plugin and its
// result is wrapped in the standard ToolResult envelope.
func TestHandleRequest_ToolsCall_DelegatesToPlugin(t *testing.T) {
	wikiDir := setupTestWiki(t)
	s := NewServer(wikiDir)
	if err := s.Index.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	stub := &stubRetrievalPlugin{
		name: "stub-retrieval",
		tools: []plugins.MCPToolDef{
			{Name: "stub_search", Description: "stub", InputSchema: map[string]any{}},
		},
		callResults: map[string]any{
			"stub_search": []map[string]any{{"page": "auth.md", "score": 0.42}},
		},
	}
	reg := plugins.NewRegistry()
	if err := reg.Register(stub); err != nil {
		t.Fatalf("register: %v", err)
	}
	s.Registry = reg

	args, _ := json.Marshal(map[string]any{"query": "anything"})
	params, _ := json.Marshal(ToolCallParams{Name: "stub_search", Arguments: args})
	resp := s.handleRequest(JSONRPCRequest{JSONRPC: "2.0", ID: 99, Method: "tools/call", Params: params})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	rb, _ := json.Marshal(resp.Result)
	var tr ToolResult
	if err := json.Unmarshal(rb, &tr); err != nil {
		t.Fatalf("unmarshal ToolResult: %v", err)
	}
	if tr.IsError {
		t.Fatalf("expected success, got error result: %s", tr.Content[0].Text)
	}
	if !strings.Contains(tr.Content[0].Text, "auth.md") {
		t.Errorf("expected stub payload in response, got: %s", tr.Content[0].Text)
	}
}

// TestHandleRequest_ToolsList_NilRegistry confirms the pre-D4 behavior
// is preserved when no Registry is attached: only 3 builtin tools.
func TestHandleRequest_ToolsList_NilRegistry(t *testing.T) {
	wikiDir := setupTestWiki(t)
	s := NewServer(wikiDir)
	if err := s.Index.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	// s.Registry is nil — that is the pre-D4 default.

	resp := s.handleRequest(JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	tools := resp.Result.(map[string]interface{})["tools"].([]ToolDefinition)
	if len(tools) != 3 {
		t.Errorf("expected exactly 3 builtin tools without registry, got %d", len(tools))
	}
}

func TestServer_ParseError(t *testing.T) {
	wikiDir := setupTestWiki(t)
	s := NewServer(wikiDir)

	var output bytes.Buffer
	s.reader = strings.NewReader("this is not json\n")
	s.writer = &output

	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	outStr := strings.TrimSpace(output.String())
	var resp JSONRPCResponse
	if err := json.Unmarshal([]byte(outStr), &resp); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected parse error")
	}

	if resp.Error.Code != -32700 {
		t.Errorf("error code = %d, want %d", resp.Error.Code, -32700)
	}
}

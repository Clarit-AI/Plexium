package pageindex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// JSONRPCRequest represents a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ToolDefinition describes an MCP tool for tools/list.
type ToolDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// ToolCallParams represents the params for a tools/call request.
type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResult represents the result of a tool call.
type ToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent represents a content block in a tool result.
type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Server implements an MCP-compatible server for PageIndex.
// It reads JSON-RPC requests from stdin and writes responses to stdout.
type Server struct {
	Index  *PageIndex
	Port   int // 0 = stdio mode
	reader io.Reader
	writer io.Writer
}

// NewServer creates a new MCP server wrapping the given wiki root.
func NewServer(wikiRoot string) *Server {
	idx := New(wikiRoot)
	return &Server{
		Index:  idx,
		reader: os.Stdin,
		writer: os.Stdout,
	}
}

// Start launches the MCP server. In stdio mode, reads from stdin.
func (s *Server) Start() error {
	if err := s.Index.Load(); err != nil {
		return fmt.Errorf("loading page index: %w", err)
	}

	scanner := bufio.NewScanner(s.reader)
	// Increase buffer for large requests
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			resp := JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      nil,
				Error: &RPCError{
					Code:    -32700,
					Message: "Parse error",
				},
			}
			s.writeResponse(resp)
			continue
		}

		resp := s.handleRequest(req)
		s.writeResponse(resp)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}

	return nil
}

// writeResponse serializes and writes a JSON-RPC response.
func (s *Server) writeResponse(resp JSONRPCResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		// Best-effort error response
		errResp := fmt.Sprintf(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"Internal error"}}`)
		fmt.Fprintln(s.writer, errResp)
		return
	}
	fmt.Fprintln(s.writer, string(data))
}

// handleRequest processes a single JSON-RPC 2.0 request.
func (s *Server) handleRequest(req JSONRPCRequest) JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	default:
		return JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32601,
				Message: fmt.Sprintf("Method not found: %s", req.Method),
			},
		}
	}
}

// handleInitialize returns server capabilities.
func (s *Server) handleInitialize(req JSONRPCRequest) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "plexium-pageindex",
				"version": "0.1.0",
			},
		},
	}
}

// handleToolsList returns the available tool definitions.
func (s *Server) handleToolsList(req JSONRPCRequest) JSONRPCResponse {
	tools := []ToolDefinition{
		{
			Name:        "pageindex_search",
			Description: "Search wiki pages by query. Returns ranked results with relevance scores.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query string",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "pageindex_get_page",
			Description: "Get full content of a specific wiki page by path.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Relative path to the wiki page (e.g., modules/auth.md)",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "pageindex_list_pages",
			Description: "List all indexed wiki pages with metadata.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}

	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"tools": tools,
		},
	}
}

// handleToolsCall dispatches a tool invocation.
func (s *Server) handleToolsCall(req JSONRPCRequest) JSONRPCResponse {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32602,
				Message: "Invalid params: " + err.Error(),
			},
		}
	}

	switch params.Name {
	case "pageindex_search":
		return s.callSearch(req.ID, params.Arguments)
	case "pageindex_get_page":
		return s.callGetPage(req.ID, params.Arguments)
	case "pageindex_list_pages":
		return s.callListPages(req.ID)
	default:
		return JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32602,
				Message: fmt.Sprintf("Unknown tool: %s", params.Name),
			},
		}
	}
}

// callSearch handles a pageindex_search tool call.
func (s *Server) callSearch(id interface{}, args json.RawMessage) JSONRPCResponse {
	var input struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      id,
			Error: &RPCError{
				Code:    -32602,
				Message: "Invalid arguments: " + err.Error(),
			},
		}
	}

	results := s.Index.Search(input.Query)
	data, _ := json.Marshal(results)

	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result: ToolResult{
			Content: []ToolContent{
				{Type: "text", Text: string(data)},
			},
		},
	}
}

// callGetPage handles a pageindex_get_page tool call.
func (s *Server) callGetPage(id interface{}, args json.RawMessage) JSONRPCResponse {
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      id,
			Error: &RPCError{
				Code:    -32602,
				Message: "Invalid arguments: " + err.Error(),
			},
		}
	}

	page, err := s.Index.GetPage(input.Path)
	if err != nil {
		return JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      id,
			Result: ToolResult{
				Content: []ToolContent{
					{Type: "text", Text: fmt.Sprintf("Error: %v", err)},
				},
				IsError: true,
			},
		}
	}

	data, _ := json.Marshal(page)

	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result: ToolResult{
			Content: []ToolContent{
				{Type: "text", Text: string(data)},
			},
		},
	}
}

// callListPages handles a pageindex_list_pages tool call.
func (s *Server) callListPages(id interface{}) JSONRPCResponse {
	pages := s.Index.ListPages()
	data, _ := json.Marshal(pages)

	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result: ToolResult{
			Content: []ToolContent{
				{Type: "text", Text: string(data)},
			},
		},
	}
}

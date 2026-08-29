package mcp

import (
	"encoding/json"
	"errors"
	"strconv"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var errResultMustBeObject = errors.New("MCP result must be a JSON object")

// ToolResult returns one logical document as equivalent structured and JSON text content.
func ToolResult(document any, isError bool) (*mcpsdk.CallToolResult, error) {
	canonical, structured, err := encodeDocument(document)
	if err != nil {
		return nil, err
	}
	if combinedContentBytes(canonical) > MaxResponseContentBytes {
		if isError {
			failure, ok := document.(ErrorDocument)
			if !ok {
				return nil, errResultMustBeObject
			}
			failure.Detail = "The requested failure detail exceeds the MCP response limit."
			canonical, structured, err = encodeDocument(failure)
		} else {
			revision := documentRevision(structured)
			canonical, structured, err = encodeDocument(responseTooLargeDocument(revision))
			isError = true
		}
		if err != nil {
			return nil, err
		}
	}
	if combinedContentBytes(canonical) > MaxResponseContentBytes {
		return nil, errors.New("bounded MCP error exceeds response limit")
	}
	return &mcpsdk.CallToolResult{
		Content:           []mcpsdk.Content{&mcpsdk.TextContent{Text: string(canonical)}},
		StructuredContent: structured,
		IsError:           isError,
	}, nil
}

func encodeDocument(document any) ([]byte, map[string]any, error) {
	canonical, err := json.Marshal(document)
	if err != nil {
		return nil, nil, err
	}
	var structured map[string]any
	if err = json.Unmarshal(canonical, &structured); err != nil || structured == nil {
		return nil, nil, errResultMustBeObject
	}
	return canonical, structured, nil
}

func combinedContentBytes(canonical []byte) int {
	return 2 * len(canonical)
}

func documentRevision(structured map[string]any) uint64 {
	revision, _ := structured["revision"].(string)
	value, _ := strconv.ParseUint(revision, 10, 64)
	return value
}

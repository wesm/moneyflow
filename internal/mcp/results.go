package mcp

import (
	"bytes"
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
			failure, ok := errorDocumentValue(document)
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
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.UseNumber()
	if err = decoder.Decode(&structured); err != nil || structured == nil {
		return nil, nil, errResultMustBeObject
	}
	return canonical, structured, nil
}

func errorDocumentValue(document any) (ErrorDocument, bool) {
	switch value := document.(type) {
	case ErrorDocument:
		return value, true
	case *ErrorDocument:
		if value != nil {
			return *value, true
		}
	}
	return ErrorDocument{}, false
}

func combinedContentBytes(canonical []byte) int {
	return 2 * len(canonical)
}

func documentRevision(structured map[string]any) uint64 {
	revision, _ := structured["revision"].(string)
	value, _ := strconv.ParseUint(revision, 10, 64)
	return value
}

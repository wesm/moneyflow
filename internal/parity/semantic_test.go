package parity

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSemanticFrameStrictDecodeAndGeometry(t *testing.T) {
	t.Parallel()

	valid := `{"schema_version":1,"name":"main","width":2,"height":1,"regions":[{"name":"hints","origin":{"x":0,"y":0},"width":2,"height":1,"lines":["ok"]}],"columns":[],"visible_row_ids":[],"breadcrumb":"","stats":"","flags":[],"selection_ids":[],"hints":"ok","overlay":[]}`
	var frame SemanticFrame
	require.NoError(t, decodeStrict([]byte(valid), &frame))
	require.NoError(t, frame.Validate())

	unknown := strings.Replace(valid, `"name":"main"`, `"name":"main","style":"red"`, 1)
	assert.Error(t, decodeStrict([]byte(unknown), &SemanticFrame{}))
	overflow := strings.Replace(valid, `"width":2,"height":1`, `"width":1,"height":1`, 1)
	require.NoError(t, decodeStrict([]byte(overflow), &frame))
	assert.ErrorContains(t, frame.Validate(), "geometry")
}

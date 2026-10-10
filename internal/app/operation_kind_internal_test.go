package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

// domainOperationTypes lists every OperationType constant declared by the domain package, so a
// new journal operation type fails the mapping test until it has a feedback kind.
func domainOperationTypes(t *testing.T) []domain.OperationType {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "../domain/operations.go", nil, 0)
	require.NoError(t, err)
	var types []domain.OperationType
	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok {
			return true
		}
		typeName, typed := spec.Type.(*ast.Ident)
		if !typed || typeName.Name != "OperationType" || len(spec.Values) != 1 {
			return true
		}
		literal, isLiteral := spec.Values[0].(*ast.BasicLit)
		require.True(t, isLiteral, "operation type constants must be string literals")
		text, unquoteErr := strconv.Unquote(literal.Value)
		require.NoError(t, unquoteErr)
		types = append(types, domain.OperationType(text))
		return true
	})
	require.NotEmpty(t, types)
	return types
}

func TestEveryJournalOperationTypeHasAFeedbackKind(t *testing.T) {
	kinds := make(map[OperationKind]bool)
	for _, kind := range AllOperationKinds() {
		kinds[kind] = true
	}
	used := make(map[OperationKind]bool)
	for _, operationType := range domainOperationTypes(t) {
		kind := operationKind(operationType, true)
		assert.NotEmpty(t, kind, "operation type %q has no feedback kind", operationType)
		assert.True(t, kinds[kind], "kind %q is not listed by AllOperationKinds", kind)
		used[kind] = true
	}
	used[operationKind(domain.OperationTransactionHide, false)] = true
	for kind := range kinds {
		assert.True(t, used[kind], "kind %q is listed but no operation type maps to it", kind)
	}
}

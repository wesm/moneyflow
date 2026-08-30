package ynab

import "testing"

func FuzzDecodePlan(f *testing.F) {
	f.Add([]byte(`{"data":{"plan":{"id":"plan-a"},"server_knowledge":1}}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{`))
	f.Fuzz(func(_ *testing.T, contents []byte) {
		var response planResponse
		_ = decodeJSON(contents, &response)
	})
}

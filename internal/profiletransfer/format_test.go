package profiletransfer

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/store"
)

func TestFormatRoundTrip(t *testing.T) {
	document := testDocument(t)
	for i, minor := range []int64{math.MinInt64, math.MaxInt64, -12345} {
		document.State.Snapshot.Committed.Transactions[i].Amount = domain.Money{Minor: minor, Currency: "USD", Scale: uint8(i)}
	}
	document.State.Snapshot.Committed.Transactions[0].Notes = strings.Repeat("é", 40000)
	var output bytes.Buffer
	require.NoError(t, Encode(&output, document))
	require.Contains(t, output.String(), `"format":"moneyflow-profile"`)
	require.Contains(t, output.String(), `"amount_minor":-9223372036854775808`)
	require.Contains(t, output.String(), `"collision_key"`)
	require.NotContains(t, output.String(), `"CollisionKey"`)
	decoded, err := Decode(bytes.NewReader(output.Bytes()))
	require.NoError(t, err)
	require.Equal(t, document.Header, decoded.Header)
	require.ElementsMatch(t, document.State.Snapshot.Committed.Transactions, decoded.State.Snapshot.Committed.Transactions)
	require.ElementsMatch(t, document.State.Snapshot.Committed.Categories, decoded.State.Snapshot.Committed.Categories)
	var repeated bytes.Buffer
	require.NoError(t, Encode(&repeated, decoded))
	require.Equal(t, output.String(), repeated.String())
}

func TestFormatRejectsMalformedFiles(t *testing.T) {
	var output bytes.Buffer
	require.NoError(t, Encode(&output, testDocument(t)))
	valid := output.String()
	lines := strings.Split(strings.TrimSuffix(valid, "\n"), "\n")
	cases := map[string]string{
		"unknown field":    strings.Replace(valid, `"source_name":`, `"secret-value":true,"source_name":`, 1),
		"unknown type":     strings.Replace(valid, `"type":"account"`, `"type":"secret-value"`, 1),
		"version":          strings.Replace(valid, `"format_version":1`, `"format_version":999`, 1),
		"duplicate key":    strings.Replace(valid, `"format_version":1`, `"format_version":1,"format_version":1`, 1),
		"duplicate record": strings.Join(append(append([]string{}, lines[:2]...), lines[1:]...), "\n"),
		"truncated":        strings.Join(lines[:len(lines)-1], "\n"),
		"trailing":         valid + lines[1] + "\n",
		"invalid UTF8":     strings.Replace(valid, `"source_name":`, "\xff\"source_name\":", 1),
		"bad date":         strings.Replace(valid, `"date":"`, `"date":"secret-value`, 1),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Decode(strings.NewReader(content))
			require.Error(t, err)
			require.Contains(t, err.Error(), "line")
			require.NotContains(t, err.Error(), "secret-value")
		})
	}
}

func TestFormatLimits(t *testing.T) {
	document := testDocument(t)
	var output bytes.Buffer
	require.NoError(t, Encode(&output, document))
	for _, limit := range []limits{
		{bytes: 40, line: MaxRecordBytes, records: MaxRecords},
		{bytes: MaxFileBytes, line: 40, records: MaxRecords},
		{bytes: MaxFileBytes, line: MaxRecordBytes, records: 1},
	} {
		_, err := decode(bytes.NewReader(output.Bytes()), limit)
		require.Error(t, err)
		require.Error(t, encode(&bytes.Buffer{}, document, limit))
	}
	document.State.Snapshot.Committed.Transactions[0].Notes = strings.Repeat("x", MaxRecordBytes)
	require.Error(t, Encode(&bytes.Buffer{}, document))
}

func TestFormatPreservesProviderFacts(t *testing.T) {
	document := testDocument(t)
	document.Header.ProviderKind = "ynab"
	state := &document.State
	state.Provider.Binding = &store.ProviderBinding{Kind: "ynab", Namespace: "ynab", RemoteProfileID: "example-plan", Currency: "USD", Scale: 2, BoundAt: time.Unix(100, 0).UTC()}
	txn := &state.Snapshot.Committed.Transactions[0]
	txn.Amount = domain.Money{Minor: -123, Currency: "USD", Scale: 2}
	state.YNABSplits = []store.YNABTransactionSplit{{ParentTransactionID: txn.ID, ExternalID: "split-example", AmountMinor: -123, AmountMilliunits: -1230, Memo: "Example memo", PayeeExternalID: "payee-example", CategoryLabel: "Example category", TransferAccountExternalID: "account-example"}}
	state.Provider.WriteRestrictions = []store.ProviderWriteRestriction{{Kind: domain.EntityKindTransaction, EntityID: txn.ID, Reason: "transfer"}}
	state.Provider.Lineage = []store.ProviderIdentityLineage{{Kind: domain.EntityKindMerchant, Namespace: "ynab/merchant", ExternalID: "old", PriorLocalID: "merchant-old", CurrentLocalID: txn.MerchantID, ProviderLabel: "Old Merchant", Disposition: "alias", BatchVersion: 42}}
	state.Provider.Allocations = []store.LabelAllocation{{Kind: domain.EntityKindMerchant, Namespace: "ynab/merchant", ExternalID: "old", BaseCollisionKey: "old merchant", DisplayLabel: "Old Merchant", ProviderLabel: "Old Merchant", Unsuffixed: true}}
	state.Snapshot.KnownDrills = []domain.DrillIdentity{{Dimension: domain.DimensionMerchant, Currency: "USD", Scale: 2, Key: "merchant-empty"}}
	state.Snapshot.Committed.ExternalIdentities = append(state.Snapshot.Committed.ExternalIdentities, domain.ExternalIdentity{EntityType: domain.EntityKindTransaction, EntityID: "deleted", Namespace: "ynab/transaction", ExternalID: "deleted"})
	var output bytes.Buffer
	require.NoError(t, Encode(&output, document))
	decoded, err := Decode(bytes.NewReader(output.Bytes()))
	require.NoError(t, err)
	require.Equal(t, state.Provider.Binding, decoded.State.Provider.Binding)
	require.Equal(t, state.Provider.Lineage, decoded.State.Provider.Lineage)
	require.Equal(t, state.Provider.Allocations, decoded.State.Provider.Allocations)
	require.Equal(t, state.Provider.WriteRestrictions, decoded.State.Provider.WriteRestrictions)
	require.Equal(t, state.YNABSplits, decoded.State.YNABSplits)
	require.Equal(t, state.Snapshot.KnownDrills, decoded.State.Snapshot.KnownDrills)
	require.ElementsMatch(t, state.Snapshot.Committed.ExternalIdentities, decoded.State.Snapshot.Committed.ExternalIdentities)
	require.Contains(t, output.String(), `"batch_version":42`)
	require.Contains(t, output.String(), `"identity_key":"merchant-empty"`)
	_, err = Decode(strings.NewReader(strings.Replace(output.String(), `"amount_milliunits":-1230`, `"amount_milliunits":-1231`, 1)))
	require.Error(t, err)
	document.State.YNABSplits[0].AmountMilliunits = -1231
	require.Error(t, Encode(&bytes.Buffer{}, document))
}

func TestFormatPreservesAmazonMoneyAndOverlays(t *testing.T) {
	document := testDocument(t)
	document.Header.ProviderKind = "amazon"
	txn := document.State.Snapshot.Committed.Transactions[0]
	unit := int64(155)
	document.State.AmazonSettings = &store.AmazonSettings{Currency: "USD", Scale: 2, CreatedAt: time.Unix(100, 0).UTC()}
	document.State.AmazonItems = []store.AmazonOrderItem{{LocalTransactionID: txn.ID, SourceIdentity: "example-source", OrderID: "example-order", ASIN: "EXAMPLE", ProductName: "Example Product", OrderDate: txn.Date, Quantity: 2, AmountMinor: -310, UnitPriceMinor: &unit, Currency: "USD", Scale: 2, OrderStatus: "Closed", ShipmentStatus: "Delivered", IdentityFingerprint: strings.Repeat("a", 64), FullFingerprint: strings.Repeat("b", 64), Retired: true, LocalAccountID: txn.AccountID, LocalMerchantID: txn.MerchantID, LocalCategoryID: txn.CategoryID, LocalNotes: "Saved overlay", LocalHidden: true}}
	var output bytes.Buffer
	require.NoError(t, Encode(&output, document))
	decoded, err := Decode(bytes.NewReader(output.Bytes()))
	require.NoError(t, err)
	require.Equal(t, document.State.AmazonSettings, decoded.State.AmazonSettings)
	require.Equal(t, document.State.AmazonItems, decoded.State.AmazonItems)
	require.Contains(t, output.String(), `"unit_price":"1.55"`)
	_, err = Decode(strings.NewReader(strings.Replace(output.String(), `"unit_price":"1.55"`, `"unit_price":"1.56"`, 1)))
	require.Error(t, err)
}

func TestFormatRejectsInconsistentSplitTotals(t *testing.T) {
	document := testDocument(t)
	document.Header.ProviderKind = "ynab"
	document.State.Provider.Binding = &store.ProviderBinding{Kind: "ynab", Namespace: "ynab", RemoteProfileID: "example-plan", Currency: "USD", Scale: 3, BoundAt: time.Unix(100, 0).UTC()}
	parent := &document.State.Snapshot.Committed.Transactions[0]
	parent.Amount = domain.Money{Minor: -123, Currency: "USD", Scale: 3}
	document.State.YNABSplits = []store.YNABTransactionSplit{
		{ParentTransactionID: parent.ID, ExternalID: "split-a", AmountMinor: -100, AmountMilliunits: -100},
		{ParentTransactionID: parent.ID, Position: 1, ExternalID: "split-b", AmountMinor: -23, AmountMilliunits: -23},
	}
	var valid bytes.Buffer
	require.NoError(t, Encode(&valid, document))
	_, err := Decode(bytes.NewReader(valid.Bytes()))
	require.NoError(t, err)
	for _, test := range []struct {
		name   string
		amount int64
	}{
		{"mismatch", -101},
		{"overflow", math.MinInt64},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Change both representations together so only the parent total is invalid.
			document.State.YNABSplits[0].AmountMinor = test.amount
			document.State.YNABSplits[0].AmountMilliunits = test.amount
			split := ynabSplit{ynabSplitFields(document.State.YNABSplits[0]), (domain.Money{Minor: test.amount, Currency: "USD", Scale: 3}).DecimalString(), "USD", 3}
			data, err := json.Marshal(split)
			require.NoError(t, err)
			replacement, err := json.Marshal(envelope{Type: "ynab_split", Data: data})
			require.NoError(t, err)
			lines := strings.Split(valid.String(), "\n")
			for i, line := range lines {
				if strings.Contains(line, `"external_id":"split-a"`) {
					lines[i] = string(replacement)
				}
			}
			t.Run("decode", func(t *testing.T) {
				_, err := Decode(strings.NewReader(strings.Join(lines, "\n")))
				require.Error(t, err)
			})
			t.Run("encode", func(t *testing.T) {
				require.Error(t, Encode(&bytes.Buffer{}, document))
			})
		})
	}
}

func TestFormatCompletesEmptyProfileAndRejectsWrongCounts(t *testing.T) {
	header, err := json.Marshal(testDocument(t).Header)
	require.NoError(t, err)
	end, err := json.Marshal(endRecord{counts()})
	require.NoError(t, err)
	content := "{\"type\":\"header\",\"data\":" + string(header) + "}\n{\"type\":\"end\",\"data\":" + string(end) + "}\n"
	document, err := Decode(strings.NewReader(content))
	require.NoError(t, err)
	require.Equal(t, 3, document.AddedSentinels)
	require.NoError(t, document.State.Snapshot.Validate())
	_, err = Decode(strings.NewReader(strings.Replace(content, `"account":0`, `"account":1`, 1)))
	require.ErrorContains(t, err, "counts")
}

func TestFormatRejectsInvalidMoneyAndReferences(t *testing.T) {
	document := testDocument(t)
	document.State.Snapshot.Committed.Transactions[0].Amount = domain.Money{Minor: -98765, Currency: "USD", Scale: 3}
	document.State.Snapshot.Committed.Transactions[0].Notes = "Example Unicode note"
	var output bytes.Buffer
	require.NoError(t, Encode(&output, document))
	for _, content := range []string{
		strings.Replace(output.String(), `"amount_minor":-98765`, `"amount_minor":-98764`, 1),
		strings.Replace(output.String(), `"account_id":"`, `"account_id":"missing-`, 1),
	} {
		_, err := Decode(strings.NewReader(content))
		require.Error(t, err)
	}
}

func testDocument(t *testing.T) Document {
	t.Helper()
	transactions := fixture.Generate(42, 32)
	committed, err := fixture.CommittedProfile(transactions)
	require.NoError(t, err)
	var document Document
	document.Header = Header{Format: "moneyflow-profile", FormatVersion: 1, SourceBuild: "test", ProviderKind: "local", ExportedAt: time.Unix(100, 0).UTC(), SourceName: "Example Profile"}
	document.State.Snapshot.Committed = committed
	return document
}

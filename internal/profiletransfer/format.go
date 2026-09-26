// Package profiletransfer owns the bounded, versioned JSONL profile format.
package profiletransfer

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

// Fixed bounds apply to complete files, individual lines, and all data records.
const (
	MaxFileBytes   int64 = 512 << 20
	MaxRecordBytes       = 1 << 20
	MaxRecords           = 1_000_000
)

// Header identifies the transfer format, not a database schema version.
type Header struct {
	Format         string    `json:"format"`
	FormatVersion  int       `json:"format_version"`
	SourceBuild    string    `json:"source_build"`
	ProviderKind   string    `json:"provider_kind"`
	ExportedAt     time.Time `json:"exported_at"`
	SourceRevision uint64    `json:"source_revision"`
	SourceName     string    `json:"source_name"`
}

// Document contains only the saved state that may cross the transfer boundary.
type Document struct {
	Header         Header
	State          store.ProfileTransfer
	AddedSentinels int
}

type envelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type endRecord struct {
	Counts map[string]int `json:"counts"`
}
type limits struct {
	bytes         int64
	line, records int
}

var recordTypes = []string{"account", "merchant", "group", "category", "transaction", "external_identity", "provider_binding", "label_allocation", "provider_lineage", "write_restriction", "ynab_split", "amazon_settings", "amazon_item", "known_drill", "csv_settings", "csv_file", "csv_row", "csv_file_row"}

func counts() map[string]int {
	result := make(map[string]int, len(recordTypes))
	for _, kind := range recordTypes {
		// Existing version-one footers contain every original class, including zeros.
		// Optional CSV classes are counted only when their records actually occur.
		if !strings.HasPrefix(kind, "csv_") {
			result[kind] = 0
		}
	}
	return result
}

func (header Header) validate() error {
	_, offset := header.ExportedAt.Zone()
	if header.Format != "moneyflow-profile" || header.FormatVersion != 1 || header.SourceBuild == "" ||
		strings.TrimSpace(header.SourceName) == "" || header.ExportedAt.IsZero() || offset != 0 {
		return errors.New("invalid or unsupported header")
	}
	return nil
}

func readRecord(state *store.ProfileTransfer, splits *[]ynabSplit, record envelope) (string, error) {
	p := &state.Snapshot.Committed
	switch record.Type {
	case "csv_settings":
		var value csvSettings
		err := strictJSON(record.Data, &value)
		converted := store.CSVSettings(value)
		state.CSV.Settings = &converted
		return "singleton", err
	case "csv_file":
		var value csvFile
		err := strictJSON(record.Data, &value)
		state.CSV.Files = append(state.CSV.Files, store.CSVFile(value))
		return value.Key, err
	case "csv_row":
		var value csvRow
		if err := strictJSON(record.Data, &value); err != nil {
			return "", err
		}
		converted, err := value.record()
		state.CSV.Rows = append(state.CSV.Rows, converted)
		return value.SourceKey, err
	case "csv_file_row":
		var value csvFileRow
		err := strictJSON(record.Data, &value)
		state.CSV.Membership = append(state.CSV.Membership, store.CSVFileRow(value))
		return value.FileKey + "\x00" + value.SourceKey, err
	case "account":
		var value account
		err := strictJSON(record.Data, &value)
		p.Accounts = append(p.Accounts, domain.Account(value))
		return string(value.ID), err
	case "merchant":
		var value merchant
		err := strictJSON(record.Data, &value)
		p.Merchants = append(p.Merchants, domain.Merchant(value))
		return string(value.ID), err
	case "group":
		var value group
		err := strictJSON(record.Data, &value)
		p.Groups = append(p.Groups, domain.CategoryGroup(value))
		return string(value.ID), err
	case "category":
		var value category
		err := strictJSON(record.Data, &value)
		p.Categories = append(p.Categories, domain.Category(value))
		return string(value.ID), err
	case "transaction":
		var value transaction
		if err := strictJSON(record.Data, &value); err != nil {
			return "", err
		}
		converted, err := value.record()
		p.Transactions = append(p.Transactions, converted)
		return string(value.ID), err
	case "external_identity":
		var value externalIdentity
		err := strictJSON(record.Data, &value)
		p.ExternalIdentities = append(p.ExternalIdentities, domain.ExternalIdentity(value))
		return value.Namespace + "\x00" + value.ExternalID, err
	case "provider_binding":
		var value providerBinding
		if err := strictJSON(record.Data, &value); err != nil {
			return "", err
		}
		_, offset := value.BoundAt.Zone()
		if offset != 0 {
			return "", errors.New("timestamp must be UTC")
		}
		converted := store.ProviderBinding(value)
		state.Provider.Binding = &converted
		return "singleton", nil
	case "label_allocation":
		var value labelAllocation
		err := strictJSON(record.Data, &value)
		state.Provider.Allocations = append(state.Provider.Allocations, store.LabelAllocation(value))
		return value.Namespace + "\x00" + value.ExternalID, err
	case "provider_lineage":
		var value providerLineage
		err := strictJSON(record.Data, &value)
		state.Provider.Lineage = append(state.Provider.Lineage, store.ProviderIdentityLineage(value))
		return value.Namespace + "\x00" + value.ExternalID, err
	case "write_restriction":
		var value writeRestriction
		err := strictJSON(record.Data, &value)
		state.Provider.WriteRestrictions = append(state.Provider.WriteRestrictions, store.ProviderWriteRestriction(value))
		return string(value.Kind) + "\x00" + string(value.EntityID), err
	case "ynab_split":
		var value ynabSplit
		if err := strictJSON(record.Data, &value); err != nil {
			return "", err
		}
		converted, err := value.record()
		state.YNABSplits = append(state.YNABSplits, converted)
		*splits = append(*splits, value)
		return value.ExternalID, err
	case "amazon_settings":
		var value amazonSettings
		if err := strictJSON(record.Data, &value); err != nil {
			return "", err
		}
		_, offset := value.CreatedAt.Zone()
		if offset != 0 {
			return "", errors.New("timestamp must be UTC")
		}
		state.AmazonSettings = &store.AmazonSettings{Currency: value.Currency, Scale: value.Scale, CreatedAt: value.CreatedAt}
		return "singleton", nil
	case "amazon_item":
		var value amazonItem
		if err := strictJSON(record.Data, &value); err != nil {
			return "", err
		}
		converted, err := value.record()
		state.AmazonItems = append(state.AmazonItems, converted)
		return value.SourceIdentity, err
	case "known_drill":
		var value knownDrill
		if err := strictJSON(record.Data, &value); err != nil {
			return "", err
		}
		converted := domain.DrillIdentity(value)
		state.Snapshot.KnownDrills = append(state.Snapshot.KnownDrills, converted)
		return converted.CanonicalKey()
	default:
		return "", errors.New("unknown record type")
	}
}

func finishDocument(document *Document, splits []ynabSplit) error {
	p := &document.State.Snapshot.Committed
	if !slices.ContainsFunc(p.Groups, func(v domain.CategoryGroup) bool { return v.ID == domain.UncategorizedGroupID }) {
		p.Groups = append(p.Groups, domain.CategoryGroup{ID: domain.UncategorizedGroupID, Label: domain.UncategorizedLabel, CollisionKey: domain.UncategorizedCollisionKey, Protected: true})
		document.AddedSentinels++
	}
	for _, sentinel := range domain.ProtectedCategories() {
		if !slices.ContainsFunc(p.Categories, func(v domain.Category) bool { return v.ID == sentinel.ID }) {
			p.Categories = append(p.Categories, sentinel)
			document.AddedSentinels++
		}
	}
	slices.SortFunc(document.State.Snapshot.KnownDrills, func(a, b domain.DrillIdentity) int {
		left, _ := a.CanonicalKey()
		right, _ := b.CanonicalKey()
		return strings.Compare(left, right)
	})
	if len(splits) > 0 {
		binding := document.State.Provider.Binding
		if binding == nil {
			return errors.New("splits need provider binding")
		}
		parents := make(map[domain.EntityID]domain.Money, len(p.Transactions))
		for _, value := range p.Transactions {
			parents[value.ID] = value.Amount
		}
		positions := make(map[string]bool, len(splits))
		for _, split := range splits {
			parent, found := parents[split.ParentTransactionID]
			key := fmt.Sprintf("%s\x00%d", split.ParentTransactionID, split.Position)
			if !found || split.Currency != parent.Currency || split.Scale != parent.Scale ||
				split.Currency != binding.Currency || split.Scale != binding.Scale || split.Position < 0 || positions[key] {
				return errors.New("invalid split reference or money")
			}
			positions[key] = true
		}
	}
	return app.ValidateProfileTransfer(document.State, document.Header.ProviderKind)
}

func formatError(line int, kind, reason string) error {
	if kind != "header" && kind != "end" && !slices.Contains(recordTypes, kind) {
		kind = "record"
	}
	return fmt.Errorf("profile JSONL line %d (%s): %s", line, kind, reason)
}

// Encode writes deterministic JSONL and refuses any limit overflow.
func Encode(writer io.Writer, document Document) error {
	return encode(writer, document, limits{MaxFileBytes, MaxRecordBytes, MaxRecords})
}

func encode(writer io.Writer, document Document, limit limits) error {
	if err := document.Header.validate(); err != nil {
		return formatError(1, "header", "invalid or unsupported header")
	}
	if err := app.ValidateProfileTransfer(document.State, document.Header.ProviderKind); err != nil {
		return formatError(1, "header", "invalid profile state")
	}
	line, records := 0, 0
	var total int64
	observed := counts()
	write := func(kind string, value any) error {
		line++
		var moneyErr error
		switch record := value.(type) {
		case transaction:
			_, moneyErr = record.record()
		case ynabSplit:
			_, moneyErr = record.record()
		case amazonItem:
			_, moneyErr = record.record()
		case csvRow:
			_, moneyErr = record.record()
		}
		if moneyErr != nil {
			return formatError(line, kind, "invalid money")
		}
		if kind != "header" && kind != "end" {
			records++
			observed[kind]++
		}
		data, err := json.Marshal(value)
		if err != nil {
			return formatError(line, kind, "invalid record")
		}
		encoded, err := json.Marshal(envelope{Type: kind, Data: data})
		if err != nil {
			return formatError(line, kind, "invalid record")
		}
		total += int64(len(encoded) + 1)
		if len(encoded) > limit.line || total > limit.bytes || records > limit.records {
			return formatError(line, kind, "transfer limit exceeded")
		}
		encoded = append(encoded, '\n')
		n, err := writer.Write(encoded)
		if err != nil {
			return err
		}
		if n != len(encoded) {
			return io.ErrShortWrite
		}
		return nil
	}
	if err := write("header", document.Header); err != nil {
		return err
	}
	if err := writeData(document.State, write); err != nil {
		return err
	}
	return write("end", endRecord{observed})
}

func writeValues[T any](values []T, kind string, key func(T) string, wire func(T) any, write func(string, any) error) error {
	values = slices.Clone(values)
	slices.SortFunc(values, func(a, b T) int { return strings.Compare(key(a), key(b)) })
	for _, value := range values {
		if err := write(kind, wire(value)); err != nil {
			return err
		}
	}
	return nil
}

func writeData(state store.ProfileTransfer, write func(string, any) error) error {
	if err := writeCSV(state.CSV, write); err != nil {
		return err
	}
	p := state.Snapshot.Committed
	if err := writeValues(p.Accounts, "account", func(v domain.Account) string { return string(v.ID) }, func(v domain.Account) any { return account(v) }, write); err != nil {
		return err
	}
	if err := writeValues(p.Merchants, "merchant", func(v domain.Merchant) string { return string(v.ID) }, func(v domain.Merchant) any { return merchant(v) }, write); err != nil {
		return err
	}
	if err := writeValues(p.Groups, "group", func(v domain.CategoryGroup) string { return string(v.ID) }, func(v domain.CategoryGroup) any { return group(v) }, write); err != nil {
		return err
	}
	if err := writeValues(p.Categories, "category", func(v domain.Category) string { return string(v.ID) }, func(v domain.Category) any { return category(v) }, write); err != nil {
		return err
	}
	if err := writeValues(p.Transactions, "transaction", func(v domain.TransactionRecord) string { return string(v.ID) }, func(v domain.TransactionRecord) any { return transactionFrom(v) }, write); err != nil {
		return err
	}
	if err := writeValues(p.ExternalIdentities, "external_identity", func(v domain.ExternalIdentity) string { return v.Namespace + "\x00" + v.ExternalID }, func(v domain.ExternalIdentity) any { return externalIdentity(v) }, write); err != nil {
		return err
	}
	if state.Provider.Binding != nil {
		value := providerBinding(*state.Provider.Binding)
		value.BoundAt = value.BoundAt.UTC()
		if err := write("provider_binding", value); err != nil {
			return err
		}
	}
	if err := writeValues(state.Provider.Allocations, "label_allocation", func(v store.LabelAllocation) string { return v.Namespace + "\x00" + v.ExternalID }, func(v store.LabelAllocation) any { return labelAllocation(v) }, write); err != nil {
		return err
	}
	if err := writeValues(state.Provider.Lineage, "provider_lineage", func(v store.ProviderIdentityLineage) string { return v.Namespace + "\x00" + v.ExternalID }, func(v store.ProviderIdentityLineage) any { return providerLineage(v) }, write); err != nil {
		return err
	}
	if err := writeValues(state.Provider.WriteRestrictions, "write_restriction", func(v store.ProviderWriteRestriction) string { return string(v.Kind) + "\x00" + string(v.EntityID) }, func(v store.ProviderWriteRestriction) any { return writeRestriction(v) }, write); err != nil {
		return err
	}
	if len(state.YNABSplits) > 0 && state.Provider.Binding == nil {
		return errors.New("profile transfer: splits require provider binding")
	}
	if err := writeValues(state.YNABSplits, "ynab_split", func(v store.YNABTransactionSplit) string {
		return fmt.Sprintf("%s\x00%020d", v.ParentTransactionID, v.Position)
	}, func(v store.YNABTransactionSplit) any {
		binding := state.Provider.Binding
		return ynabSplit{ynabSplitFields(v), (domain.Money{Minor: v.AmountMinor, Currency: binding.Currency, Scale: binding.Scale}).DecimalString(), binding.Currency, binding.Scale}
	}, write); err != nil {
		return err
	}
	if state.AmazonSettings != nil {
		v := state.AmazonSettings
		if err := write("amazon_settings", amazonSettings{v.Currency, v.Scale, v.CreatedAt.UTC()}); err != nil {
			return err
		}
	}
	if err := writeValues(state.AmazonItems, "amazon_item", func(v store.AmazonOrderItem) string { return v.SourceIdentity }, func(v store.AmazonOrderItem) any { return amazonItemFrom(v) }, write); err != nil {
		return err
	}
	return writeValues(state.Snapshot.KnownDrills, "known_drill", func(v domain.DrillIdentity) string { key, _ := v.CanonicalKey(); return key }, func(v domain.DrillIdentity) any { return knownDrill(v) }, write)
}

// Decode reads a complete transfer before the caller reserves a destination profile.
func Decode(reader io.Reader) (Document, error) {
	return decode(reader, limits{MaxFileBytes, MaxRecordBytes, MaxRecords})
}

func decode(reader io.Reader, limit limits) (Document, error) {
	bounded := &io.LimitedReader{R: reader, N: limit.bytes + 1}
	scanner := bufio.NewScanner(bounded)
	scanner.Buffer(make([]byte, min(64<<10, limit.line+2)), limit.line+2)
	var document Document
	observed := counts()
	seen := make(map[string]struct{})
	line, records := 0, 0
	ended := false
	var splits []ynabSplit
	for scanner.Scan() {
		line++
		if ended {
			return Document{}, formatError(line, "end", "data follows end record")
		}
		if len(scanner.Bytes()) > limit.line || bounded.N <= 0 {
			return Document{}, formatError(line, "record", "transfer limit exceeded")
		}
		var record envelope
		if err := strictJSON(scanner.Bytes(), &record); err != nil {
			return Document{}, formatError(line, "record", "invalid JSON record")
		}
		if line == 1 {
			if record.Type != "header" || strictJSON(record.Data, &document.Header) != nil || document.Header.validate() != nil {
				return Document{}, formatError(line, "header", "invalid or unsupported header")
			}
			document.State.Snapshot.Revision = document.Header.SourceRevision
			continue
		}
		if record.Type == "end" {
			var end endRecord
			if strictJSON(record.Data, &end) != nil || !maps.Equal(end.Counts, observed) {
				return Document{}, formatError(line, "end", "record counts disagree")
			}
			ended = true
			continue
		}
		if !slices.Contains(recordTypes, record.Type) {
			return Document{}, formatError(line, "record", "unknown record type")
		}
		records++
		if records > limit.records {
			return Document{}, formatError(line, record.Type, "transfer limit exceeded")
		}
		key, err := readRecord(&document.State, &splits, record)
		if err != nil {
			return Document{}, formatError(line, record.Type, "invalid record")
		}
		key = record.Type + "\x00" + key
		if _, duplicate := seen[key]; duplicate {
			return Document{}, formatError(line, record.Type, "duplicate record")
		}
		seen[key] = struct{}{}
		observed[record.Type]++
	}
	if scanner.Err() != nil || bounded.N <= 0 {
		return Document{}, formatError(line+1, "record", "read failed or transfer limit exceeded")
	}
	if !ended {
		return Document{}, formatError(line+1, "end", "missing end record")
	}
	if err := finishDocument(&document, splits); err != nil {
		return Document{}, formatError(line, "end", "invalid profile references or money")
	}
	return document, nil
}

func strictJSON(data []byte, destination any) error {
	if !utf8.Valid(data) || len(data) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("invalid JSON")
	}
	keys := json.NewDecoder(bytes.NewReader(data))
	keys.UseNumber()
	if err := uniqueKeys(keys); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func uniqueKeys(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	switch token {
	case json.Delim('{'):
		seen := make(map[string]struct{})
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return errors.New("invalid key")
			}
			if _, exists := seen[name]; exists {
				return errors.New("duplicate key")
			}
			seen[name] = struct{}{}
			if err = uniqueKeys(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case json.Delim('['):
		for decoder.More() {
			if err = uniqueKeys(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	}
	return nil
}

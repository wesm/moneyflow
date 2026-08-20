package amazon

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wesm/moneyflow/internal/domain"
)

var requiredHeaders = []string{
	"Order ID", "Order Date", "Product Name", "Quantity", "Total Owed",
	"Order Status", "Shipment Status",
}

// Parse validates and normalizes a complete bounded Amazon order-history candidate.
func Parse(
	ctx context.Context,
	files []SourceFile,
	settings Settings,
	limits Limits,
	observe ObserveFunc,
) (Candidate, error) {
	if err := limits.validate(); err != nil {
		return Candidate{}, err
	}
	if len(files) == 0 {
		return Candidate{}, newError(CodeEmpty, ErrEmpty)
	}
	if len(files) > limits.Files || settings.Scale > 9 || !domain.IsValidCurrency(settings.Currency) {
		return Candidate{}, newError(CodeInvalid, ErrInvalid)
	}
	files = append([]SourceFile(nil), files...)
	slices.SortFunc(files, func(left, right SourceFile) int {
		return strings.Compare(left.RelativeName, right.RelativeName)
	})
	candidate := Candidate{FileCount: len(files)}
	observed := make(map[string]struct{})
	observedSources := make(map[string]map[string]struct{})
	var totalBytes int64
	for index, source := range files {
		if err := ctx.Err(); err != nil {
			return Candidate{}, err
		}
		if observe != nil {
			observe(Progress{Phase: "parsing", Completed: index, Total: len(files)})
		}
		if err := parseFile(
			ctx, source, settings, limits, &candidate, observed, observedSources, &totalBytes,
		); err != nil {
			return Candidate{}, err
		}
	}
	if err := deduplicateOverlappingOrders(&candidate, observedSources); err != nil {
		return Candidate{}, err
	}
	for orderID := range observed {
		candidate.ObservedOrderIDs = append(candidate.ObservedOrderIDs, orderID)
	}
	slices.Sort(candidate.ObservedOrderIDs)
	if len(candidate.Rows) == 0 && len(candidate.ObservedOrderIDs) == 0 {
		return Candidate{}, newError(CodeEmpty, ErrEmpty)
	}
	digestFields := []string{"amazon-candidate-v1"}
	for _, orderID := range candidate.ObservedOrderIDs {
		digestFields = append(digestFields, "order", orderID)
	}
	for _, row := range candidate.Rows {
		digestFields = append(digestFields, "row", row.IdentityFingerprint, row.FullFingerprint)
	}
	candidate.Digest = canonicalDigest(digestFields)
	if observe != nil {
		observe(Progress{Phase: "parsing", Completed: len(files), Total: len(files)})
	}
	return candidate, nil
}

func parseFile(
	ctx context.Context,
	source SourceFile,
	settings Settings,
	limits Limits,
	candidate *Candidate,
	observed map[string]struct{},
	observedSources map[string]map[string]struct{},
	totalBytes *int64,
) error {
	info, err := os.Lstat(source.Path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return newError(CodeInvalid, ErrInvalid)
	}
	if info.Size() > limits.BytesPerFile || *totalBytes > limits.TotalBytes-info.Size() {
		return newError(CodeTooLarge, ErrTooLarge)
	}
	*totalBytes += info.Size()
	file, err := os.Open(source.Path) // #nosec G304 -- coordinator/discovery supplies an inspected file.
	if err != nil {
		return newError(CodeInvalid, ErrInvalid)
	}
	defer func() { _ = file.Close() }()
	bounded := &csvBoundaryReader{
		reader:         &contextReader{ctx: ctx, reader: io.LimitReader(file, limits.BytesPerFile+1)},
		maxRecordBytes: limits.BytesPerRecord, maxColumns: limits.Columns, columns: 1,
		atFieldStart: true,
	}
	reader := csv.NewReader(bounded)
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = false
	header, err := reader.Read()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if errors.Is(err, errCSVBoundary) {
			return newError(CodeTooLarge, ErrTooLarge)
		}
		return newError(CodeInvalid, ErrInvalid)
	}
	if len(header) > 0 {
		header[0] = strings.TrimPrefix(header[0], "\ufeff")
	}
	indexes, err := validateHeaders(header, limits)
	if err != nil {
		return err
	}
	for record := 1; ; record++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		fields, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if errors.Is(readErr, errCSVBoundary) {
				return newError(CodeTooLarge, ErrTooLarge)
			}
			return coordinateError(source.RelativeName, record, "", "invalid_csv", ErrInvalid)
		}
		candidate.LogicalRecordCount++
		if candidate.LogicalRecordCount > limits.Records {
			return newError(CodeTooLarge, ErrTooLarge)
		}
		if len(fields) > limits.Columns || recordBytes(fields) > limits.BytesPerRecord {
			return newError(CodeTooLarge, ErrTooLarge)
		}
		for _, field := range fields {
			if int64(len(field)) > limits.BytesPerField || !utf8.ValidString(field) {
				return coordinateError(source.RelativeName, record, "", "invalid_field", ErrInvalid)
			}
		}
		if blankRecord(fields) {
			candidate.BlankRecordCount++
			continue
		}
		row, cancelled, parseErr := parseRow(source.RelativeName, record, fields, indexes, settings)
		if parseErr != nil {
			return parseErr
		}
		observed[row.OrderID] = struct{}{}
		if observedSources[row.OrderID] == nil {
			observedSources[row.OrderID] = make(map[string]struct{})
		}
		observedSources[row.OrderID][source.RelativeName] = struct{}{}
		if cancelled {
			candidate.CancelledRecordCount++
			continue
		}
		candidate.Rows = append(candidate.Rows, row)
	}
	return nil
}

func deduplicateOverlappingOrders(
	candidate *Candidate,
	observedSources map[string]map[string]struct{},
) error {
	keepSource := make(map[string]string)
	for orderID, sourceSet := range observedSources {
		if len(sourceSet) < 2 {
			continue
		}
		sources := make([]string, 0, len(sourceSet))
		for source := range sourceSet {
			sources = append(sources, source)
		}
		slices.Sort(sources)
		reference := orderSourceFingerprints(candidate.Rows, orderID, sources[0])
		for _, source := range sources[1:] {
			if !slices.Equal(reference, orderSourceFingerprints(candidate.Rows, orderID, source)) {
				return overlappingOrderError(candidate.Rows, orderID, source)
			}
		}
		keepSource[orderID] = sources[0]
	}
	if len(keepSource) == 0 {
		return nil
	}
	rows := candidate.Rows[:0]
	for _, row := range candidate.Rows {
		if source, duplicate := keepSource[row.OrderID]; duplicate && row.RelativeFilename != source {
			continue
		}
		rows = append(rows, row)
	}
	candidate.Rows = rows
	return nil
}

func orderSourceFingerprints(rows []Row, orderID, source string) []string {
	fingerprints := make([]string, 0)
	for _, row := range rows {
		if row.OrderID == orderID && row.RelativeFilename == source {
			fingerprints = append(fingerprints, row.FullFingerprint)
		}
	}
	slices.Sort(fingerprints)
	return fingerprints
}

func overlappingOrderError(rows []Row, orderID, source string) error {
	record := 1
	for _, row := range rows {
		if row.OrderID == orderID && row.RelativeFilename == source {
			record = row.Record
			break
		}
	}
	return coordinateError(source, record, "", "overlapping_order_conflict", ErrInvalid)
}

func validateHeaders(headers []string, limits Limits) (map[string]int, error) {
	if len(headers) == 0 || len(headers) > limits.Columns {
		return nil, newError(CodeInvalid, ErrInvalid)
	}
	indexes := make(map[string]int, len(headers))
	for index, header := range headers {
		if !utf8.ValidString(header) {
			return nil, newError(CodeInvalid, ErrInvalid)
		}
		if _, duplicate := indexes[header]; duplicate {
			return nil, newError(CodeInvalid, ErrInvalid)
		}
		indexes[header] = index
	}
	for _, required := range requiredHeaders {
		if _, ok := indexes[required]; !ok {
			return nil, newError(CodeInvalid, ErrInvalid)
		}
	}
	return indexes, nil
}

func parseRow(
	filename string,
	record int,
	fields []string,
	indexes map[string]int,
	settings Settings,
) (Row, bool, error) {
	value := func(name string) string {
		index, ok := indexes[name]
		if !ok || index >= len(fields) {
			return ""
		}
		return fields[index]
	}
	orderID, err := domain.NormalizeDisplayLabel(value("Order ID"))
	if err != nil {
		return Row{}, false, coordinateError(filename, record, "Order ID", "invalid_order_id", ErrInvalid)
	}
	status, err := domain.NormalizeDisplayLabel(value("Order Status"))
	if err != nil {
		return Row{}, false, coordinateError(filename, record, "Order Status", "invalid_status", ErrInvalid)
	}
	if status == "Cancelled" {
		return Row{OrderID: orderID, OrderStatus: status}, true, nil
	}
	date, err := parseOrderDate(value("Order Date"))
	if err != nil {
		return Row{}, false, coordinateError(filename, record, "Order Date", "invalid_date", ErrInvalid)
	}
	product, err := domain.NormalizeDisplayLabel(value("Product Name"))
	if err != nil {
		return Row{}, false, coordinateError(filename, record, "Product Name", "invalid_product", ErrInvalid)
	}
	quantity, err := parseQuantity(value("Quantity"))
	if err != nil {
		return Row{}, false, coordinateError(filename, record, "Quantity", "invalid_quantity", ErrInvalid)
	}
	amount, err := parseAmazonMoney(value("Total Owed"), settings.Currency, settings.Scale)
	if err != nil || amount.Minor == math.MinInt64 {
		return Row{}, false, coordinateError(filename, record, "Total Owed", "invalid_money", ErrInvalid)
	}
	amount.Minor = -amount.Minor
	shipment, err := domain.NormalizeDisplayLabel(value("Shipment Status"))
	if err != nil {
		return Row{}, false, coordinateError(filename, record, "Shipment Status", "invalid_status", ErrInvalid)
	}
	currency := value("Currency")
	if currency == "" {
		currency = string(settings.Currency)
	}
	if currency != string(settings.Currency) {
		return Row{}, false, coordinateError(filename, record, "Currency", "currency_mismatch", ErrInvalid)
	}
	asin := strings.TrimSpace(value("ASIN"))
	asinless := ""
	if asin == "" || asin == "_ASINLESS_" {
		asin = ""
		asinless, err = ASINLessKey(product)
		if err != nil {
			return Row{}, false, coordinateError(filename, record, "ASIN", "invalid_asin", ErrInvalid)
		}
	}
	var unitPrice *int64
	if raw := value("Unit Price"); raw != "" {
		unit, unitErr := parseAmazonMoney(raw, settings.Currency, settings.Scale)
		if unitErr != nil {
			return Row{}, false, coordinateError(filename, record, "Unit Price", "invalid_money", ErrInvalid)
		}
		unitPrice = &unit.Minor
	}
	row := Row{
		OrderID: orderID, ProductName: product, ASIN: asin, ASINLessKey: asinless,
		OrderDate: date, Quantity: quantity, AmountMinor: amount.Minor,
		UnitPriceMinor: unitPrice, Currency: settings.Currency, Scale: settings.Scale,
		OrderStatus: status, ShipmentStatus: shipment,
		RelativeFilename: filename, Record: record,
	}
	fingerprints, err := Fingerprints(row)
	if err != nil {
		return Row{}, false, coordinateError(filename, record, "", "invalid_identity", ErrInvalid)
	}
	row.IdentityFingerprint = fingerprints.Identity
	row.FullFingerprint = fingerprints.Full
	return row, false, nil
}

func parseOrderDate(value string) (domain.Date, error) {
	if parsed, err := domain.ParseDate(value); err == nil {
		return parsed, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return domain.Date{}, err
	}
	parsed = parsed.UTC()
	return domain.NewDate(parsed.Year(), parsed.Month(), parsed.Day())
}

func parseQuantity(value string) (int64, error) {
	if value == "" {
		return 1, nil
	}
	quantity, err := strconv.ParseInt(value, 10, 64)
	if err != nil || quantity < 1 {
		return 0, ErrInvalid
	}
	return quantity, nil
}

func parseAmazonMoney(value string, currency domain.Currency, scale uint8) (domain.Money, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return domain.Money{}, ErrInvalid
	}
	sign := ""
	digits := value
	if strings.HasPrefix(digits, "+") || strings.HasPrefix(digits, "-") {
		sign, digits = digits[:1], digits[1:]
	}
	parts := strings.Split(digits, ".")
	if len(parts) > 2 || !validGroupedInteger(parts[0]) {
		return domain.Money{}, ErrInvalid
	}
	if len(parts) == 2 && strings.Contains(parts[1], ",") {
		return domain.Money{}, ErrInvalid
	}
	canonical := sign + strings.ReplaceAll(parts[0], ",", "")
	if len(parts) == 2 {
		canonical += "." + parts[1]
	}
	return domain.ParseMoney(canonical, currency, scale)
}

func validGroupedInteger(value string) bool {
	if value == "" {
		return false
	}
	groups := strings.Split(value, ",")
	if len(groups) > 1 && (len(groups[0]) < 1 || len(groups[0]) > 3) {
		return false
	}
	for index, group := range groups {
		if index > 0 && len(group) != 3 {
			return false
		}
		for _, digit := range group {
			if digit < '0' || digit > '9' {
				return false
			}
		}
	}
	return true
}

func recordBytes(fields []string) int64 {
	var size int64
	for _, field := range fields {
		size += int64(len(field))
	}
	return size
}

func blankRecord(fields []string) bool {
	for _, field := range fields {
		if field != "" {
			return false
		}
	}
	return true
}

func (candidate Candidate) String() string {
	return fmt.Sprintf("Amazon candidate %s", candidate.Digest)
}

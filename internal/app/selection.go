package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/wesm/moneyflow/internal/domain"
)

const (
	selectionPrefix    = "mfsel1."
	emptySelectionJSON = `{"v":1,"kind":"transaction","base":"explicit"}`

	// MaxSelectionIdentities bounds all explicit and delta identities in one token.
	MaxSelectionIdentities = 8_192
	// MaxSelectionIdentityBytes bounds one decoded stable identity.
	MaxSelectionIdentityBytes = 512
	// MaxSelectionDocumentBytes bounds the decoded canonical JSON document.
	MaxSelectionDocumentBytes = 1 << 20
	// MaxEncodedSelectionBytes bounds the complete opaque wire value.
	MaxEncodedSelectionBytes = 14 * (1 << 20) / 10
)

var emptySelectionValue = SelectionValue(
	selectionPrefix + base64.RawURLEncoding.EncodeToString([]byte(emptySelectionJSON)),
)

// SelectionValue is an opaque browser-held exact selection document.
type SelectionValue string

// IdentityKind identifies the stable row identity represented by a selection.
type IdentityKind string

const (
	// IdentityTransaction identifies normalized transaction IDs.
	IdentityTransaction IdentityKind = "transaction"
	// IdentityAggregate identifies composite aggregate partition IDs.
	IdentityAggregate IdentityKind = "aggregate"
)

// SelectionErrorCode is a stable machine-readable selection failure code.
type SelectionErrorCode string

const (
	// SelectionInvalid identifies malformed values and result-kind mismatches.
	SelectionInvalid SelectionErrorCode = "invalid_selection"
	// SelectionTooLarge identifies an exact transition that cannot fit the wire contract.
	SelectionTooLarge SelectionErrorCode = "selection_too_large"
	// SelectionReset warns that invalid hydration state was discarded.
	SelectionReset SelectionErrorCode = "selection_reset"
)

// SelectionError separates stable public failure information from its cause.
type SelectionError struct {
	Code   SelectionErrorCode
	Detail string
	cause  error
}

// Error returns only the safe public detail.
func (selectionErr *SelectionError) Error() string {
	return selectionErr.Detail
}

// Unwrap returns the diagnostic cause.
func (selectionErr *SelectionError) Unwrap() error {
	return selectionErr.cause
}

// SelectionSnapshot is one resolved concrete selection set.
type SelectionSnapshot struct {
	Kind IdentityKind
	IDs  map[string]struct{}
}

type selectionBaseKind string

const (
	selectionBaseExplicit selectionBaseKind = "explicit"
	selectionBaseAll      selectionBaseKind = "all"
)

type selectionDocument struct {
	Version uint8             `json:"v"`
	Kind    IdentityKind      `json:"kind"`
	Base    selectionBaseKind `json:"base"`
	IDs     []string          `json:"ids,omitempty"`
	State   *AnalyticalState  `json:"state,omitempty"`
	Include []string          `json:"include,omitempty"`
	Exclude []string          `json:"exclude,omitempty"`
}

type encodedSelectionCandidate struct {
	value     SelectionValue
	canonical []byte
}

// EmptySelection returns the canonical kind-neutral empty value.
func EmptySelection() SelectionValue {
	return emptySelectionValue
}

func decodeSelection(value SelectionValue) (selectionDocument, error) {
	if len(value) > MaxEncodedSelectionBytes {
		return selectionDocument{}, invalidSelection(errors.New("encoded selection exceeds limit"))
	}
	encoded := strings.TrimPrefix(string(value), selectionPrefix)
	if encoded == string(value) || encoded == "" || strings.Contains(encoded, "=") {
		return selectionDocument{}, invalidSelection(errors.New("selection prefix or padding is invalid"))
	}
	if base64.RawURLEncoding.DecodedLen(len(encoded)) > MaxSelectionDocumentBytes {
		return selectionDocument{}, invalidSelection(errors.New("decoded selection exceeds limit"))
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return selectionDocument{}, invalidSelection(fmt.Errorf("decode selection: %w", err))
	}
	if len(data) > MaxSelectionDocumentBytes {
		return selectionDocument{}, invalidSelection(errors.New("decoded selection exceeds limit"))
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document selectionDocument
	if err := decoder.Decode(&document); err != nil {
		return selectionDocument{}, invalidSelection(fmt.Errorf("decode selection document: %w", err))
	}
	if err := requireJSONEOF(decoder); err != nil {
		return selectionDocument{}, invalidSelection(err)
	}
	if err := document.validate(); err != nil {
		return selectionDocument{}, invalidSelection(err)
	}
	if err := document.validateBounds(); err != nil {
		return selectionDocument{}, invalidSelection(err)
	}
	canonical, err := marshalSelectionDocument(document)
	if err != nil {
		return selectionDocument{}, invalidSelection(err)
	}
	if !bytes.Equal(data, canonical) {
		return selectionDocument{}, invalidSelection(errors.New("selection is not canonical"))
	}
	return document, nil
}

func encodeSelection(document selectionDocument) (SelectionValue, error) {
	if err := document.validate(); err != nil {
		return "", invalidSelection(err)
	}
	if err := document.validateBounds(); err != nil {
		return "", tooLargeSelection(err)
	}
	data, err := marshalSelectionDocument(document)
	if err != nil {
		return "", invalidSelection(err)
	}
	if len(data) > MaxSelectionDocumentBytes {
		return "", tooLargeSelection(errors.New("decoded selection exceeds limit"))
	}
	value := SelectionValue(selectionPrefix + base64.RawURLEncoding.EncodeToString(data))
	if len(value) > MaxEncodedSelectionBytes {
		return "", tooLargeSelection(errors.New("encoded selection exceeds limit"))
	}
	return value, nil
}

func marshalSelectionDocument(document selectionDocument) ([]byte, error) {
	data, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("marshal selection document: %w", err)
	}
	return data, nil
}

func (document selectionDocument) validate() error {
	if document.Version != 1 {
		return errors.New("unsupported selection version")
	}
	if !document.Kind.valid() {
		return errors.New("invalid selection identity kind")
	}
	if err := validateCanonicalIdentities(document.IDs); err != nil {
		return fmt.Errorf("invalid explicit identities: %w", err)
	}
	if err := validateCanonicalIdentities(document.Include); err != nil {
		return fmt.Errorf("invalid inclusion identities: %w", err)
	}
	if err := validateCanonicalIdentities(document.Exclude); err != nil {
		return fmt.Errorf("invalid exclusion identities: %w", err)
	}
	if intersectsSorted(document.Include, document.Exclude) {
		return errors.New("selection inclusion and exclusion identities overlap")
	}

	switch document.Base {
	case selectionBaseExplicit:
		if document.State != nil {
			return errors.New("explicit selection contains a defining state")
		}
		if intersectsSorted(document.IDs, document.Include) {
			return errors.New("explicit selection base and inclusion identities overlap")
		}
	case selectionBaseAll:
		if document.State == nil {
			return errors.New("all selection has no defining state")
		}
		if len(document.IDs) != 0 {
			return errors.New("all selection contains explicit base identities")
		}
		if err := validateSelectionState(*document.State); err != nil {
			return err
		}
		if identityKindForState(*document.State) != document.Kind {
			return errors.New("all selection kind does not match its defining state")
		}
	default:
		return errors.New("invalid selection base kind")
	}
	return nil
}

func (document selectionDocument) validateBounds() error {
	if len(document.IDs)+len(document.Include)+len(document.Exclude) > MaxSelectionIdentities {
		return errors.New("selection contains too many identities")
	}
	for _, identities := range [][]string{document.IDs, document.Include, document.Exclude} {
		for _, identity := range identities {
			if len(identity) > MaxSelectionIdentityBytes {
				return errors.New("selection identity exceeds limit")
			}
		}
	}
	return nil
}

func validateCanonicalIdentities(identities []string) error {
	for index, identity := range identities {
		if identity == "" || !utf8.ValidString(identity) {
			return errors.New("identity is empty or invalid UTF-8")
		}
		if index > 0 && identities[index-1] >= identity {
			return errors.New("identities are not sorted and unique")
		}
	}
	return nil
}

func validateSelectionState(state AnalyticalState) error {
	if err := (ViewState{Version: ViewStateSchemaVersion, Current: state}).Validate(); err != nil {
		return fmt.Errorf("invalid selection defining state: %w", err)
	}
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("selection document contains trailing JSON")
	}
	return fmt.Errorf("decode trailing selection JSON: %w", err)
}

func (kind IdentityKind) valid() bool {
	return kind == IdentityTransaction || kind == IdentityAggregate
}

func identityKindForState(state AnalyticalState) IdentityKind {
	if state.SubGrouping != nil || state.Mode == domain.ResultModeAggregate {
		return IdentityAggregate
	}
	return IdentityTransaction
}

func invalidSelection(cause error) *SelectionError {
	return &SelectionError{
		Code: SelectionInvalid, Detail: "The selection value is invalid.", cause: cause,
	}
}

func tooLargeSelection(cause error) *SelectionError {
	return &SelectionError{
		Code:   SelectionTooLarge,
		Detail: "The requested selection is too large to preserve exactly.",
		cause:  cause,
	}
}

// ResolveSelection resolves an opaque value to its complete concrete stable-identity set.
func (service *Service) ResolveSelection(
	state AnalyticalState,
	value SelectionValue,
) (SelectionSnapshot, error) {
	if err := validateSelectionState(state); err != nil {
		return SelectionSnapshot{}, invalidSelection(err)
	}
	document, err := decodeSelection(value)
	if err != nil {
		return SelectionSnapshot{}, err
	}
	currentKind := identityKindForState(state)
	if document.Kind != currentKind && !document.isUniversalEmpty() {
		return SelectionSnapshot{}, invalidSelection(errors.New("selection kind does not match result"))
	}
	ids, err := service.resolveSelectionBase(document)
	if err != nil {
		return SelectionSnapshot{}, err
	}
	for _, identity := range document.Exclude {
		delete(ids, identity)
	}
	for _, identity := range document.Include {
		ids[identity] = struct{}{}
	}
	return SelectionSnapshot{Kind: currentKind, IDs: ids}, nil
}

// ToggleSelection toggles one stable identity and returns the smallest exact value.
func (service *Service) ToggleSelection(
	state AnalyticalState,
	value SelectionValue,
	kind IdentityKind,
	identity string,
) (SelectionValue, error) {
	if !kind.valid() || kind != identityKindForState(state) {
		return value, invalidSelection(errors.New("selection target kind does not match result"))
	}
	if identity == "" || !utf8.ValidString(identity) {
		return value, invalidSelection(errors.New("selection target identity is invalid"))
	}
	if len(identity) > MaxSelectionIdentityBytes {
		return value, tooLargeSelection(errors.New("selection target identity exceeds limit"))
	}
	snapshot, err := service.ResolveSelection(state, value)
	if err != nil {
		return value, err
	}
	target := cloneSet(snapshot.IDs)
	toggleSetValue(target, identity)
	return service.smallestSelection(state, value, kind, target)
}

// ToggleAllSelection selects or clears the complete current result.
func (service *Service) ToggleAllSelection(
	state AnalyticalState,
	value SelectionValue,
) (SelectionValue, error) {
	snapshot, err := service.ResolveSelection(state, value)
	if err != nil {
		return value, err
	}
	currentKind, currentIDs, err := service.identitiesForState(state)
	if err != nil {
		return value, err
	}
	if currentKind != snapshot.Kind {
		return value, invalidSelection(errors.New("selection kind does not match current result"))
	}
	target := cloneSet(snapshot.IDs)
	visible := sortedIdentitySet(currentIDs)
	toggleAll(target, visible)
	if equalIdentitySets(target, snapshot.IDs) {
		return value, nil
	}
	return service.smallestSelection(state, value, currentKind, target)
}

func (service *Service) smallestSelection(
	state AnalyticalState,
	oldValue SelectionValue,
	kind IdentityKind,
	target map[string]struct{},
) (SelectionValue, error) {
	candidates := []selectionDocument{{
		Version: 1,
		Kind:    kind,
		Base:    selectionBaseExplicit,
		IDs:     sortedIdentitySet(target),
	}}

	_, currentBase, err := service.identitiesForState(state)
	if err != nil {
		return oldValue, err
	}
	currentState := state.Clone()
	candidates = append(candidates, selectionDocumentForBase(
		kind,
		selectionBaseAll,
		nil,
		&currentState,
		currentBase,
		target,
	))

	oldDocument, err := decodeSelection(oldValue)
	if err != nil {
		return oldValue, err
	}
	if oldDocument.Kind == kind && !oldDocument.isUniversalEmpty() {
		oldBase, resolveErr := service.resolveSelectionBase(oldDocument)
		if resolveErr != nil {
			return oldValue, resolveErr
		}
		candidates = append(candidates, selectionDocumentForBase(
			kind,
			oldDocument.Base,
			oldDocument.IDs,
			oldDocument.State,
			oldBase,
			target,
		))
	}

	values := make([]encodedSelectionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		encoded, encodeErr := encodeSelection(candidate)
		if encodeErr == nil {
			canonical, marshalErr := marshalSelectionDocument(candidate)
			if marshalErr != nil {
				return oldValue, invalidSelection(marshalErr)
			}
			values = append(values, encodedSelectionCandidate{
				value: encoded, canonical: canonical,
			})
			continue
		}
		var selectionErr *SelectionError
		if !errors.As(encodeErr, &selectionErr) || selectionErr.Code != SelectionTooLarge {
			return oldValue, encodeErr
		}
	}
	if len(values) == 0 {
		return oldValue, tooLargeSelection(errors.New("no exact selection representation fits"))
	}
	return chooseSmallestSelection(values), nil
}

func chooseSmallestSelection(candidates []encodedSelectionCandidate) SelectionValue {
	sort.Slice(candidates, func(left, right int) bool {
		if len(candidates[left].value) != len(candidates[right].value) {
			return len(candidates[left].value) < len(candidates[right].value)
		}
		return bytes.Compare(candidates[left].canonical, candidates[right].canonical) < 0
	})
	return candidates[0].value
}

func selectionDocumentForBase(
	kind IdentityKind,
	base selectionBaseKind,
	baseIDs []string,
	state *AnalyticalState,
	resolvedBase map[string]struct{},
	target map[string]struct{},
) selectionDocument {
	document := selectionDocument{
		Version: 1,
		Kind:    kind,
		Base:    base,
		IDs:     append([]string(nil), baseIDs...),
	}
	if state != nil {
		cloned := state.Clone()
		document.State = &cloned
	}
	for identity := range target {
		if _, exists := resolvedBase[identity]; !exists {
			document.Include = append(document.Include, identity)
		}
	}
	for identity := range resolvedBase {
		if _, exists := target[identity]; !exists {
			document.Exclude = append(document.Exclude, identity)
		}
	}
	sort.Strings(document.Include)
	sort.Strings(document.Exclude)
	return document
}

func (service *Service) resolveSelectionBase(
	document selectionDocument,
) (map[string]struct{}, error) {
	if document.Base == selectionBaseExplicit {
		return identitySliceSet(document.IDs), nil
	}
	kind, identities, err := service.identitiesForState(*document.State)
	if err != nil {
		return nil, err
	}
	if kind != document.Kind {
		return nil, invalidSelection(errors.New("selection base kind does not match result"))
	}
	return identities, nil
}

func (service *Service) identitiesForState(
	state AnalyticalState,
) (IdentityKind, map[string]struct{}, error) {
	if err := validateSelectionState(state); err != nil {
		return "", nil, invalidSelection(err)
	}
	session := sessionFromAnalyticalState(state)
	for index := range session.Drilldowns {
		if session.Drilldowns[index].Dimension != domain.DimensionTime {
			session.Drilldowns[index].Label = "resolved"
		}
	}
	result, err := service.Query(session)
	if err != nil {
		return "", nil, invalidSelection(fmt.Errorf("resolve selection result: %w", err))
	}
	kind := identityKindForState(state)
	identities := make(map[string]struct{}, result.FilteredCount)
	if kind == IdentityTransaction {
		for _, row := range result.DetailRows {
			identities[row.Transaction.ID] = struct{}{}
		}
		return kind, identities, nil
	}
	for _, row := range result.AggregateRows {
		identities[AggregateIdentity(row)] = struct{}{}
	}
	return kind, identities, nil
}

func (document selectionDocument) isUniversalEmpty() bool {
	return document.Base == selectionBaseExplicit && document.State == nil &&
		len(document.IDs) == 0 && len(document.Include) == 0 && len(document.Exclude) == 0
}

func identitySliceSet(identities []string) map[string]struct{} {
	result := make(map[string]struct{}, len(identities))
	for _, identity := range identities {
		result[identity] = struct{}{}
	}
	return result
}

func sortedIdentitySet(identities map[string]struct{}) []string {
	result := make([]string, 0, len(identities))
	for identity := range identities {
		result = append(result, identity)
	}
	sort.Strings(result)
	return result
}

func intersectsSorted(left []string, right []string) bool {
	leftIndex := 0
	rightIndex := 0
	for leftIndex < len(left) && rightIndex < len(right) {
		switch {
		case left[leftIndex] == right[rightIndex]:
			return true
		case left[leftIndex] < right[rightIndex]:
			leftIndex++
		default:
			rightIndex++
		}
	}
	return false
}

func equalIdentitySets(left map[string]struct{}, right map[string]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for identity := range left {
		if _, exists := right[identity]; !exists {
			return false
		}
	}
	return true
}

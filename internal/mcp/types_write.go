package mcp

// UpdateTransactionCategoryInput stages or previews one exact category assignment.
type UpdateTransactionCategoryInput struct {
	ExpectedRevision string `json:"expected_revision"`
	TransactionID    string `json:"transaction_id"`
	CategoryID       string `json:"category_id,omitempty"`
	CategoryLabel    string `json:"category_label,omitempty"`
	DryRun           bool   `json:"dry_run,omitempty"`
}

// BatchUpdateCategoryInput stages or previews one atomic bounded category assignment.
type BatchUpdateCategoryInput struct {
	ExpectedRevision string   `json:"expected_revision"`
	TransactionIDs   []string `json:"transaction_ids"`
	CategoryID       string   `json:"category_id,omitempty"`
	CategoryLabel    string   `json:"category_label,omitempty"`
	DryRun           bool     `json:"dry_run,omitempty"`
}

// CursorMutationInput moves the durable journal cursor at one exact revision.
type CursorMutationInput struct {
	ExpectedRevision string `json:"expected_revision"`
}

// CommitChangesInput confirms one previously reviewed journal revision.
type CommitChangesInput struct {
	ExpectedRevision string `json:"expected_revision"`
	ReviewedRevision string `json:"reviewed_revision"`
}

// BatchVersionInput guards one provider-write batch control action.
type BatchVersionInput struct {
	BatchVersion string `json:"batch_version"`
}

// StopReconcileInput starts one revision- and batch-version-checked reconciliation.
type StopReconcileInput struct {
	ExpectedRevision string `json:"expected_revision"`
	BatchVersion     string `json:"batch_version"`
}

// ReconcileStatusInput selects one process-local reconciliation attempt.
type ReconcileStatusInput struct {
	AttemptID string `json:"attempt_id,omitempty"`
}

// ConfirmReconcileInput confirms one process-local reconciliation candidate.
type ConfirmReconcileInput struct {
	AttemptID         string `json:"attempt_id"`
	ExpectedRevision  string `json:"expected_revision"`
	BatchVersion      string `json:"batch_version"`
	ConfirmationToken string `json:"confirmation_token"`
}

// MutationChangeDocument reports one exact transaction before and after an edit.
type MutationChangeDocument struct {
	TransactionID string              `json:"transaction_id"`
	Before        TransactionDocument `json:"before"`
	After         TransactionDocument `json:"after"`
}

// MutationDocument is the bounded structured result of one edit or cursor mutation.
type MutationDocument struct {
	Header
	DryRun               bool                     `json:"dry_run"`
	AffectedCount        int                      `json:"affected_count"`
	Window               CollectionWindow         `json:"window"`
	Changes              []MutationChangeDocument `json:"changes"`
	Pending              PendingDocument          `json:"pending"`
	SelectionDisposition string                   `json:"selection_disposition"`
}

// CommitDocument distinguishes local completion from an accepted background provider write.
type CommitDocument struct {
	Header
	Completed        bool                `json:"completed"`
	BackgroundActive bool                `json:"background_active"`
	Write            WriteStatusDocument `json:"write"`
}

// BatchControlDocument is one authoritative counts-only write-control result.
type BatchControlDocument struct {
	Header
	BackgroundActive bool                `json:"background_active"`
	Write            WriteStatusDocument `json:"write"`
}

// AttemptDocument is one credential-blind process-local reconciliation snapshot.
type AttemptDocument struct {
	Header
	AttemptID         string              `json:"attempt_id"`
	State             string              `json:"state"`
	Code              string              `json:"code,omitempty"`
	Generation        string              `json:"generation"`
	StartedAt         string              `json:"started_at"`
	FinishedAt        string              `json:"finished_at,omitempty"`
	Write             WriteStatusDocument `json:"write"`
	ConfirmationToken string              `json:"confirmation_token,omitempty"`
}

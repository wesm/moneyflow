CREATE TABLE schema_metadata (
    singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
    schema_version INTEGER NOT NULL CHECK(typeof(schema_version) = 'integer' AND schema_version >= 0)
) STRICT;

INSERT INTO schema_metadata(singleton, schema_version) VALUES (1, 5);

CREATE TABLE profile_state (
    singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
    revision INTEGER NOT NULL CHECK(typeof(revision) = 'integer' AND revision >= 0),
    journal_cursor INTEGER NOT NULL CHECK(typeof(journal_cursor) = 'integer' AND journal_cursor >= 0)
) STRICT;

INSERT INTO profile_state(singleton, revision, journal_cursor) VALUES (1, 0, 0);

CREATE TABLE accounts (
    id TEXT PRIMARY KEY CHECK(id <> ''),
    label TEXT NOT NULL CHECK(label <> ''),
    collision_key TEXT NOT NULL CHECK(collision_key <> ''),
    retired INTEGER NOT NULL CHECK(retired IN (0, 1))
) STRICT;

CREATE UNIQUE INDEX accounts_active_collision_key
ON accounts(collision_key) WHERE retired = 0;

CREATE TABLE merchants (
    id TEXT PRIMARY KEY CHECK(id <> ''),
    label TEXT NOT NULL CHECK(label <> ''),
    collision_key TEXT NOT NULL CHECK(collision_key <> ''),
    retired INTEGER NOT NULL CHECK(retired IN (0, 1)),
    protected INTEGER NOT NULL CHECK(protected IN (0, 1)),
    merge_destination_id TEXT REFERENCES merchants(id),
    CHECK(merge_destination_id IS NULL OR (retired = 1 AND merge_destination_id <> id)),
    CHECK(protected = 0 OR (retired = 0 AND merge_destination_id IS NULL))
) STRICT;

CREATE UNIQUE INDEX merchants_active_collision_key
ON merchants(collision_key) WHERE retired = 0;

CREATE TABLE category_groups (
    id TEXT PRIMARY KEY CHECK(id <> ''),
    label TEXT NOT NULL CHECK(label <> ''),
    collision_key TEXT NOT NULL CHECK(collision_key <> ''),
    retired INTEGER NOT NULL CHECK(retired IN (0, 1)),
    protected INTEGER NOT NULL CHECK(protected IN (0, 1)),
    merge_destination_id TEXT REFERENCES category_groups(id),
    CHECK(merge_destination_id IS NULL OR (retired = 1 AND merge_destination_id <> id)),
    CHECK(protected = 0 OR (retired = 0 AND merge_destination_id IS NULL))
) STRICT;

CREATE UNIQUE INDEX category_groups_active_collision_key
ON category_groups(collision_key) WHERE retired = 0;

INSERT INTO category_groups(
    id, label, collision_key, retired, protected, merge_destination_id
) VALUES (
    'group_system_uncategorized', 'Uncategorized', 'uncategorized', 0, 1, NULL
);

CREATE TABLE categories (
    id TEXT PRIMARY KEY CHECK(id <> ''),
    group_id TEXT NOT NULL REFERENCES category_groups(id),
    label TEXT NOT NULL CHECK(label <> ''),
    collision_key TEXT NOT NULL CHECK(collision_key <> ''),
    retired INTEGER NOT NULL CHECK(retired IN (0, 1)),
    protected INTEGER NOT NULL CHECK(protected IN (0, 1)),
    merge_destination_id TEXT REFERENCES categories(id),
    CHECK(merge_destination_id IS NULL OR (retired = 1 AND merge_destination_id <> id)),
    CHECK(protected = 0 OR (retired = 0 AND merge_destination_id IS NULL))
) STRICT;

CREATE UNIQUE INDEX categories_active_collision_key
ON categories(collision_key) WHERE retired = 0;

INSERT INTO categories(
    id, group_id, label, collision_key, retired, protected, merge_destination_id
) VALUES (
    'category_system_uncategorized', 'group_system_uncategorized',
    'Uncategorized', 'uncategorized', 0, 1, NULL
);

CREATE TABLE transactions (
    id TEXT PRIMARY KEY CHECK(id <> ''),
    provider TEXT NOT NULL CHECK(provider <> ''),
    provider_id TEXT NOT NULL CHECK(provider_id <> ''),
    account_id TEXT NOT NULL REFERENCES accounts(id),
    merchant_id TEXT NOT NULL REFERENCES merchants(id),
    category_id TEXT NOT NULL REFERENCES categories(id),
    transaction_date TEXT NOT NULL CHECK(
        length(transaction_date) = 10 AND
        transaction_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'
    ),
    amount_minor INTEGER NOT NULL CHECK(typeof(amount_minor) = 'integer'),
    currency TEXT NOT NULL CHECK(length(currency) = 3 AND currency GLOB '[A-Z][A-Z][A-Z]'),
    scale INTEGER NOT NULL CHECK(typeof(scale) = 'integer' AND scale BETWEEN 0 AND 9),
    notes TEXT NOT NULL,
    hidden INTEGER NOT NULL CHECK(hidden IN (0, 1)),
    pending INTEGER NOT NULL CHECK(pending IN (0, 1)),
    metadata_json TEXT NOT NULL CHECK(json_valid(metadata_json))
) STRICT;

CREATE UNIQUE INDEX transactions_provider_identity
ON transactions(provider, provider_id);

CREATE INDEX transactions_account ON transactions(account_id);
CREATE INDEX transactions_merchant ON transactions(merchant_id);
CREATE INDEX transactions_category ON transactions(category_id);
CREATE INDEX transactions_date ON transactions(transaction_date);

CREATE TABLE external_identities (
    entity_type TEXT NOT NULL CHECK(entity_type IN ('account', 'merchant', 'group', 'category', 'transaction')),
    entity_id TEXT NOT NULL CHECK(entity_id <> ''),
    namespace TEXT NOT NULL CHECK(namespace <> ''),
    external_id TEXT NOT NULL CHECK(external_id <> ''),
    PRIMARY KEY(namespace, external_id),
    UNIQUE(entity_type, entity_id, namespace)
) STRICT;

CREATE TABLE known_drills (
    dimension TEXT NOT NULL CHECK(dimension IN ('account', 'merchant', 'group', 'category')),
    currency TEXT NOT NULL CHECK(length(currency) = 3 AND currency GLOB '[A-Z][A-Z][A-Z]'),
    scale INTEGER NOT NULL CHECK(typeof(scale) = 'integer' AND scale BETWEEN 0 AND 9),
    identity_key TEXT NOT NULL CHECK(identity_key <> ''),
    PRIMARY KEY(dimension, currency, scale, identity_key)
) STRICT;

CREATE TABLE journal_operations (
    id TEXT PRIMARY KEY CHECK(id <> ''),
    profile_singleton INTEGER NOT NULL DEFAULT 1 REFERENCES profile_state(singleton)
        CHECK(profile_singleton = 1),
    sequence INTEGER NOT NULL UNIQUE CHECK(typeof(sequence) = 'integer' AND sequence > 0),
    operation_type TEXT NOT NULL CHECK(operation_type IN (
        'merchant.label', 'merchant.merge', 'merchant.reassign',
        'category.assign', 'category.create', 'category.label', 'category.move',
        'category.merge', 'category.delete', 'group.create', 'group.label',
        'group.merge', 'group.delete', 'transaction.hide-toggle'
    )),
    payload_version INTEGER NOT NULL CHECK(typeof(payload_version) = 'integer' AND payload_version > 0),
    creation_revision INTEGER NOT NULL CHECK(typeof(creation_revision) = 'integer' AND creation_revision >= 0),
    created_at_unix_ms INTEGER NOT NULL CHECK(typeof(created_at_unix_ms) = 'integer' AND created_at_unix_ms >= 0)
) STRICT;

CREATE TABLE operation_payloads (
    operation_id TEXT PRIMARY KEY REFERENCES journal_operations(id) ON DELETE CASCADE,
    payload_version INTEGER NOT NULL CHECK(typeof(payload_version) = 'integer' AND payload_version > 0),
    payload_json TEXT NOT NULL CHECK(json_valid(payload_json))
) STRICT;

CREATE TABLE operation_targets (
    operation_id TEXT NOT NULL REFERENCES journal_operations(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK(typeof(ordinal) = 'integer' AND ordinal >= 0),
    entity_id TEXT NOT NULL CHECK(entity_id <> ''),
    PRIMARY KEY(operation_id, ordinal),
    UNIQUE(operation_id, entity_id)
) STRICT;

CREATE TABLE provider_binding (
    singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
    kind TEXT NOT NULL CHECK(kind <> ''),
    namespace TEXT NOT NULL CHECK(namespace <> ''),
    remote_profile_id TEXT NOT NULL CHECK(remote_profile_id <> ''),
    currency TEXT NOT NULL CHECK(length(currency) = 3 AND currency GLOB '[A-Z][A-Z][A-Z]'),
    scale INTEGER NOT NULL CHECK(typeof(scale) = 'integer' AND scale BETWEEN 0 AND 9),
    bound_at_unix_ms INTEGER NOT NULL CHECK(
        typeof(bound_at_unix_ms) = 'integer' AND bound_at_unix_ms >= 0
    )
) STRICT;

CREATE TABLE provider_refresh_state (
    singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
    generation INTEGER NOT NULL CHECK(typeof(generation) = 'integer' AND generation >= 0),
    last_attempt_unix_ms INTEGER CHECK(
        last_attempt_unix_ms IS NULL OR
        (typeof(last_attempt_unix_ms) = 'integer' AND last_attempt_unix_ms >= 0)
    ),
    last_success_unix_ms INTEGER CHECK(
        last_success_unix_ms IS NULL OR
        (typeof(last_success_unix_ms) = 'integer' AND last_success_unix_ms >= 0)
    ),
    next_eligible_unix_ms INTEGER CHECK(
        next_eligible_unix_ms IS NULL OR
        (typeof(next_eligible_unix_ms) = 'integer' AND next_eligible_unix_ms >= 0)
    ),
    status_code TEXT NOT NULL CHECK(status_code IN (
        '', 'provider_reconnect_required', 'provider_identity_mismatch',
        'provider_snapshot_unstable', 'provider_refresh_in_progress',
        'provider_deletion_confirmation_required', 'provider_confirmation_invalid',
        'provider_refresh_stale', 'provider_rate_limited', 'provider_unavailable',
        'provider_data_invalid'
    )),
    imported_transactions INTEGER NOT NULL CHECK(
        typeof(imported_transactions) = 'integer' AND imported_transactions >= 0
    ),
    removed_transactions INTEGER NOT NULL CHECK(
        typeof(removed_transactions) = 'integer' AND removed_transactions >= 0
    )
) STRICT;

INSERT INTO provider_refresh_state(
    singleton, generation, status_code, imported_transactions, removed_transactions
) VALUES (1, 0, '', 0, 0);

CREATE TABLE provider_operation_lease (
    singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
    owner_id TEXT NOT NULL CHECK(owner_id <> ''),
    renderer TEXT NOT NULL CHECK(renderer IN ('cli', 'tui', 'web')),
    operation_kind TEXT NOT NULL CHECK(operation_kind IN ('refresh', 'write', 'reconcile')),
    expires_at_unix_ms INTEGER NOT NULL CHECK(
        typeof(expires_at_unix_ms) = 'integer' AND expires_at_unix_ms >= 0
    )
) STRICT;

CREATE TABLE provider_label_allocations (
    entity_type TEXT NOT NULL CHECK(entity_type IN ('account', 'merchant', 'group', 'category')),
    namespace TEXT NOT NULL CHECK(namespace <> ''),
    external_id TEXT NOT NULL CHECK(external_id <> ''),
    base_collision_key TEXT NOT NULL CHECK(base_collision_key <> ''),
    display_label TEXT NOT NULL CHECK(display_label <> ''),
    provider_label TEXT NOT NULL CHECK(provider_label <> ''),
    suffix_token TEXT NOT NULL,
    unsuffixed INTEGER NOT NULL CHECK(unsuffixed IN (0, 1)),
    CHECK((unsuffixed = 1 AND suffix_token = '') OR (unsuffixed = 0 AND suffix_token <> '')),
    PRIMARY KEY(namespace, external_id)
) STRICT;

CREATE UNIQUE INDEX provider_label_allocations_unsuffixed_owner
ON provider_label_allocations(entity_type, base_collision_key)
WHERE unsuffixed = 1;

CREATE TABLE provider_identity_lineage (
    namespace TEXT NOT NULL CHECK(namespace <> ''),
    external_id TEXT NOT NULL CHECK(external_id <> ''),
    entity_type TEXT NOT NULL CHECK(entity_type IN ('account', 'merchant', 'group', 'category')),
    prior_local_id TEXT NOT NULL CHECK(prior_local_id <> ''),
    current_local_id TEXT NOT NULL CHECK(current_local_id <> ''),
    provider_label TEXT NOT NULL CHECK(provider_label <> ''),
    disposition TEXT NOT NULL CHECK(disposition IN ('alias', 'reactivated')),
    batch_version INTEGER NOT NULL CHECK(typeof(batch_version) = 'integer' AND batch_version > 0),
    PRIMARY KEY(namespace, external_id)
) STRICT;

CREATE TABLE provider_write_batches (
    profile_singleton INTEGER PRIMARY KEY DEFAULT 1 REFERENCES profile_state(singleton)
        CHECK(profile_singleton = 1),
    batch_id TEXT NOT NULL UNIQUE CHECK(batch_id <> ''),
    phase TEXT NOT NULL CHECK(phase IN (
        'writing', 'reconciling', 'paused', 'reconnect_required',
        'rate_limited', 'attention_required'
    )),
    version INTEGER NOT NULL CHECK(typeof(version) = 'integer' AND version > 0),
    reviewed_revision INTEGER NOT NULL CHECK(
        typeof(reviewed_revision) = 'integer' AND reviewed_revision >= 0
    ),
    prepared_revision INTEGER NOT NULL CHECK(
        typeof(prepared_revision) = 'integer' AND prepared_revision >= 0
    ),
    refresh_generation INTEGER NOT NULL CHECK(
        typeof(refresh_generation) = 'integer' AND refresh_generation >= 0
    ),
    frozen_cursor INTEGER NOT NULL CHECK(typeof(frozen_cursor) = 'integer' AND frozen_cursor > 0),
    frozen_prefix_digest TEXT NOT NULL CHECK(frozen_prefix_digest <> ''),
    frozen_operation_count INTEGER NOT NULL CHECK(
        typeof(frozen_operation_count) = 'integer' AND frozen_operation_count > 0
    ),
    total_items INTEGER NOT NULL CHECK(typeof(total_items) = 'integer' AND total_items > 0),
    completed_items INTEGER NOT NULL CHECK(
        typeof(completed_items) = 'integer' AND completed_items BETWEEN 0 AND total_items
    ),
    failed_items INTEGER NOT NULL CHECK(
        typeof(failed_items) = 'integer' AND failed_items BETWEEN 0 AND total_items
    ),
    override_count INTEGER NOT NULL CHECK(typeof(override_count) = 'integer' AND override_count >= 0),
    attention_class TEXT CHECK(
        attention_class IS NULL OR attention_class IN ('retryable', 'reconcile_only')
    ),
    attention_reason TEXT CHECK(
        attention_reason IS NULL OR attention_reason IN (
            'provider_write_unavailable_exhausted', 'provider_write_response_incomplete',
            'provider_write_target_not_found', 'provider_write_rejected',
            'provider_write_identity_conflict', 'provider_write_retired_identity',
            'provider_write_expectation_invalid'
        )
    ),
    prepared_at_unix_ms INTEGER NOT NULL CHECK(
        typeof(prepared_at_unix_ms) = 'integer' AND prepared_at_unix_ms >= 0
    ),
    updated_at_unix_ms INTEGER NOT NULL CHECK(
        typeof(updated_at_unix_ms) = 'integer' AND updated_at_unix_ms >= prepared_at_unix_ms
    ),
    next_eligible_unix_ms INTEGER CHECK(
        next_eligible_unix_ms IS NULL OR
        (typeof(next_eligible_unix_ms) = 'integer' AND next_eligible_unix_ms >= updated_at_unix_ms)
    ),
    CHECK((attention_class IS NULL) = (attention_reason IS NULL)),
    CHECK(phase = 'attention_required' OR attention_class IS NULL)
) STRICT;

CREATE TABLE provider_write_batch_operations (
    batch_id TEXT NOT NULL REFERENCES provider_write_batches(batch_id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK(typeof(ordinal) = 'integer' AND ordinal >= 0),
    operation_id TEXT NOT NULL REFERENCES journal_operations(id),
    PRIMARY KEY(batch_id, ordinal),
    UNIQUE(batch_id, operation_id)
) STRICT;

CREATE TABLE provider_write_items (
    item_id TEXT PRIMARY KEY CHECK(item_id <> ''),
    batch_id TEXT NOT NULL REFERENCES provider_write_batches(batch_id) ON DELETE CASCADE,
    position INTEGER NOT NULL CHECK(typeof(position) = 'integer' AND position >= 0),
    transaction_id TEXT NOT NULL CHECK(transaction_id <> ''),
    transaction_external_id TEXT NOT NULL CHECK(transaction_external_id <> ''),
    requested_merchant_local_id TEXT,
    requested_merchant_name TEXT,
    requested_category_external_id TEXT,
    requested_hidden INTEGER CHECK(requested_hidden IS NULL OR requested_hidden IN (0, 1)),
    originating_operation_ids_json TEXT NOT NULL CHECK(json_valid(originating_operation_ids_json)),
    expectation_kind TEXT CHECK(
        expectation_kind IS NULL OR expectation_kind IN ('existing', 'merge_destination', 'new')
    ),
    expected_merchant_external_id TEXT,
    new_group_key TEXT,
    group_leader INTEGER NOT NULL CHECK(group_leader IN (0, 1)),
    item_state TEXT NOT NULL CHECK(item_state IN ('pending', 'succeeded', 'failed')),
    attempt_count INTEGER NOT NULL CHECK(typeof(attempt_count) = 'integer' AND attempt_count >= 0),
    CHECK(requested_merchant_name IS NULL OR requested_merchant_name <> ''),
    CHECK(requested_category_external_id IS NULL OR requested_category_external_id <> ''),
    CHECK((expectation_kind IS NULL) = (requested_merchant_name IS NULL)),
    CHECK(expectation_kind <> 'new' OR (new_group_key IS NOT NULL AND new_group_key <> '')),
    CHECK(expectation_kind = 'new' OR new_group_key IS NULL),
    CHECK(expectation_kind NOT IN ('existing', 'merge_destination') OR
        (expected_merchant_external_id IS NOT NULL AND expected_merchant_external_id <> '')),
    CHECK(expectation_kind IN ('existing', 'merge_destination') OR expected_merchant_external_id IS NULL),
    CHECK(requested_merchant_name IS NOT NULL OR requested_category_external_id IS NOT NULL OR
        requested_hidden IS NOT NULL),
    UNIQUE(batch_id, position),
    UNIQUE(batch_id, transaction_id)
) STRICT;

CREATE INDEX provider_write_items_batch_state_position
ON provider_write_items(batch_id, item_state, position);

CREATE TABLE provider_write_results (
    item_id TEXT PRIMARY KEY REFERENCES provider_write_items(item_id) ON DELETE CASCADE,
    transaction_external_id TEXT NOT NULL CHECK(transaction_external_id <> ''),
    merchant_external_id TEXT,
    merchant_label TEXT,
    category_external_id TEXT,
    hidden INTEGER CHECK(hidden IS NULL OR hidden IN (0, 1)),
    override_count INTEGER NOT NULL CHECK(typeof(override_count) = 'integer' AND override_count >= 0),
    recorded_at_unix_ms INTEGER NOT NULL CHECK(
        typeof(recorded_at_unix_ms) = 'integer' AND recorded_at_unix_ms >= 0
    ),
    CHECK(merchant_external_id IS NULL OR merchant_external_id <> ''),
    CHECK(merchant_label IS NULL OR merchant_label <> ''),
    CHECK(category_external_id IS NULL OR category_external_id <> '')
) STRICT;

CREATE TABLE provider_last_write_summary (
    singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
    completed_at_unix_ms INTEGER NOT NULL CHECK(
        typeof(completed_at_unix_ms) = 'integer' AND completed_at_unix_ms >= 0
    ),
    committed_revision INTEGER NOT NULL CHECK(
        typeof(committed_revision) = 'integer' AND committed_revision >= 0
    ),
    operation_count INTEGER NOT NULL CHECK(typeof(operation_count) = 'integer' AND operation_count >= 0),
    item_count INTEGER NOT NULL CHECK(typeof(item_count) = 'integer' AND item_count >= 0),
    override_count INTEGER NOT NULL CHECK(typeof(override_count) = 'integer' AND override_count >= 0)
) STRICT;

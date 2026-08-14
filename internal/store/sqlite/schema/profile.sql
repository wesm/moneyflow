CREATE TABLE schema_metadata (
    singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
    schema_version INTEGER NOT NULL CHECK(typeof(schema_version) = 'integer' AND schema_version >= 0)
) STRICT;

INSERT INTO schema_metadata(singleton, schema_version) VALUES (1, 2);

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

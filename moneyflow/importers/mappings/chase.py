"""Chase credit card CSV mapping."""

from moneyflow.importers.engine import InstitutionMapping

chase_credit_mapping = InstitutionMapping(
    name="chase_credit",
    display_name="Chase Credit Card",
    file_pattern="Chase*.csv",
    id_prefix="chase_",
    date_fmt="%m/%d/%Y",
    column_map={
        "Transaction Date": "date",
        "Post Date": "post_date",
        "Description": "merchant",
        "Category": "category",
        "Type": "type",
        "Amount": "amount",
        "Memo": "notes",
    },
    amount_sign=1,  # Chase credit: expenses already negative
    skip_rows=0,
    dedup_fields=("date", "amount", "merchant"),
    extra_columns=("post_date", "type"),
    date_columns=("date",),
    id_fields=("date", "amount", "merchant", "notes"),
    currency="USD",
    default_category="Uncategorized",
    default_category_id="cat_uncategorized",
    encoding="utf-8",
    debit_column=None,
    credit_column=None,
)

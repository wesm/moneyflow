"""Registry of institution mappings."""
from moneyflow.importers.engine import InstitutionMapping

from .chase import chase_credit_mapping

INSTITUTION_MAPPINGS: dict[str, InstitutionMapping] = {
    "chase_credit": chase_credit_mapping,
}

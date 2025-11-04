"""
Generate realistic synthetic transaction data for demo mode.

Creates multiple years of transactions for a millennial couple in a major US city
with ~$250k gross income, realistic spending patterns, and edge cases for testing features.
Defaults to 3 years of data (2023-2025) to enable testing of multi-year TIME views.
"""

import hashlib
import random
from typing import Any, Dict, List

from moneyflow.categories import DEFAULT_CATEGORY_GROUPS


class DemoDataGenerator:
    """Generate realistic synthetic financial data."""

    def __init__(self, start_year: int = 2023, years: int = 3, seed: int = 42):
        """
        Initialize data generator.

        Args:
            start_year: First year to generate data for
            years: Number of years of data to generate
            seed: Random seed for reproducible data
        """
        self.start_year = start_year
        self.years = years
        random.seed(seed)
        self.transaction_counter = 1000

    def generate_full_year(self) -> tuple[List[Dict], List[Dict], List[Dict]]:
        """
        Generate transactions for configured number of years, plus categories and category groups.

        Returns:
            Tuple of (transactions, categories, category_groups)
        """
        categories = self._create_categories()
        category_groups = self._create_category_groups()
        transactions = self._generate_transactions()

        return transactions, categories, category_groups

    def _create_category_groups(self) -> List[Dict]:
        """Create category groups using built-in structure."""
        return [
            {"id": "grp_food", "name": "Food & Dining", "type": "expense"},
            {"id": "grp_transport", "name": "Transportation", "type": "expense"},
            {"id": "grp_home", "name": "Home", "type": "expense"},
            {"id": "grp_shopping", "name": "Shopping", "type": "expense"},
            {"id": "grp_entertainment", "name": "Entertainment", "type": "expense"},
            {"id": "grp_health", "name": "Health & Fitness", "type": "expense"},
            {"id": "grp_bills", "name": "Bills & Utilities", "type": "expense"},
            {"id": "grp_income", "name": "Income", "type": "income"},
            {"id": "grp_transfers", "name": "Transfers", "type": "transfer"},
        ]

    def _create_categories(self) -> List[Dict]:
        """
        Create comprehensive category list including all categories from DEFAULT_CATEGORY_GROUPS.

        This provides a realistic set of categories for the demo experience,
        allowing users to explore category editing features.

        We start with hardcoded IDs for categories used in transaction generation
        (to keep tests passing), then add all additional categories from DEFAULT_CATEGORY_GROUPS.
        """
        # Start with categories used in transaction generation (must keep these IDs for tests)
        base_categories = [
            {
                "id": "cat_groceries",
                "name": "Groceries",
                "group": {"id": "grp_food", "type": "expense"},
            },
            {
                "id": "cat_restaurants",
                "name": "Restaurants & Bars",
                "group": {"id": "grp_food", "type": "expense"},
            },
            {
                "id": "cat_coffee",
                "name": "Coffee Shops",
                "group": {"id": "grp_food", "type": "expense"},
            },
            {"id": "cat_gas", "name": "Gas", "group": {"id": "grp_transport", "type": "expense"}},
            {
                "id": "cat_parking",
                "name": "Parking & Tolls",
                "group": {"id": "grp_transport", "type": "expense"},
            },
            {
                "id": "cat_uber",
                "name": "Taxi & Ride Shares",
                "group": {"id": "grp_transport", "type": "expense"},
            },
            {"id": "cat_rent", "name": "Rent", "group": {"id": "grp_home", "type": "expense"}},
            {
                "id": "cat_utilities",
                "name": "Gas & Electric",
                "group": {"id": "grp_home", "type": "expense"},
            },
            {
                "id": "cat_internet",
                "name": "Internet & Cable",
                "group": {"id": "grp_home", "type": "expense"},
            },
            {
                "id": "cat_shopping",
                "name": "Shopping",
                "group": {"id": "grp_shopping", "type": "expense"},
            },
            {
                "id": "cat_amazon",
                "name": "Amazon",
                "group": {"id": "grp_shopping", "type": "expense"},
            },
            {
                "id": "cat_streaming",
                "name": "Entertainment & Recreation",
                "group": {"id": "grp_entertainment", "type": "expense"},
            },
            {"id": "cat_gym", "name": "Fitness", "group": {"id": "grp_health", "type": "expense"}},
            {
                "id": "cat_medical",
                "name": "Medical",
                "group": {"id": "grp_health", "type": "expense"},
            },
            {"id": "cat_phone", "name": "Phone", "group": {"id": "grp_bills", "type": "expense"}},
            {
                "id": "cat_insurance",
                "name": "Insurance",
                "group": {"id": "grp_bills", "type": "expense"},
            },
            {
                "id": "cat_paycheck",
                "name": "Paychecks",
                "group": {"id": "grp_income", "type": "income"},
            },
            {
                "id": "cat_transfer",
                "name": "Transfer",
                "group": {"id": "grp_transfers", "type": "transfer"},
            },
        ]

        # Get set of names we've already added
        existing_names = {cat["name"] for cat in base_categories}

        # Map group names to group IDs
        group_id_map = {
            "Business": "grp_business",
            "Cash & ATM": "grp_cash",
            "Food & Dining": "grp_food",
            "Travel": "grp_travel",
            "Automotive": "grp_transport",
            "Services": "grp_home",
            "Housing": "grp_home",
            "Shopping": "grp_shopping",
            "Entertainment": "grp_entertainment",
            "Education": "grp_education",
            "Health & Fitness": "grp_health",
            "Gifts & Charity": "grp_gifts",
            "Bills & Utilities": "grp_bills",
            "Financial": "grp_financial",
            "Personal Care": "grp_personal",
            "Income": "grp_income",
            "Transfers": "grp_transfers",
            "Uncategorized": "grp_uncategorized",
        }

        # Add all categories from DEFAULT_CATEGORY_GROUPS that aren't already in base list
        cat_id_counter = 100  # Start at 100 to avoid conflicts with hardcoded IDs
        for group_name, category_list in DEFAULT_CATEGORY_GROUPS.items():
            group_id = group_id_map.get(group_name, f"grp_{group_name.lower().replace(' ', '_')}")
            group_type = (
                "income"
                if group_name == "Income"
                else ("transfer" if group_name == "Transfers" else "expense")
            )

            for cat_name in category_list:
                # Skip if already in base categories
                if cat_name in existing_names:
                    continue

                # Add new category
                cat_id = f"cat_{cat_id_counter:03d}"
                cat_id_counter += 1

                base_categories.append(
                    {"id": cat_id, "name": cat_name, "group": {"id": group_id, "type": group_type}}
                )

        return base_categories

    def _generate_transactions(self) -> List[Dict]:
        """Generate multiple years of realistic transactions."""
        transactions = []

        # Generate for each year
        for year_offset in range(self.years):
            current_year = self.start_year + year_offset
            # Generate for each month in the year
            for month in range(1, 13):
                transactions.extend(self._generate_month_transactions(current_year, month))

        return transactions

    def _generate_month_transactions(self, year: int, month: int) -> List[Dict]:
        """Generate transactions for a single month in a specific year."""
        transactions = []

        # Income - biweekly paychecks (1st and 15th)
        transactions.extend(self._generate_paychecks(year, month))

        # Fixed recurring expenses
        transactions.extend(self._generate_recurring_expenses(year, month))

        # Variable expenses
        transactions.extend(self._generate_groceries(year, month))
        transactions.extend(self._generate_restaurants(year, month))
        transactions.extend(self._generate_coffee(year, month))
        transactions.extend(self._generate_gas(year, month))
        transactions.extend(self._generate_amazon(year, month))
        transactions.extend(self._generate_shopping(year, month))
        transactions.extend(self._generate_entertainment(year, month))

        # Occasional expenses
        if month in [3, 6, 9, 12]:  # Quarterly
            transactions.extend(self._generate_travel(year, month))

        # Add some duplicates for testing (1-2% of transactions)
        if random.random() < 0.5:
            transactions.extend(self._create_duplicate_transactions(transactions))

        # Add some transfers
        transactions.extend(self._generate_transfers(year, month))

        return transactions

    def _generate_paychecks(self, year: int, month: int) -> List[Dict]:
        """Generate biweekly paychecks."""
        transactions = []

        # Person 1: ~$4,300 biweekly
        # Person 2: ~$2,900 biweekly
        pay_dates = [1, 15]  # Simplified - 1st and 15th

        for day in pay_dates:
            # Person 1 paycheck
            transactions.append(
                self._create_transaction(
                    year,
                    month,
                    day,
                    amount=4300 + random.uniform(-50, 50),
                    merchant="Employer 1 Payroll",
                    category_id="cat_paycheck",
                    account="Chase Checking",
                )
            )

            # Person 2 paycheck
            transactions.append(
                self._create_transaction(
                    year,
                    month,
                    day,
                    amount=2900 + random.uniform(-50, 50),
                    merchant="Employer 2 Payroll",
                    category_id="cat_paycheck",
                    account="Chase Checking",
                )
            )

        return transactions

    def _generate_recurring_expenses(self, year: int, month: int) -> List[Dict]:
        """Generate monthly recurring bills."""
        transactions = []

        # Rent on 1st
        transactions.append(
            self._create_transaction(
                year,
                month,
                1,
                amount=-3400,
                merchant="Property Management Co",
                category_id="cat_rent",
                account="Chase Checking",
            )
        )

        # Utilities mid-month
        transactions.append(
            self._create_transaction(
                year,
                month,
                15,
                amount=-random.uniform(150, 250),
                merchant="Pacific Gas & Electric",
                category_id="cat_utilities",
                account="Chase Checking",
            )
        )

        # Internet
        transactions.append(
            self._create_transaction(
                year,
                month,
                5,
                amount=-89.99,
                merchant="Comcast",
                category_id="cat_internet",
                account="Chase Checking",
            )
        )

        # Phone
        transactions.append(
            self._create_transaction(
                year,
                month,
                10,
                amount=-140,
                merchant="Verizon Wireless",
                category_id="cat_phone",
                account="Chase Sapphire Reserve",
            )
        )

        # Gym memberships
        transactions.append(
            self._create_transaction(
                year,
                month,
                3,
                amount=-120,
                merchant="Equinox Fitness",
                category_id="cat_gym",
                account="Chase Sapphire Reserve",
            )
        )

        # Streaming services
        for service, amount in [("Netflix", 22.99), ("Spotify Premium", 16.99), ("HBO Max", 15.99)]:
            transactions.append(
                self._create_transaction(
                    year,
                    month,
                    random.randint(5, 10),
                    amount=-amount,
                    merchant=service,
                    category_id="cat_streaming",
                    account="Chase Sapphire Reserve",
                    is_recurring=True,
                )
            )

        # Insurance
        transactions.append(
            self._create_transaction(
                year,
                month,
                1,
                amount=-185,
                merchant="State Farm Insurance",
                category_id="cat_insurance",
                account="Chase Checking",
            )
        )

        return transactions

    def _generate_groceries(self, year: int, month: int) -> List[Dict]:
        """Generate grocery shopping transactions."""
        transactions = []

        # 8-12 grocery trips per month
        num_trips = random.randint(8, 12)
        grocery_stores = [
            "Whole Foods Market",
            "WHOLE FOODS MARKET #123",  # Name variation for testing
            "Trader Joe's",
            "Safeway",
        ]

        for _ in range(num_trips):
            day = random.randint(1, 28)
            store = random.choice(grocery_stores)
            amount = -random.uniform(60, 180)

            transactions.append(
                self._create_transaction(
                    year,
                    month,
                    day,
                    amount=amount,
                    merchant=store,
                    category_id="cat_groceries",
                    account=random.choice(["Chase Checking", "Chase Sapphire Reserve"]),
                )
            )

        return transactions

    def _generate_restaurants(self, year: int, month: int) -> List[Dict]:
        """Generate restaurant transactions."""
        transactions = []

        # 12-18 restaurant visits per month
        num_visits = random.randint(12, 18)
        restaurants = [
            "Chipotle Mexican Grill",
            "Shake Shack",
            "The French Laundry",
            "Local Bistro",
            "Sushi Bar",
            "Italian Restaurant",
            "Thai Kitchen",
        ]

        for _ in range(num_visits):
            day = random.randint(1, 28)
            restaurant = random.choice(restaurants)
            # Weekend dinners more expensive
            if day % 7 in [5, 6]:  # Rough weekend approximation
                amount = -random.uniform(60, 150)
            else:
                amount = -random.uniform(25, 80)

            transactions.append(
                self._create_transaction(
                    year,
                    month,
                    day,
                    amount=amount,
                    merchant=restaurant,
                    category_id="cat_restaurants",
                    account="Chase Sapphire Reserve",  # Get points on dining
                )
            )

        return transactions

    def _generate_coffee(self, year: int, month: int) -> List[Dict]:
        """Generate coffee shop transactions."""
        transactions = []

        # 15-25 coffee purchases per month
        num_visits = random.randint(15, 25)
        coffee_shops = [
            "Starbucks",
            "STARBUCKS #1234",  # Name variation
            "Blue Bottle Coffee",
            "Local Coffee Shop",
            "Peet's Coffee",
        ]

        for _ in range(num_visits):
            day = random.randint(1, 28)
            shop = random.choice(coffee_shops)
            amount = -random.uniform(4.50, 12.00)

            transactions.append(
                self._create_transaction(
                    year,
                    month,
                    day,
                    amount=amount,
                    merchant=shop,
                    category_id="cat_coffee",
                    account=random.choice(["Chase Checking", "Chase Sapphire Reserve"]),
                )
            )

        return transactions

    def _generate_gas(self, year: int, month: int) -> List[Dict]:
        """Generate gas station transactions."""
        transactions = []

        # 4-6 fillups per month
        num_fillups = random.randint(4, 6)
        gas_stations = [
            "Shell",
            "Chevron",
            "76 Gas Station",
        ]

        for _ in range(num_fillups):
            day = random.randint(1, 28)
            station = random.choice(gas_stations)
            amount = -random.uniform(45, 75)

            transactions.append(
                self._create_transaction(
                    year,
                    month,
                    day,
                    amount=amount,
                    merchant=station,
                    category_id="cat_gas",
                    account="Chase Sapphire Reserve",
                )
            )

        return transactions

    def _generate_amazon(self, year: int, month: int) -> List[Dict]:
        """Generate Amazon purchases with name variations."""
        transactions = []

        # 6-10 Amazon purchases per month
        num_purchases = random.randint(6, 10)
        amazon_names = [
            "Amazon",
            "AMAZON.COM",
            "Amazon Marketplace",
            "AMZN Mktp US",
        ]

        for _ in range(num_purchases):
            day = random.randint(1, 28)
            name = random.choice(amazon_names)
            amount = -random.uniform(15, 250)

            # Sometimes miscategorized (should be edit_categoryd in demo)
            category = random.choice(
                [
                    "cat_amazon",
                    "cat_shopping",
                    "cat_groceries",  # Sometimes groceries from Amazon
                ]
            )

            transactions.append(
                self._create_transaction(
                    year,
                    month,
                    day,
                    amount=amount,
                    merchant=name,
                    category_id=category,
                    account="Amex Platinum",
                )
            )

        return transactions

    def _generate_shopping(self, year: int, month: int) -> List[Dict]:
        """Generate misc shopping transactions."""
        transactions = []

        # 4-8 shopping trips per month
        num_trips = random.randint(4, 8)
        stores = [
            "Target",
            "Nordstrom",
            "Apple Store",
            "Best Buy",
            "IKEA",
        ]

        for _ in range(num_trips):
            day = random.randint(1, 28)
            store = random.choice(stores)
            amount = -random.uniform(50, 400)

            transactions.append(
                self._create_transaction(
                    year,
                    month,
                    day,
                    amount=amount,
                    merchant=store,
                    category_id="cat_shopping",
                    account=random.choice(["Chase Sapphire Reserve", "Amex Platinum"]),
                )
            )

        return transactions

    def _generate_entertainment(self, year: int, month: int) -> List[Dict]:
        """Generate entertainment transactions."""
        transactions = []

        # 2-5 entertainment expenses per month
        num_events = random.randint(2, 5)
        venues = [
            "AMC Theaters",
            "Concert Venue",
            "Museum",
            "Theater",
            "Sports Event",
        ]

        for _ in range(num_events):
            day = random.randint(1, 28)
            venue = random.choice(venues)
            amount = -random.uniform(30, 200)

            transactions.append(
                self._create_transaction(
                    year,
                    month,
                    day,
                    amount=amount,
                    merchant=venue,
                    category_id="cat_streaming",
                    account="Chase Sapphire Reserve",
                )
            )

        return transactions

    def _generate_travel(self, year: int, month: int) -> List[Dict]:
        """Generate travel-related transactions (quarterly)."""
        transactions = []

        # Flight
        transactions.append(
            self._create_transaction(
                year,
                month,
                random.randint(1, 10),
                amount=-random.uniform(600, 1200),
                merchant="United Airlines",
                category_id="cat_streaming",
                account="Chase Sapphire Reserve",
            )
        )

        # Hotel
        transactions.append(
            self._create_transaction(
                year,
                month,
                random.randint(15, 25),
                amount=-random.uniform(800, 1500),
                merchant="Marriott Hotels",
                category_id="cat_streaming",
                account="Chase Sapphire Reserve",
            )
        )

        return transactions

    def _generate_transfers(self, year: int, month: int) -> List[Dict]:
        """Generate internal transfers (should be hidden from reports)."""
        transactions = []

        # Savings transfer each month
        transactions.append(
            self._create_transaction(
                year,
                month,
                2,
                amount=-2000,
                merchant="Transfer to Savings",
                category_id="cat_transfer",
                account="Chase Checking",
                hide_from_reports=True,
            )
        )

        # Credit card payment
        transactions.append(
            self._create_transaction(
                year,
                month,
                20,
                amount=-random.uniform(2000, 4000),
                merchant="Credit Card Payment",
                category_id="cat_transfer",
                account="Chase Checking",
                hide_from_reports=True,
            )
        )

        return transactions

    def _create_duplicate_transactions(self, existing: List[Dict]) -> List[Dict]:
        """Create some duplicate transactions for testing duplicate detection."""
        duplicates = []

        # Pick a random transaction and duplicate it
        if existing:
            original = random.choice(existing)
            # Create exact duplicate (accidental double-charge scenario)
            duplicate = original.copy()
            duplicate["id"] = self._generate_id()
            duplicates.append(duplicate)

        return duplicates

    def _create_transaction(
        self,
        year: int,
        month: int,
        day: int,
        amount: float,
        merchant: str,
        category_id: str,
        account: str,
        hide_from_reports: bool = False,
        is_recurring: bool = False,
    ) -> Dict[str, Any]:
        """Create a single transaction."""
        txn_id = self._generate_id()

        # Map account name to ID
        account_map = {
            "Chase Checking": {"id": "acc_chase_checking", "displayName": "Chase Checking"},
            "Chase Savings": {"id": "acc_chase_savings", "displayName": "Chase Savings"},
            "Chase Sapphire Reserve": {
                "id": "acc_chase_sapphire",
                "displayName": "Chase Sapphire Reserve",
            },
            "Amex Platinum": {"id": "acc_amex_platinum", "displayName": "Amex Platinum"},
        }

        account_info = account_map.get(account, {"id": "acc_unknown", "displayName": account})

        # Find category name from ID
        category_names = {
            "cat_groceries": "Groceries",
            "cat_restaurants": "Restaurants & Bars",
            "cat_coffee": "Coffee Shops",
            "cat_gas": "Gas",
            "cat_parking": "Parking & Tolls",
            "cat_uber": "Taxi & Ride Shares",
            "cat_rent": "Rent",
            "cat_utilities": "Gas & Electric",
            "cat_internet": "Internet & Cable",
            "cat_shopping": "Shopping",
            "cat_amazon": "Online Shopping",
            "cat_streaming": "Entertainment & Recreation",
            "cat_gym": "Fitness",
            "cat_medical": "Medical",
            "cat_phone": "Phone",
            "cat_insurance": "Insurance",
            "cat_paycheck": "Paychecks",
            "cat_transfer": "Transfer",
        }

        return {
            "id": txn_id,
            "date": f"{year}-{month:02d}-{min(day, 28):02d}",
            "amount": round(amount, 2),
            "merchant": {
                "id": f"merch_{hashlib.md5(merchant.encode()).hexdigest()[:8]}",
                "name": merchant,
            },
            "category": {
                "id": category_id,
                "name": category_names.get(category_id, "Uncategorized"),
            },
            "account": account_info,
            "notes": "",
            "hideFromReports": hide_from_reports,
            "pending": False,
            "isRecurring": is_recurring,
        }

    def _generate_id(self) -> str:
        """Generate a unique transaction ID."""
        txn_id = f"demo_txn_{self.transaction_counter:06d}"
        self.transaction_counter += 1
        return txn_id


def generate_demo_data(start_year: int = 2023, years: int = 3) -> tuple:
    """
    Generate demo data for multiple years.

    Args:
        start_year: First year to generate data for
        years: Number of years of data to generate (default: 3)

    Returns:
        Tuple of (transactions, categories, category_groups)
    """
    generator = DemoDataGenerator(start_year=start_year, years=years)
    return generator.generate_full_year()

package fixture

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
)

// DemoSeed fixes the synthetic demo dataset so captures and tests stay reproducible.
const DemoSeed = 20261007

const (
	demoProvider       = "demo"
	demoScale    uint8 = 2
)

type demoAccount struct {
	id, name string
	currency domain.Currency
}

var (
	demoChecking = demoAccount{"account-checking", "Everyday Checking", "USD"}
	demoCard     = demoAccount{"account-card", "Rewards Card", "USD"}
	demoSavings  = demoAccount{"account-savings", "High-Yield Savings", "USD"}
	demoTravel   = demoAccount{"account-travel-eur", "Travel Card (EUR)", "EUR"}
)

type demoCategory struct {
	id, name, group string
}

type demoMerchant struct {
	id, name string
}

type demoCadence int

const (
	demoMonthly demoCadence = iota
	demoSemiMonthly
	demoWeekly
	demoBiweekly
	demoMostWeekdays
	demoQuarterly
	demoSemiannual
	demoAnnual
	demoTravelSeasons
	demoSummerWeekends
	demoTripDaily
	demoTripMostDays
	demoTripCheckout
)

type demoPlan struct {
	merchant demoMerchant
	category demoCategory
	account  demoAccount
	cadence  demoCadence
	day      int
	minCents int64
	maxCents int64
	income   bool
	mirror   *demoAccount
	// seasonalPercent scales an amount by month as a whole percentage, keeping money integral.
	seasonalPercent func(month time.Month) int64
	// alternate replaces the category on every alternateEvery-th occurrence, so top
	// categories and their percentages carry information for mixed merchants.
	alternate      *demoCategory
	alternateEvery int
	// rawNames replace the merchant from demoRawNamesFrom onward, cycling by occurrence, so
	// recent imports carry the messy provider names that users clean up.
	rawNames []demoMerchant
	// refundEvery adds a same-day refund of the purchase on every refundEvery-th occurrence.
	refundEvery int
}

var demoCategories = map[string]demoCategory{
	"rent":      {"category-rent", "Rent", "Housing"},
	"utilities": {"category-utilities", "Utilities", "Housing"},
	"internet":  {"category-internet", "Internet", "Housing"},
	"insurance": {"category-insurance", "Insurance", "Housing"},
	"groceries": {"category-groceries", "Groceries", "Food"},
	"dining":    {"category-dining", "Dining", "Food"},
	"coffee":    {"category-coffee", "Coffee", "Food"},
	"transit":   {"category-transit", "Transit", "Transport"},
	"fuel":      {"category-fuel", "Fuel", "Transport"},
	"rideshare": {"category-rideshare", "Rideshare", "Transport"},
	"auto":      {"category-auto", "Auto", "Transport"},
	"shopping":  {"category-shopping", "Shopping", "Shopping"},
	"books":     {"category-books", "Books", "Shopping"},
	"clothing":  {"category-clothing", "Clothing", "Shopping"},
	"home":      {"category-home", "Home", "Shopping"},
	"pharmacy":  {"category-pharmacy", "Pharmacy", "Health"},
	"fitness":   {"category-fitness", "Fitness", "Health"},
	"medical":   {"category-medical", "Medical", "Health"},
	"streaming": {"category-streaming", "Streaming", "Entertainment"},
	"events":    {"category-events", "Events", "Entertainment"},
	"flights":   {"category-flights", "Flights", "Travel"},
	"lodging":   {"category-lodging", "Lodging", "Travel"},
	"salary":    {"category-salary", "Salary", "Income"},
	"interest":  {"category-interest", "Interest", "Income"},
	"refunds":   {"category-refunds", "Refunds", "Income"},
	"freelance": {"category-freelance", "Freelance", "Income"},
	"transfer":  {"category-transfer", "Transfer", "Transfers"},
	"payment":   {"category-card-payment", "Credit Card Payment", "Transfers"},
}

func demoCategoryRef(key string) *demoCategory {
	category := demoCategories[key]
	return &category
}

func winterSummerPercent(month time.Month) int64 {
	switch month {
	case time.December, time.January, time.February, time.July, time.August:
		return 160
	case time.November, time.March, time.June, time.September:
		return 120
	default:
		return 100
	}
}

func holidayPercent(month time.Month) int64 {
	switch month {
	case time.December:
		return 240
	case time.November:
		return 170
	default:
		return 100
	}
}

func demoPlans() []demoPlan {
	plans := demoBillPlans()
	plans = append(plans, demoEverydayPlans()...)
	plans = append(plans, demoLeisurePlans()...)
	plans = append(plans, demoIncomePlans()...)
	return append(plans, demoTripPlans()...)
}

func demoBillPlans() []demoPlan {
	c := demoCategories
	return []demoPlan{
		{merchant: demoMerchant{"merchant-rent", "Maple Court Apartments"}, category: c["rent"],
			account: demoChecking, cadence: demoMonthly, day: 1, minCents: 215000, maxCents: 215000},
		{merchant: demoMerchant{"merchant-power", "City Power & Light"}, category: c["utilities"],
			account: demoChecking, cadence: demoMonthly, day: 12, minCents: 7200, maxCents: 11800,
			seasonalPercent: winterSummerPercent},
		{merchant: demoMerchant{"merchant-water", "Metro Water Utility"}, category: c["utilities"],
			account: demoChecking, cadence: demoMonthly, day: 18, minCents: 3900, maxCents: 6600},
		{merchant: demoMerchant{"merchant-fiber", "Fiber Internet Co"}, category: c["internet"],
			account: demoCard, cadence: demoMonthly, day: 6, minCents: 7900, maxCents: 7900},
		{merchant: demoMerchant{"merchant-renters", "Renters Insurance Co"}, category: c["insurance"],
			account: demoChecking, cadence: demoMonthly, day: 22, minCents: 1850, maxCents: 1850},
		{merchant: demoMerchant{"merchant-autoins", "Auto Insurance Co"}, category: c["auto"],
			account: demoChecking, cadence: demoMonthly, day: 9, minCents: 12400, maxCents: 12400},
		{merchant: demoMerchant{"merchant-fitness", "Fitness Club"}, category: c["fitness"],
			account: demoCard, cadence: demoMonthly, day: 3, minCents: 4900, maxCents: 4900},
		{merchant: demoMerchant{"merchant-video", "Streaming Video"}, category: c["streaming"],
			account: demoCard, cadence: demoMonthly, day: 8, minCents: 1599, maxCents: 1599},
		{merchant: demoMerchant{"merchant-music", "Music Streaming"}, category: c["streaming"],
			account: demoCard, cadence: demoMonthly, day: 11, minCents: 1099, maxCents: 1099},
		{merchant: demoMerchant{"merchant-dental", "Downtown Dental"}, category: c["medical"],
			account: demoChecking, cadence: demoSemiannual, day: 16, minCents: 18000, maxCents: 18000},
	}
}

func demoEverydayPlans() []demoPlan {
	c := demoCategories
	return []demoPlan{
		{merchant: demoMerchant{"merchant-grocery", "Corner Grocery"}, category: c["groceries"],
			account: demoCard, cadence: demoWeekly, day: int(time.Saturday),
			minCents: 5400, maxCents: 14200, alternate: demoCategoryRef("home"), alternateEvery: 6,
			rawNames: []demoMerchant{
				{"merchant-grocery-1182", "CORNER GROCERY #1182"},
				{"merchant-grocery-0447", "CORNER GROCERY #0447"},
				{"merchant-grocery-online", "CORNER GROCERY ONLINE"},
			}},
		{merchant: demoMerchant{"merchant-market", "Riverside Farmers Market"},
			category: c["groceries"], account: demoCard, cadence: demoSummerWeekends,
			day: int(time.Sunday), minCents: 1800, maxCents: 4600},
		{merchant: demoMerchant{"merchant-cafe", "Bean & Leaf Cafe"}, category: c["coffee"],
			account: demoCard, cadence: demoMostWeekdays, minCents: 425, maxCents: 875,
			alternate: demoCategoryRef("dining"), alternateEvery: 5,
			rawNames: []demoMerchant{
				{"merchant-sq-beanleaf", "SQ *BEAN & LEAF"},
				{"merchant-cafe", "Bean & Leaf Cafe"},
			}},
		{merchant: demoMerchant{"merchant-noodle", "Noodle House"}, category: c["dining"],
			account: demoCard, cadence: demoWeekly, day: int(time.Thursday),
			minCents: 2200, maxCents: 4800},
		{merchant: demoMerchant{"merchant-pizza", "Slice Pizza"}, category: c["dining"],
			account: demoCard, cadence: demoBiweekly, day: int(time.Friday),
			minCents: 1900, maxCents: 3400},
		{merchant: demoMerchant{"merchant-taco", "Taco Truck"}, category: c["dining"],
			account: demoCard, cadence: demoWeekly, day: int(time.Tuesday),
			minCents: 1100, maxCents: 1800},
		{merchant: demoMerchant{"merchant-transit", "Metro Transit"}, category: c["transit"],
			account: demoCard, cadence: demoWeekly, day: int(time.Monday),
			minCents: 2500, maxCents: 2500},
		{merchant: demoMerchant{"merchant-rideshare", "Rideshare Co"}, category: c["rideshare"],
			account: demoCard, cadence: demoBiweekly, day: int(time.Saturday),
			minCents: 1200, maxCents: 3800, alternate: demoCategoryRef("events"), alternateEvery: 3},
		{merchant: demoMerchant{"merchant-fuel", "Northside Fuel"}, category: c["fuel"],
			account: demoCard, cadence: demoBiweekly, day: int(time.Wednesday),
			minCents: 3800, maxCents: 7200},
		{merchant: demoMerchant{"merchant-pharmacy", "Corner Pharmacy"}, category: c["pharmacy"],
			account: demoCard, cadence: demoBiweekly, day: int(time.Tuesday),
			minCents: 800, maxCents: 4200, alternate: demoCategoryRef("medical"), alternateEvery: 4},
	}
}

func demoLeisurePlans() []demoPlan {
	c := demoCategories
	return []demoPlan{
		{merchant: demoMerchant{"merchant-megastore", "Online Megastore"}, category: c["shopping"],
			account: demoCard, cadence: demoWeekly, day: int(time.Wednesday),
			minCents: 1200, maxCents: 9800, seasonalPercent: holidayPercent,
			alternate: demoCategoryRef("home"), alternateEvery: 3, refundEvery: 9},
		{merchant: demoMerchant{"merchant-bookshop", "Bookshop on Main"}, category: c["books"],
			account: demoCard, cadence: demoMonthly, day: 14, minCents: 1400, maxCents: 4200,
			alternate: demoCategoryRef("coffee"), alternateEvery: 4},
		{merchant: demoMerchant{"merchant-outfitters", "Outfitters"}, category: c["clothing"],
			account: demoCard, cadence: demoMonthly, day: 20, minCents: 3500, maxCents: 14000,
			seasonalPercent: holidayPercent},
		{merchant: demoMerchant{"merchant-hardware", "Hardware Depot"}, category: c["home"],
			account: demoCard, cadence: demoMonthly, day: 27, minCents: 2000, maxCents: 11000},
		{merchant: demoMerchant{"merchant-cinema", "Grand Cinema"}, category: c["events"],
			account: demoCard, cadence: demoMonthly, day: 24, minCents: 1600, maxCents: 3600},
		{merchant: demoMerchant{"merchant-tickets", "Ticket Vendor"}, category: c["events"],
			account: demoCard, cadence: demoQuarterly, day: 5, minCents: 6000, maxCents: 18000},
		{merchant: demoMerchant{"merchant-airline", "Skyway Airlines"}, category: c["flights"],
			account: demoCard, cadence: demoTravelSeasons, day: 7, minCents: 24000, maxCents: 62000},
		{merchant: demoMerchant{"merchant-hotel", "Harbor Hotel"}, category: c["lodging"],
			account: demoCard, cadence: demoTravelSeasons, day: 21, minCents: 31000, maxCents: 78000},
	}
}

func demoIncomePlans() []demoPlan {
	c := demoCategories
	return []demoPlan{
		{merchant: demoMerchant{"merchant-payroll", "Northwind Payroll"}, category: c["salary"],
			account: demoChecking, cadence: demoSemiMonthly, minCents: 340000, maxCents: 340000,
			income: true},
		{merchant: demoMerchant{"merchant-interest", "High-Yield Savings"}, category: c["interest"],
			account: demoSavings, cadence: demoMonthly, day: 28, minCents: 1200, maxCents: 3800,
			income: true},
		{merchant: demoMerchant{"merchant-taxes", "Tax Refund"}, category: c["refunds"],
			account: demoChecking, cadence: demoAnnual, day: 19, minCents: 60000, maxCents: 140000,
			income: true},
		{merchant: demoMerchant{"merchant-freelance", "Freelance Client"}, category: c["freelance"],
			account: demoChecking, cadence: demoQuarterly, day: 25, minCents: 75000,
			maxCents: 160000, income: true},
		{merchant: demoMerchant{"merchant-savings", "Savings Transfer"}, category: c["transfer"],
			account: demoChecking, cadence: demoMonthly, day: 2, minCents: 50000, maxCents: 50000,
			mirror: &demoSavings},
		{merchant: demoMerchant{"merchant-cardpay", "Credit Card Payment"}, category: c["payment"],
			account: demoChecking, cadence: demoMonthly, day: 26, minCents: 120000,
			maxCents: 190000, mirror: &demoCard},
	}
}

func demoTripPlans() []demoPlan {
	c := demoCategories
	return []demoPlan{
		{merchant: demoMerchant{"merchant-bistro", "Bistro du Port"}, category: c["dining"],
			account: demoTravel, cadence: demoTripDaily, minCents: 1800, maxCents: 6400},
		{merchant: demoMerchant{"merchant-citymetro", "City Metro"}, category: c["transit"],
			account: demoTravel, cadence: demoTripDaily, minCents: 210, maxCents: 840},
		{merchant: demoMerchant{"merchant-museum", "City Museum"}, category: c["events"],
			account: demoTravel, cadence: demoTripMostDays, minCents: 1200, maxCents: 2400},
		{merchant: demoMerchant{"merchant-riviera", "Hotel Riviera"}, category: c["lodging"],
			account: demoTravel, cadence: demoTripCheckout, minCents: 64000, maxCents: 98000},
	}
}

type demoTrip struct {
	first, last domain.Date
}

func demoTrips() []demoTrip {
	return []demoTrip{
		{mustDemoDate(2025, time.June, 9), mustDemoDate(2025, time.June, 15)},
		{mustDemoDate(2026, time.May, 18), mustDemoDate(2026, time.May, 23)},
	}
}

func demoTripFor(date domain.Date) (demoTrip, bool) {
	for _, trip := range demoTrips() {
		if date.Compare(trip.first) >= 0 && date.Compare(trip.last) <= 0 {
			return trip, true
		}
	}
	return demoTrip{}, false
}

type demoDay struct {
	date    domain.Date
	weekday time.Weekday
	week    int
}

func newDemoDay(date domain.Date) demoDay {
	calendar := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	_, week := calendar.ISOWeek()
	return demoDay{date: date, weekday: calendar.Weekday(), week: week}
}

func (day demoDay) monthIn(months ...time.Month) bool {
	return slices.Contains(months, day.date.Month())
}

func (day demoDay) onPlanDay(plan demoPlan) bool { return day.date.Day() == plan.day }

func (day demoDay) onPlanWeekday(plan demoPlan) bool { return int(day.weekday) == plan.day }

func (day demoDay) weekend() bool {
	return day.weekday == time.Saturday || day.weekday == time.Sunday
}

type demoRule func(plan demoPlan, day demoDay, random *rand.Rand) bool

var demoCadenceRules = map[demoCadence]demoRule{
	demoMonthly: func(plan demoPlan, day demoDay, _ *rand.Rand) bool {
		return day.onPlanDay(plan)
	},
	demoSemiMonthly: func(_ demoPlan, day demoDay, _ *rand.Rand) bool {
		return day.date.Day() == 1 || day.date.Day() == 15
	},
	demoWeekly: func(plan demoPlan, day demoDay, _ *rand.Rand) bool {
		return day.onPlanWeekday(plan)
	},
	demoBiweekly: func(plan demoPlan, day demoDay, _ *rand.Rand) bool {
		return day.onPlanWeekday(plan) && day.week%2 == 0
	},
	demoMostWeekdays: func(_ demoPlan, day demoDay, random *rand.Rand) bool {
		return !day.weekend() && random.Intn(100) < 55
	},
	demoQuarterly: func(plan demoPlan, day demoDay, _ *rand.Rand) bool {
		return day.onPlanDay(plan) && day.monthIn(time.January, time.April, time.July, time.October)
	},
	demoSemiannual: func(plan demoPlan, day demoDay, _ *rand.Rand) bool {
		return day.onPlanDay(plan) && day.monthIn(time.February, time.August)
	},
	demoAnnual: func(plan demoPlan, day demoDay, _ *rand.Rand) bool {
		return day.onPlanDay(plan) && day.monthIn(time.March)
	},
	demoTravelSeasons: func(plan demoPlan, day demoDay, _ *rand.Rand) bool {
		return day.onPlanDay(plan) && day.monthIn(time.March, time.July, time.November)
	},
	demoSummerWeekends: func(plan demoPlan, day demoDay, _ *rand.Rand) bool {
		month := day.date.Month()
		return day.onPlanWeekday(plan) && day.week%2 == 1 &&
			month >= time.April && month <= time.October
	},
	demoTripDaily: func(_ demoPlan, day demoDay, _ *rand.Rand) bool {
		_, traveling := demoTripFor(day.date)
		return traveling
	},
	demoTripMostDays: func(_ demoPlan, day demoDay, random *rand.Rand) bool {
		_, traveling := demoTripFor(day.date)
		return traveling && random.Intn(100) < 40
	},
	demoTripCheckout: func(_ demoPlan, day demoDay, _ *rand.Rand) bool {
		trip, traveling := demoTripFor(day.date)
		return traveling && day.date.Compare(trip.last) == 0
	},
}

type demoEntry struct {
	date     domain.Date
	merchant demoMerchant
	category demoCategory
	account  demoAccount
	cents    int64
	notes    string
}

type demoBuilder struct {
	sequence     int
	pendingFrom  domain.Date
	transactions []domain.Transaction
}

func (builder *demoBuilder) nextID() string {
	builder.sequence++
	return fmt.Sprintf("demo-%06d", builder.sequence)
}

func (builder *demoBuilder) add(entry demoEntry) {
	groupID, err := syntheticGroupID(entry.category.group)
	if err != nil {
		panic("demo generator has an invalid category group")
	}
	id := builder.nextID()
	builder.transactions = append(builder.transactions, domain.Transaction{
		ID: id, ProviderID: "provider-" + id, Provider: demoProvider,
		Account:  domain.EntityRef{ID: entry.account.id, Name: entry.account.name},
		Date:     entry.date,
		Merchant: domain.EntityRef{ID: entry.merchant.id, Name: entry.merchant.name},
		Category: domain.CategoryRef{
			ID: entry.category.id, Name: entry.category.name,
			GroupID: groupID, Group: entry.category.group,
		},
		Amount: domain.Money{
			Minor: entry.cents, Currency: entry.account.currency, Scale: demoScale,
		},
		Pending: entry.date.Compare(builder.pendingFrom) >= 0,
		Notes:   entry.notes,
	})
}

func (builder *demoBuilder) addOccurrence(
	plan demoPlan, date domain.Date, occurrence int, random *rand.Rand,
) {
	category := plan.category
	if plan.alternate != nil && occurrence%plan.alternateEvery == 0 {
		category = *plan.alternate
	}
	merchant := demoMerchantFor(plan, date, occurrence)
	cents := demoAmount(plan, date, random)
	signed := -cents
	if plan.income {
		signed = cents
	}
	builder.add(demoEntry{date, merchant, category, plan.account, signed, ""})
	if plan.mirror != nil {
		builder.add(demoEntry{date, merchant, category, *plan.mirror, cents, ""})
	}
	if plan.refundEvery > 0 && occurrence%plan.refundEvery == 0 {
		builder.add(demoEntry{date, merchant, category, plan.account, cents, "Returned item"})
	}
}

// Demo returns the deterministic synthetic household behind `--demo`. It spans 2024-01-01
// through 2026-10-07 with USD checking, card, and savings accounts plus a EUR travel card. It
// has recurring merchants, seasonal spending, refunds, income, transfers, raw provider names on
// recent charges, hidden and pending rows, two uncategorized rows, and one duplicate pair.
func Demo() []domain.Transaction {
	//nolint:gosec // reproducible synthetic data, not security.
	random := rand.New(rand.NewSource(DemoSeed))
	builder := &demoBuilder{
		pendingFrom:  mustDemoDate(2026, time.October, 1),
		transactions: make([]domain.Transaction, 0, 2800),
	}
	occurrences := make(map[string]int)
	plans := demoPlans()
	first, last := mustDemoDate(2024, time.January, 1), mustDemoDate(2026, time.October, 7)
	for date := first; date.Compare(last) <= 0; date = mustNextDemoDay(date) {
		day := newDemoDay(date)
		for _, plan := range plans {
			if !demoCadenceRules[plan.cadence](plan, day, random) {
				continue
			}
			occurrences[plan.merchant.id]++
			builder.addOccurrence(plan, date, occurrences[plan.merchant.id], random)
		}
	}
	builder.decorate()
	return builder.transactions
}

func (builder *demoBuilder) decorate() {
	reimbursed := 0
	for index := range builder.transactions {
		transaction := &builder.transactions[index]
		lateInMonth := transaction.Date.Day() >= 20
		if transaction.Merchant.ID == "merchant-noodle" && lateInMonth && reimbursed < 6 {
			transaction.Hidden = true
			transaction.Notes = "Reimbursed by work"
			reimbursed++
		}
		if transaction.Merchant.ID == "merchant-hotel" {
			transaction.Notes = "Two nights"
		}
	}
	builder.addDuplicateGroceryCharge(mustDemoDate(2026, time.August, 22))
	unknown := demoMerchant{"merchant-unknown", "Unknown Merchant"}
	uncategorized := demoCategory{syntheticUncategorizedCategoryID, "Uncategorized", "Uncategorized"}
	for _, date := range []domain.Date{
		mustDemoDate(2026, time.August, 14), mustDemoDate(2026, time.August, 29),
	} {
		builder.add(demoEntry{date, unknown, uncategorized, demoCard, -3750, ""})
	}
}

func (builder *demoBuilder) addDuplicateGroceryCharge(date domain.Date) {
	for _, transaction := range builder.transactions {
		grocery := strings.HasPrefix(transaction.Merchant.ID, "merchant-grocery")
		if !grocery || transaction.Date.Compare(date) != 0 {
			continue
		}
		copied := transaction.Clone()
		id := builder.nextID()
		copied.ID, copied.ProviderID = id, "provider-"+id
		copied.Notes = "Possible duplicate charge"
		builder.transactions = append(builder.transactions, copied)
		return
	}
	panic("demo generator has no grocery charge on the duplicate date")
}

func demoRawNamesFrom() domain.Date { return mustDemoDate(2026, time.July, 1) }

func demoMerchantFor(plan demoPlan, date domain.Date, occurrence int) demoMerchant {
	if len(plan.rawNames) == 0 || date.Compare(demoRawNamesFrom()) < 0 {
		return plan.merchant
	}
	return plan.rawNames[occurrence%len(plan.rawNames)]
}

func demoAmount(plan demoPlan, date domain.Date, random *rand.Rand) int64 {
	cents := plan.minCents
	if plan.maxCents > plan.minCents {
		cents += random.Int63n(plan.maxCents - plan.minCents + 1)
	}
	if plan.seasonalPercent != nil {
		cents = cents * plan.seasonalPercent(date.Month()) / 100
	}
	if plan.merchant.id == "merchant-payroll" && date.Compare(mustDemoDate(2025, time.July, 1)) >= 0 {
		cents = 352000
	}
	return cents
}

func mustDemoDate(year int, month time.Month, day int) domain.Date {
	date, err := domain.NewDate(year, month, day)
	if err != nil {
		panic("demo generator has an invalid fixed date")
	}
	return date
}

func mustNextDemoDay(date domain.Date) domain.Date {
	next, err := date.AddDays(1)
	if err != nil {
		panic("demo generator date range exceeds the domain")
	}
	return next
}

package data

const (
	FundamentalFieldMarketCap        = "market_cap"
	FundamentalFieldPERatio          = "pe_ratio"
	FundamentalFieldEPS              = "eps"
	FundamentalFieldRevenue          = "revenue"
	FundamentalFieldRevenueGrowthYoY = "revenue_growth_yoy"
	FundamentalFieldGrossMargin      = "gross_margin"
	FundamentalFieldDebtToEquity     = "debt_to_equity"
	FundamentalFieldFreeCashFlow     = "free_cash_flow"
	FundamentalFieldDividendYield    = "dividend_yield"
)

func IsFundamentalFieldMissing(f Fundamentals, field string) bool {
	for _, missing := range f.MissingFields {
		if missing == field {
			return true
		}
	}
	return false
}

func MissingFundamentalFields(fields ...string) []string {
	seen := make(map[string]bool, len(fields))
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if field == "" || seen[field] {
			continue
		}
		seen[field] = true
		out = append(out, field)
	}
	return out
}

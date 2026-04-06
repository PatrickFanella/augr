package universe

// IndexBoost returns a scoring multiplier for tickers in major indices.
// S&P 500 and Nasdaq 100 get the highest boost; Russell 1000 gets moderate.
// Returns 1.0 for tickers not in any tracked index.
func IndexBoost(ticker string) float64 {
	if _, ok := sp500[ticker]; ok {
		return 3.0
	}
	if _, ok := ndx100[ticker]; ok {
		return 3.0
	}
	if _, ok := russell1000[ticker]; ok {
		return 2.0
	}
	return 1.0
}

// IsIndexMember returns true if the ticker is in any major index.
func IsIndexMember(ticker string) bool {
	return IndexBoost(ticker) > 1.0
}

// sp500 contains S&P 500 constituents (as of early 2026).
// This is a static snapshot — rebalanced quarterly by S&P Dow Jones.
var sp500 = toSet([]string{
	"AAPL", "ABBV", "ABT", "ACN", "ADBE", "ADI", "ADM", "ADP", "ADSK", "AEE",
	"AEP", "AES", "AFL", "AIG", "AIZ", "AJG", "AKAM", "ALB", "ALGN", "ALK",
	"ALL", "ALLE", "AMAT", "AMCR", "AMD", "AME", "AMGN", "AMP", "AMT", "AMZN",
	"ANET", "ANSS", "AON", "AOS", "APA", "APD", "APH", "APTV", "ARE", "ATO",
	"ATVI", "AVB", "AVGO", "AVY", "AWK", "AXP", "AZO", "BA", "BAC", "BAX",
	"BBWI", "BBY", "BDX", "BEN", "BF.B", "BIO", "BIIB", "BK", "BKNG", "BKR",
	"BLK", "BMY", "BR", "BRK.B", "BRO", "BSX", "BWA", "BXP", "C", "CAG",
	"CAH", "CARR", "CAT", "CB", "CBOE", "CBRE", "CCI", "CCL", "CDAY", "CDNS",
	"CDW", "CE", "CEG", "CF", "CFG", "CHD", "CHRW", "CHTR", "CI", "CINF",
	"CL", "CLX", "CMA", "CMCSA", "CME", "CMG", "CMI", "CMS", "CNC", "CNP",
	"COF", "COO", "COP", "COST", "CPB", "CPRT", "CPT", "CRL", "CRM", "CSCO",
	"CSGP", "CSX", "CTAS", "CTLT", "CTRA", "CTSH", "CTVA", "CVS", "CVX", "CZR",
	"D", "DAL", "DD", "DE", "DFS", "DG", "DGX", "DHI", "DHR", "DIS",
	"DISH", "DLR", "DLTR", "DOV", "DOW", "DPZ", "DRI", "DTE", "DUK", "DVA",
	"DVN", "DXC", "DXCM", "EA", "EBAY", "ECL", "ED", "EFX", "EIX", "EL",
	"EMN", "EMR", "ENPH", "EOG", "EPAM", "EQIX", "EQR", "EQT", "ES", "ESS",
	"ETN", "ETR", "ETSY", "EVRG", "EW", "EXC", "EXPD", "EXPE", "EXR", "F",
	"FANG", "FAST", "FBHS", "FCX", "FDS", "FDX", "FE", "FFIV", "FIS", "FISV",
	"FITB", "FLT", "FMC", "FOX", "FOXA", "FRC", "FRT", "FTNT", "FTV", "GD",
	"GE", "GEHC", "GEN", "GILD", "GIS", "GL", "GLW", "GM", "GNRC", "GOOG",
	"GOOGL", "GPC", "GPN", "GRMN", "GS", "GWW", "HAL", "HAS", "HBAN", "HCA",
	"HD", "PEAK", "HES", "HIG", "HII", "HLT", "HOLX", "HON", "HPE", "HPQ",
	"HRL", "HSIC", "HST", "HSY", "HUM", "HWM", "IBM", "ICE", "IDXX", "IEX",
	"IFF", "ILMN", "INCY", "INTC", "INTU", "INVH", "IP", "IPG", "IQV", "IR",
	"IRM", "ISRG", "IT", "ITW", "IVZ", "J", "JBHT", "JCI", "JKHY", "JNJ",
	"JNPR", "JPM", "K", "KDP", "KEY", "KEYS", "KHC", "KIM", "KLAC", "KMB",
	"KMI", "KMX", "KO", "KR", "L", "LDOS", "LEN", "LH", "LHX", "LIN",
	"LKQ", "LLY", "LMT", "LNC", "LNT", "LOW", "LRCX", "LUMN", "LUV", "LVS",
	"LW", "LYB", "LYV", "MA", "MAA", "MAR", "MAS", "MCD", "MCHP", "MCK",
	"MCO", "MDLZ", "MDT", "MET", "META", "MGM", "MHK", "MKC", "MKTX", "MLM",
	"MMC", "MMM", "MNST", "MO", "MOH", "MOS", "MPC", "MPWR", "MRK", "MRNA",
	"MRO", "MS", "MSCI", "MSFT", "MSI", "MTB", "MTCH", "MTD", "MU", "NCLH",
	"NDAQ", "NDSN", "NEE", "NEM", "NFLX", "NI", "NKE", "NOC", "NOW", "NRG",
	"NSC", "NTAP", "NTRS", "NUE", "NVDA", "NVR", "NWL", "NWS", "NWSA", "NXPI",
	"O", "ODFL", "OGN", "OKE", "OMC", "ON", "ORCL", "ORLY", "OTIS", "OXY",
	"PARA", "PAYC", "PAYX", "PCAR", "PCG", "PEAK", "PEG", "PEP", "PFE", "PFG",
	"PG", "PGR", "PH", "PHM", "PKG", "PKI", "PLD", "PM", "PNC", "PNR",
	"PNW", "POOL", "PPG", "PPL", "PRU", "PSA", "PSX", "PTC", "PVH", "PWR",
	"PXD", "PYPL", "QCOM", "QRVO", "RCL", "RE", "REG", "REGN", "RF", "RHI",
	"RJF", "RL", "RMD", "ROK", "ROL", "ROP", "ROST", "RSG", "RTX", "RVTY",
	"SBAC", "SBNY", "SBUX", "SCHW", "SEE", "SHW", "SIVB", "SJM", "SLB", "SNA",
	"SNPS", "SO", "SPG", "SPGI", "SRE", "STE", "STT", "STX", "STZ", "SWK",
	"SWKS", "SYF", "SYK", "SYY", "T", "TAP", "TDG", "TDY", "TECH", "TEL",
	"TER", "TFC", "TFX", "TGT", "TMO", "TMUS", "TPR", "TRGP", "TRMB", "TROW",
	"TRV", "TSCO", "TSLA", "TSN", "TT", "TTWO", "TXN", "TXT", "TYL", "UAL",
	"UDR", "UHS", "ULTA", "UNH", "UNP", "UPS", "URI", "USB", "V", "VFC",
	"VICI", "VLO", "VMC", "VNO", "VRSK", "VRSN", "VRTX", "VTR", "VTRS", "VZ",
	"WAB", "WAT", "WBA", "WBD", "WDC", "WEC", "WELL", "WFC", "WHR", "WM",
	"WMB", "WMT", "WRB", "WRK", "WST", "WTW", "WY", "WYNN", "XEL", "XOM",
	"XRAY", "XYL", "YUM", "ZBH", "ZBRA", "ZION", "ZTS",
})

// ndx100 contains Nasdaq 100 constituents.
var ndx100 = toSet([]string{
	"AAPL", "ABNB", "ADBE", "ADI", "ADP", "ADSK", "AEP", "ALGN", "AMAT", "AMD",
	"AMGN", "AMZN", "ANSS", "APP", "ARM", "ASML", "AVGO", "AZN", "BIIB", "BKNG",
	"BKR", "CCEP", "CDNS", "CDW", "CEG", "CHTR", "CMCSA", "COST", "CPRT", "CRWD",
	"CSCO", "CSGP", "CSX", "CTAS", "CTSH", "DASH", "DDOG", "DLTR", "DXCM", "EA",
	"EXC", "FANG", "FAST", "FTNT", "GEHC", "GFS", "GILD", "GOOG", "GOOGL", "HON",
	"IDXX", "ILMN", "INTC", "INTU", "ISRG", "KDP", "KHC", "KLAC", "LIN", "LRCX",
	"LULU", "MAR", "MCHP", "MDB", "MDLZ", "MELI", "META", "MNST", "MRNA", "MRVL",
	"MSFT", "MU", "NFLX", "NVDA", "NXPI", "ODFL", "ON", "ORLY", "PANW", "PAYX",
	"PCAR", "PDD", "PEP", "PYPL", "QCOM", "REGN", "ROP", "ROST", "SBUX", "SNPS",
	"TEAM", "TMUS", "TSLA", "TTD", "TTWO", "TXN", "VRSK", "VRTX", "WBD", "WDAY",
	"XEL", "ZS",
})

// russell1000 contains Russell 1000 constituents (top ~200 not already in SP500/NDX).
// This is a representative subset — the full index has ~1000 members, most of
// which overlap with S&P 500. We list the additional mid-cap names.
var russell1000 = toSet([]string{
	"ACGL", "AXON", "BILL", "CFLT", "COIN", "CVNA", "DECK", "DOCU", "DUOL", "DKNG",
	"ENPH", "ESTC", "EXAS", "FND", "FRPT", "GLPI", "GNRC", "HUBS", "IAC", "ICE",
	"IOVA", "KNSL", "LPLA", "MASI", "MANH", "MEDP", "MKTX", "MNDY", "MTCH", "NET",
	"NTRA", "NVCR", "OKTA", "PATH", "PCOR", "PCTY", "PEN", "PINS", "PLTR", "PODD",
	"ROKU", "RNG", "RPM", "RPRX", "SAIA", "SCI", "SEDG", "SHOP", "SMAR", "SMCI",
	"SNAP", "SNOW", "SQ", "TOST", "TPL", "TREX", "TWLO", "TW", "U", "UBER",
	"VEEV", "VOYA", "VTRS", "W", "WEX", "WIX", "WOLF", "WSC", "XPO", "ZI",
	"ZM", "ZS",
})

func toSet(tickers []string) map[string]struct{} {
	m := make(map[string]struct{}, len(tickers))
	for _, t := range tickers {
		m[t] = struct{}{}
	}
	return m
}

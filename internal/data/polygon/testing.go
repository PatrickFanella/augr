package polygon

// SetBaseURLForTest sets the baseURL on a Client. Intended for use by tests
// in other packages that need to point the client at an httptest.Server.
func SetBaseURLForTest(c *Client, baseURL string) {
	c.baseURL = baseURL
}

// SnapshotBarForTest constructs a SnapshotBar with the given values.
func SnapshotBarForTest(open, high, low, closePrice, volume, vwap float64) SnapshotBar {
	return SnapshotBar{
		Open:   open,
		High:   high,
		Low:    low,
		Close:  closePrice,
		Volume: volume,
		VWAP:   vwap,
	}
}

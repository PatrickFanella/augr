package edgar

import "testing"

func TestParse13FInformationTable(t *testing.T) {
	t.Parallel()
	xmlBody := []byte(`<?xml version="1.0"?><informationTable xmlns="http://www.sec.gov/edgar/document/thirteenf/informationtable">
  <infoTable><nameOfIssuer>Example Corp</nameOfIssuer><titleOfClass>COM</titleOfClass><cusip>123456789</cusip><value>1,250</value><shrsOrPrnAmt><sshPrnamt>5000</sshPrnamt><sshPrnamtType>SH</sshPrnamtType></shrsOrPrnAmt><investmentDiscretion>SOLE</investmentDiscretion><votingAuthority><Sole>5000</Sole><Shared>0</Shared><None>0</None></votingAuthority></infoTable>
  <infoTable><nameOfIssuer>Smaller Inc</nameOfIssuer><titleOfClass>CL A</titleOfClass><cusip>987654321</cusip><figi>BBG000TEST</figi><value>250</value><shrsOrPrnAmt><sshPrnamt>100</sshPrnamt><sshPrnamtType>SH</sshPrnamtType></shrsOrPrnAmt><putCall>PUT</putCall><votingAuthority><Sole>0</Sole><Shared>0</Shared><None>100</None></votingAuthority></infoTable>
</informationTable>`)

	holdings, err := Parse13FInformationTable(xmlBody)
	if err != nil {
		t.Fatalf("Parse13FInformationTable() error = %v", err)
	}
	if len(holdings) != 2 {
		t.Fatalf("len(holdings) = %d, want 2", len(holdings))
	}
	if got := holdings[0].DisclosedValue; got != 1_250_000 {
		t.Fatalf("DisclosedValue = %v, want 1250000", got)
	}
	if holdings[0].CUSIP != "123456789" || holdings[0].SharesOrPrincipal != 5000 {
		t.Fatalf("first holding = %+v", holdings[0])
	}
	if holdings[1].PutCall != "PUT" || holdings[1].FIGI != "BBG000TEST" {
		t.Fatalf("second holding = %+v", holdings[1])
	}
}

func TestParse13FInformationTableRejectsMissingIdentity(t *testing.T) {
	t.Parallel()
	_, err := Parse13FInformationTable([]byte(`<informationTable><infoTable><nameOfIssuer></nameOfIssuer><cusip></cusip><value>1</value><shrsOrPrnAmt><sshPrnamt>1</sshPrnamt></shrsOrPrnAmt></infoTable></informationTable>`))
	if err == nil {
		t.Fatal("Parse13FInformationTable() error = nil")
	}
}

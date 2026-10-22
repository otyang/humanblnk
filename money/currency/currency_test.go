package currency

import (
	"encoding/csv"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestListMatchesTheCSV catches a list.go not regenerated after a change to
// currencies.csv.
func TestListMatchesTheCSV(t *testing.T) {
	file, err := os.Open("currencies.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = 4
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 || !slices.Equal(rows[0], []string{"code", "name", "kind", "decimals"}) {
		t.Fatalf("currencies.csv starts with %v, want the row code,name,kind,decimals and at least one currency", rows[:min(len(rows), 1)])
	}
	rows = rows[1:]
	if len(rows) != len(list) {
		t.Fatalf("list has %d currencies, currencies.csv %d: run go generate ./money/currency", len(list), len(rows))
	}
	for i, row := range rows {
		decimals, err := strconv.Atoi(row[3])
		if err != nil {
			t.Fatalf("currencies.csv row %d: decimals %q: %v", i+2, row[3], err)
		}
		want := Currency{code: row[0], name: row[1], kind: Kind(row[2]), decimals: int32(decimals)}
		if list[i] != want {
			t.Errorf("list[%d] = %+v, currencies.csv has %+v: run go generate ./money/currency", i, list[i], want)
		}
	}
}

func TestFiatCodesAreISO4217(t *testing.T) {
	raw, err := os.ReadFile("testdata/iso4217.txt")
	if err != nil {
		t.Fatal(err)
	}
	iso := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			iso[line] = true
		}
	}
	for _, c := range All(Fiat) {
		if !iso[c.Code()] {
			t.Errorf("%s is listed as fiat but is not an ISO 4217 code", c.Code())
		}
	}
}

func TestDecimals(t *testing.T) {
	for code, want := range map[string]int32{
		USD: 2, EUR: 2, NGN: 2, JPY: 0, KRW: 0, XOF: 0, KWD: 3, BHD: 3,
		BTC: 8, ETH: 18, SOL: 9, USDT: 6, USDC: 6,
	} {
		if got := MustGet(code).Decimals(); got != want {
			t.Errorf("%s decimals = %d, want %d", code, got, want)
		}
	}
}

func TestGet(t *testing.T) {
	if c, ok := Get(" usd "); !ok || c.Code() != USD || c.Kind() != Fiat || c.Name() != "US Dollar" {
		t.Errorf(`Get(" usd ") = %+v, %v`, c, ok)
	}
	for _, code := range []string{"", "XYZ", "XAU", "US", "DOGE"} {
		if c, ok := Get(code); ok {
			t.Errorf("Get(%q) = %+v, want none", code, c)
		}
	}
}

func TestLookup(t *testing.T) {
	if c, ok := Lookup(USD); !ok || c.Code() != USD {
		t.Errorf("Lookup(USD) = %+v, %v", c, ok)
	}
	for _, code := range []string{"usd", "Usd", " USD", "USD ", "", "XYZ"} {
		if c, ok := Lookup(code); ok {
			t.Errorf("Lookup(%q) = %+v, want none", code, c)
		}
	}
}

func TestMustGetPanicsOnAnUnknownCode(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustGet(XYZ) did not panic")
		}
	}()
	MustGet("XYZ")
}

func TestAll(t *testing.T) {
	all, fiat, crypto := All(), All(Fiat), All(Crypto)
	if len(all) != len(list) || len(fiat)+len(crypto) != len(all) {
		t.Fatalf("All() = %d, fiat %d + crypto %d", len(all), len(fiat), len(crypto))
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].Code() >= all[i].Code() {
			t.Fatalf("All() not ordered by code: %s before %s", all[i-1].Code(), all[i].Code())
		}
	}
	for _, c := range crypto {
		if c.Kind() != Crypto {
			t.Errorf("All(Crypto) has %s, a %s currency", c.Code(), c.Kind())
		}
	}
}

func TestValid(t *testing.T) {
	if !MustGet(USD).Valid() {
		t.Error("USD is not valid")
	}
	if (Currency{}).Valid() || (Currency{code: USD, decimals: 3}).Valid() {
		t.Error("a zero or altered Currency is valid")
	}
}

func TestAdd(t *testing.T) {
	t.Cleanup(func() {
		mu.Lock()
		delete(byCode, "HUMANBLNK")
		mu.Unlock()
	})
	c, err := Add(Spec{Code: "HUMANBLNK", Name: "HumanBlnk Point", Kind: Crypto, Decimals: 2})
	if err != nil {
		t.Fatalf("Add(HUMANBLNK) = %v", err)
	}
	if got, ok := Get(" humanblnk "); !ok || got != c || !c.Valid() || c.Decimals() != 2 {
		t.Errorf("Get(humanblnk) = %v, %t after Add", got, ok)
	}
	if !slices.Contains(All(Crypto), c) {
		t.Error("All(Crypto) does not have HUMANBLNK")
	}
	if _, err := Add(Spec{Code: "HUMANBLNK", Name: "HumanBlnk Point", Kind: Crypto, Decimals: 4}); !errors.Is(err, ErrExists) {
		t.Errorf("Add(HUMANBLNK) again = %v, want ErrExists", err)
	}
	if _, err := Add(Spec{Code: USD, Name: "US Dollar", Kind: Fiat}); !errors.Is(err, ErrExists) || MustGet(USD).Decimals() != 2 {
		t.Errorf("Add(USD) = %v, and USD has %d decimals", err, MustGet(USD).Decimals())
	}
	for _, bad := range []Spec{
		{"humanblnk", "HumanBlnk", Crypto, 2},
		{"N", "HumanBlnk", Crypto, 2},
		{"1HUMANBLNK", "HumanBlnk", Crypto, 2},
		{"HUMANBLNK2", "", Crypto, 2},
		{"HUMANBLNK2", "HumanBlnk", "points", 2},
		{"HUMANBLNK2", "HumanBlnk", Crypto, 19},
		{"HUMANBLNK2", "HumanBlnk", Crypto, -1},
	} {
		if _, err := Add(bad); err == nil {
			t.Errorf("Add(%+v) succeeded", bad)
		}
	}
}

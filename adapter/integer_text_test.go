package adapter

import (
	"strings"
	"sync"
	"testing"
)

func TestIntegerTextBoundsAndRadices(t *testing.T) {
	c := NewContext()
	defer Close(c)
	must := checked(t)
	for _, tc := range []struct {
		width uint64
		text  string
		radix int
		want  string
	}{
		{128, "340282366920938463463374607431768211455", 10, "i128 -1"},
		{128, "-170141183460469231731687303715884105728", 10, "i128 -170141183460469231731687303715884105728"},
		{128, "+10000000000000000", 16, "i128 18446744073709551616"},
		{65, "3777777777777777777777", 8, "i65 -1"},
		{8, "fF", 16, "i8 -1"},
		{8, "-10000000", 2, "i8 -128"},
		{8, "-0", 10, "i8 0"},
		{65536, strings.Repeat("1", 65536), 2, "i65536 -1"},
	} {
		ty := must(Primitive(c, 1, tc.width))
		value := must(IntegerText(ty, tc.text, tc.radix))
		printed, err := Print(value)
		if err != "" || printed != tc.want {
			t.Fatalf("width=%d base=%d: got %q, %q; want %q", tc.width, tc.radix, printed, err, tc.want)
		}
	}
	ty := must(Primitive(c, 1, 8))
	for _, tc := range []struct {
		text  string
		radix int
	}{
		{"", 10}, {"+", 10}, {"-", 10}, {"--1", 10}, {"0x1", 16}, {"1_0", 10}, {" 1", 10}, {"1\x00", 10}, {"١", 10},
		{"2", 2}, {"8", 8}, {"a", 10}, {"g", 16}, {"1", 0}, {"1", 36}, {"256", 10}, {"-129", 10},
		{strings.Repeat("0", 65537), 10}, {"+" + strings.Repeat("0", 65537), 10},
	} {
		if _, err := IntegerText(ty, tc.text, tc.radix); !strings.HasPrefix(err, "argument:") {
			t.Fatalf("accepted invalid integer text (length %d) in base %d: %s", len(tc.text), tc.radix, err)
		}
	}
	if _, err := IntegerText(must(Primitive(c, 2, 0)), "1", 10); err == "" {
		t.Fatal("accepted floating type")
	}
	if _, err := IntegerText(c, "1", 10); err == "" {
		t.Fatal("accepted context")
	}
	i1 := must(Primitive(c, 1, 1))
	if equal, err := HandleEqual(must(IntegerText(i1, "-1", 10)), must(IntegerText(i1, "1", 10))); err != "" || !equal {
		t.Fatal("i1 signed and unsigned identity", err)
	}
}

func TestIntegerTextContextLifetimeAndConcurrentClose(t *testing.T) {
	c := NewContext()
	must := checked(t)
	ty := must(Primitive(c, 1, 128))
	module := must(NewModule(c, "integer"))
	value := must(IntegerText(ty, "18446744073709551616", 10))
	if err := Close(module); err != "" {
		t.Fatal(err)
	}
	if IsClosed(value) {
		t.Fatal("module closure invalidated context constant")
	}
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Go(func() {
			for i := 0; i < 50; i++ {
				if _, err := IntegerText(ty, "18446744073709551616", 10); err != "" && !strings.HasPrefix(err, "closed:") {
					t.Error(err)
				}
			}
		})
	}
	wg.Go(func() {
		if err := Close(c); err != "" {
			t.Error(err)
		}
	})
	wg.Wait()
	if _, err := IntegerText(ty, "1", 10); !strings.HasPrefix(err, "closed:") {
		t.Fatal("closed type accepted", err)
	}
	if !IsClosed(value) {
		t.Fatal("constant survived context closure")
	}
}

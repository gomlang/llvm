package adapter

import (
	"strings"
	"sync"
	"testing"
)

func TestAggregateConstructionValidatesBeforeMutation(t *testing.T) {
	ctx := NewContext()
	defer Close(ctx)
	must := checked(t)
	module := must(Parse(ctx, "define i64 @f({i64, [2 x i64]} %a, i64 %x, <2 x i64> %v) { ret i64 0 }\ndefine i64 @g(i64 %x) { ret i64 %x }", false))
	fn := must(Function(module, "f", nil, false))
	block := readIR(t, fn, 1)[0]
	ret := readIR(t, block, 2)[0]
	b := must(NewBuilder(ctx))
	a := must(Parameter(fn, 0))
	x := must(Parameter(fn, 1))
	if _, err := ExtractValue(b, a, 0, ""); !strings.HasPrefix(err, "argument:") {
		t.Fatal("unpositioned builder", err)
	}
	if err := PositionBefore(b, ret); err != "" {
		t.Fatal(err)
	}
	array := must(ExtractValue(b, a, 1, "array"))
	updated := must(InsertValue(b, array, x, 1, "updated"))
	parent := must(InsertValue(b, a, updated, 1, "parent"))
	field := must(ExtractValue(b, parent, 0, "field"))
	if equal, err := TypeEqual(must(ValueType(field)), must(ValueType(x))); !equal || err != "" {
		t.Fatal("field type", err)
	}
	integer := must(Primitive(ctx, 1, 64))
	empty := must(Constant(must(Composite(ctx, 0, []Handle{integer}, 0, false)), 3, 0, 0))
	emptyStruct := must(Constant(must(Composite(ctx, 1, nil, 0, false)), 3, 0, 0))
	vector := must(Parameter(fn, 2))
	g := must(Function(module, "g", nil, false))
	foreign := must(Parameter(g, 0))
	otherContext := NewContext()
	defer Close(otherContext)
	otherValue := must(Constant(must(Primitive(otherContext, 1, 64)), 3, 0, 0))
	otherModule := must(Parse(ctx, "define {i64, [2 x i64]} @h({i64, [2 x i64]} %a) { ret {i64, [2 x i64]} %a }", false))
	otherAggregate := must(Parameter(must(Function(otherModule, "h", nil, false)), 0))
	before, _ := Print(module)
	calls := []func() (Handle, string){
		func() (Handle, string) { return ExtractValue(b, a, -1, "") },
		func() (Handle, string) { return ExtractValue(b, a, 2, "") },
		func() (Handle, string) { return ExtractValue(b, a, 4294967296, "") },
		func() (Handle, string) { return ExtractValue(b, array, 2, "") },
		func() (Handle, string) { return ExtractValue(b, empty, 0, "") },
		func() (Handle, string) { return ExtractValue(b, emptyStruct, 0, "") },
		func() (Handle, string) { return ExtractValue(b, vector, 0, "") },
		func() (Handle, string) { return ExtractValue(b, x, 0, "") },
		func() (Handle, string) { return ExtractValue(b, integer, 0, "") },
		func() (Handle, string) { return ExtractValue(b, otherAggregate, 0, "") },
		func() (Handle, string) { return InsertValue(b, a, x, 1, "") },
		func() (Handle, string) { return InsertValue(b, array, x, 2, "") },
		func() (Handle, string) { return InsertValue(b, a, foreign, 0, "") },
		func() (Handle, string) { return InsertValue(b, a, otherValue, 0, "") },
		func() (Handle, string) { return InsertValue(b, a, integer, 0, "") },
		func() (Handle, string) { return InsertValue(b, a, x, 0, "bad\x00name") },
		func() (Handle, string) { return ExtractValue(b, a, 0, "bad\x00name") },
	}
	for index, call := range calls {
		if _, err := call(); !strings.HasPrefix(err, "argument:") {
			t.Fatalf("rejection %d: %q", index, err)
		}
	}
	after, _ := Print(module)
	if before != after {
		t.Fatal("rejected aggregate operation mutated IR")
	}
	if err := Verify(module); err != "" {
		t.Fatal(err)
	}
	if err := Position(b, block); err != "" {
		t.Fatal(err)
	}
	if _, err := ExtractValue(b, a, 0, ""); !strings.HasPrefix(err, "argument:") {
		t.Fatal("terminated block", err)
	}
}

func TestAggregateConstantFoldingRetainsActualOwnership(t *testing.T) {
	ctx := NewContext()
	defer Close(ctx)
	must := checked(t)
	module := must(Parse(ctx, "@global = global i64 0\ndefine {ptr, i64} @f() { ret {ptr, i64} {ptr @global, i64 7} }", false))
	fn := must(Function(module, "f", nil, false))
	ret := readIR(t, readIR(t, fn, 1)[0], 2)[0]
	aggregate := readIR(t, ret, 5)[0]
	b := must(NewBuilder(ctx))
	if err := PositionBefore(b, ret); err != "" {
		t.Fatal(err)
	}
	pointer := must(ExtractValue(b, aggregate, 0, "pointer"))
	integer := must(ExtractValue(b, aggregate, 1, "integer"))
	zero := must(Constant(must(ValueType(aggregate)), 3, 0, 0))
	updated := must(InsertValue(b, zero, pointer, 0, "updated"))
	pure := must(InsertValue(b, zero, integer, 1, "pure"))
	if err := Close(module); err != "" {
		t.Fatal(err)
	}
	for _, value := range []Handle{integer, pure} {
		if _, err := Print(value); err != "" {
			t.Fatal("pure folded constant escaped module lifetime", err)
		}
	}
	for _, value := range []Handle{pointer, updated} {
		if _, err := Print(value); !strings.HasPrefix(err, "closed:") {
			t.Fatal("module-referenced constant lost ownership", err)
		}
	}
}

func TestAggregateOperationsSynchronizeWithContextClose(t *testing.T) {
	ctx := NewContext()
	must := checked(t)
	module := must(Parse(ctx, "define i64 @f({i64} %a, i64 %x) { ret i64 %x }", false))
	fn := must(Function(module, "f", nil, false))
	ret := readIR(t, readIR(t, fn, 1)[0], 2)[0]
	b := must(NewBuilder(ctx))
	if err := PositionBefore(b, ret); err != "" {
		t.Fatal(err)
	}
	a := must(Parameter(fn, 0))
	x := must(Parameter(fn, 1))
	var workers, ready sync.WaitGroup
	start := make(chan struct{})
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		ready.Add(1)
		go func() {
			defer workers.Done()
			_, err := ExtractValue(b, a, 0, "")
			if err != "" {
				t.Error(err)
			}
			ready.Done()
			<-start
			for iteration := 0; iteration < 50; iteration++ {
				_, e1 := ExtractValue(b, a, 0, "")
				_, e2 := InsertValue(b, a, x, 0, "")
				for _, err := range []string{e1, e2} {
					if err != "" && !strings.HasPrefix(err, "closed:") {
						t.Error(err)
					}
				}
			}
		}()
	}
	ready.Wait()
	close(start)
	if err := Close(ctx); err != "" {
		t.Fatal(err)
	}
	workers.Wait()
	if _, err := InsertValue(b, a, x, 0, ""); !strings.HasPrefix(err, "closed:") {
		t.Fatal(err)
	}
}

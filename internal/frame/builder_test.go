package frame

import "testing"

// EstimateValueSize is the accounting Builder.AppendRow uses, so decoders that plan rows
// with it agree with the builder exactly.
func TestEstimateValueSizeMatchesBuilder(t *testing.T) {
	str := mustField(t, "s", TypeString)
	num := mustField(t, "n", TypeNumber)
	b, err := NewBuilder("", Limits{}, str, num)
	if err != nil {
		t.Fatal(err)
	}
	var want int64
	for _, row := range [][]any{{"hello", int64(1)}, {nil, 2.5}, {"", nil}} {
		if err := b.AppendRow(row...); err != nil {
			t.Fatal(err)
		}
		for _, v := range row {
			want += int64(EstimateValueSize(v))
		}
	}
	if b.Bytes() != want {
		t.Fatalf("builder counted %d bytes, EstimateValueSize sums to %d", b.Bytes(), want)
	}
}

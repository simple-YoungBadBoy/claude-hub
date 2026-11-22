package term

import "testing"

func TestAcquireCapsTerminals(t *testing.T) {
	var rel []func()
	for i := 0; i < MaxConns; i++ {
		r, err := Acquire()
		if err != nil {
			t.Fatalf("slot %d: %v", i, err)
		}
		rel = append(rel, r)
	}
	if _, err := Acquire(); err != ErrTooMany {
		t.Fatalf("want ErrTooMany, got %v", err)
	}
	rel[0]()
	rel[0]() // releasing twice must not free two slots
	if _, err := Acquire(); err != nil {
		t.Fatalf("slot after release: %v", err)
	}
	if _, err := Acquire(); err != ErrTooMany {
		t.Fatalf("double release freed an extra slot: %v", err)
	}
}

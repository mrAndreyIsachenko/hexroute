package connectivitytrace

import "testing"

// The catalogue is exercised end to end by connectivitysoak, which injects
// every trace and checks what each one made the read model do. What that
// cannot see is the identity the evidence chain binds results to: a qualifi-
// cation record names a fault and carries a trace digest, and those two have
// to stay in one-to-one correspondence or the chain records evidence about a
// fault that some other trace produced.

func TestEveryFaultHasADistinctTrace(t *testing.T) {
	traces, err := Canonical()
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	if len(traces) != len(Faults()) {
		t.Fatalf("%d traces for %d faults", len(traces), len(Faults()))
	}

	byDigest := make(map[string]Fault, len(traces))
	byFault := make(map[Fault]string, len(traces))
	for _, trace := range traces {
		digest, err := trace.Digest()
		if err != nil {
			t.Fatalf("digest %s: %v", trace.Fault, err)
		}
		if digest == "" {
			t.Fatalf("%s digested to nothing", trace.Fault)
		}
		if other, taken := byDigest[digest]; taken {
			t.Fatalf("%s and %s share a digest, so evidence naming one "+
				"cannot be told from evidence naming the other",
				other, trace.Fault)
		}
		if _, twice := byFault[trace.Fault]; twice {
			t.Fatalf("%s appears in the catalogue twice", trace.Fault)
		}
		byDigest[digest] = trace.Fault
		byFault[trace.Fault] = digest
	}

	for _, fault := range Faults() {
		if _, ok := byFault[fault]; !ok {
			t.Fatalf("%s is declared and the catalogue does not carry it", fault)
		}
	}
}

// A digest that is not reproducible would fail verification at random, and
// the finding would read as damaged evidence rather than as an unstable
// encoder.
func TestATraceDigestsTheSameEveryTime(t *testing.T) {
	for _, fault := range Faults() {
		t.Run(string(fault), func(t *testing.T) {
			first, err := For(fault)
			if err != nil {
				t.Fatalf("for: %v", err)
			}
			firstDigest, err := first.Digest()
			if err != nil {
				t.Fatalf("digest: %v", err)
			}
			for attempt := 0; attempt < 8; attempt++ {
				again, err := For(fault)
				if err != nil {
					t.Fatalf("for: %v", err)
				}
				digest, err := again.Digest()
				if err != nil {
					t.Fatalf("digest: %v", err)
				}
				if digest != firstDigest {
					t.Fatalf("%s digested to %s then %s",
						fault, firstDigest, digest)
				}
			}
		})
	}
}

// A trace that asserted nothing would inject a fault and accept whatever
// happened, which is the shape of a soak that passes without observing.
func TestEveryTraceAssertsSomething(t *testing.T) {
	traces, err := Canonical()
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	for _, trace := range traces {
		if len(trace.Steps) == 0 {
			t.Fatalf("%s injects nothing", trace.Fault)
		}
		if trace.Expectation.Assert == (Assertion{}) {
			t.Fatalf("%s asserts nothing about what it injects", trace.Fault)
		}
		if trace.Expectation.Visible == "" {
			t.Fatalf("%s names no condition the read model must report; a "+
				"fault that produces a correct-looking snapshot has been "+
				"hidden rather than survived", trace.Fault)
		}
		// The field exists to be false. A trace expecting a guessed healthy
		// outcome would make every other number in the run meaningless.
		if trace.Expectation.GuessedHealthy {
			t.Fatalf("%s expects the read model to guess healthy", trace.Fault)
		}
	}
}

package validate

import "testing"

// Security finding #2: PathParam must reject separators, or a path param
// can reach a sibling endpoint under a valid token.
func TestPathParamRejectsSeparators(t *testing.T) {
	for _, v := range []string{"42/secrets", `42\secrets`, "42;v=1"} {
		if err := PathParam("id", v); err == nil {
			t.Errorf("PathParam accepted %q", v)
		}
	}
}

func TestPathParamAcceptsOrdinaryIDs(t *testing.T) {
	for _, v := range []string{"42", "abc-123", "a_b.c"} {
		if err := PathParam("id", v); err != nil {
			t.Errorf("PathParam rejected ordinary id %q: %v", v, err)
		}
	}
}

package handlers

import "testing"

func TestEtagCorresponde(t *testing.T) {
	const etag = `"abc"`
	casos := []struct {
		header string
		quer   bool
	}{
		{`"abc"`, true},
		{`W/"abc"`, true},
		{`"x", "abc"`, true},
		{`"x" , W/"abc" , "y"`, true},
		{`*`, true},
		{`"x"`, false},
		{`"abcd"`, false},
		{``, false},
		{` , `, false},
	}
	for _, c := range casos {
		if got := etagCorresponde(c.header, etag); got != c.quer {
			t.Errorf("etagCorresponde(%q) = %v, want %v", c.header, got, c.quer)
		}
	}
}

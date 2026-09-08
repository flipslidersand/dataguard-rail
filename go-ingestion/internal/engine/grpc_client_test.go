package engine

import "testing"

func TestIsLoopbackAddr(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"[::1]:50051", true},
		{"127.0.0.1:50051", true},
		{"localhost:50051", true},
		{"localhost", true},
		{"0.0.0.0:50051", false},
		{"192.168.1.10:50051", false},
		{"engine.internal:50051", false},
	}
	for _, c := range cases {
		if got := isLoopbackAddr(c.addr); got != c.want {
			t.Errorf("isLoopbackAddr(%q) = %v, want %v", c.addr, got, c.want)
		}
	}
}

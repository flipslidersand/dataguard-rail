package main

import "testing"

func TestCheckAPIKeyRequired(t *testing.T) {
	cases := []struct {
		name      string
		apiKey    string
		allowEnv  string
		wantError bool
	}{
		{"api key set", "secret", "", false},
		{"api key set, allow env ignored", "secret", "1", false},
		{"empty api key without opt-in refused", "", "", true},
		{"empty api key with wrong opt-in value refused", "", "true", true},
		{"empty api key with opt-in allowed", "", "1", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkAPIKeyRequired(c.apiKey, c.allowEnv)
			if c.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !c.wantError && err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}

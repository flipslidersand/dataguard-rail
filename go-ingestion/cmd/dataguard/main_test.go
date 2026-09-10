package main

import "testing"

func TestCheckAPIKeyRequirement(t *testing.T) {
	cases := []struct {
		name             string
		apiKey           string
		allowInsecureEnv string
		wantErr          bool
	}{
		{"api key set", "secret", "", false},
		{"api key set, env ignored", "secret", "1", false},
		{"no api key, no opt-in", "", "", true},
		{"no api key, wrong opt-in value", "", "true", true},
		{"no api key, explicit opt-in", "", "1", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkAPIKeyRequirement(c.apiKey, c.allowInsecureEnv)
			if (err != nil) != c.wantErr {
				t.Errorf("checkAPIKeyRequirement(%q, %q) error = %v, wantErr %v", c.apiKey, c.allowInsecureEnv, err, c.wantErr)
			}
		})
	}
}

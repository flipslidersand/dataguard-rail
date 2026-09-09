package server

import "testing"

func TestValidateSQLPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"queries/report.sql", true},
		{"report.sql", true},
		{"", false},
		{"/etc/dataguard/secrets/internal.sql", false},
		{"../secrets/internal.sql", false},
		{"queries/../../etc/passwd.sql", false},
		{"report.txt", false},
		{"report", false},
	}
	for _, c := range cases {
		if got := validateSQLPath(c.path); got != c.want {
			t.Errorf("validateSQLPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

package main

import "testing"

func TestSameNameAcceptsPorkbunNameForms(t *testing.T) {
	tests := []struct {
		got, relative string
		want          bool
	}{
		{domain, "", true},
		{"", "", true},
		{"www." + domain, "www", true},
		{"www", "www", true},
		{"other." + domain, "www", false},
	}
	for _, tt := range tests {
		if got := sameName(tt.got, tt.relative); got != tt.want {
			t.Errorf("sameName(%q, %q) = %v; want %v", tt.got, tt.relative, got, tt.want)
		}
	}
}

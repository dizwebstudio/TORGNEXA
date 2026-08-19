package api

import "testing"

func TestHostFromBaseURL(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		want    string
		wantErr bool
	}{
		{name: "empty falls back to connector default", baseURL: "", want: ""},
		{name: "https URL yields bare hostname", baseURL: "https://gateway.example.com", want: "gateway.example.com"},
		{name: "https URL with path/port yields only the hostname", baseURL: "https://gateway.example.com:8443/v1", want: "gateway.example.com"},
		{name: "non-https scheme is rejected", baseURL: "http://gateway.example.com", wantErr: true},
		{name: "bare host without scheme is rejected", baseURL: "gateway.example.com", wantErr: true},
		{name: "malformed URL is rejected", baseURL: "https://[::1", wantErr: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := hostFromBaseURL(testCase.baseURL)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", testCase.baseURL)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", testCase.baseURL, err)
			}
			if got != testCase.want {
				t.Fatalf("hostFromBaseURL(%q) = %q, want %q", testCase.baseURL, got, testCase.want)
			}
		})
	}
}

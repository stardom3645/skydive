package server

import "testing"

func TestValidateMoldAPIInputRequiresCredentialPair(t *testing.T) {
	tests := []struct {
		name      string
		request   moldCredentialsRequest
		wantError bool
	}{
		{name: "pair", request: moldCredentialsRequest{APIKey: "api", SecretKey: "secret"}},
		{name: "missing api key", request: moldCredentialsRequest{SecretKey: "secret"}, wantError: true},
		{name: "missing secret key", request: moldCredentialsRequest{APIKey: "api"}, wantError: true},
		{name: "db only", request: moldCredentialsRequest{DBPassword: "password"}, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateMoldAPIInput(test.request)
			if (err != nil) != test.wantError {
				t.Fatalf("validateMoldAPIInput() error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}

package common

import "testing"

func TestMoldUIURLFromServerProperties(t *testing.T) {
	tests := []struct {
		name       string
		properties string
		want       string
	}{
		{
			name: "current HTTP listener",
			properties: `http.enable=true
http.port=8080
https.enable=false
https.port=8443`,
			want: "http://ccvm:8080/client/#/accountuser?username=admin",
		},
		{
			name: "future HTTPS listener",
			properties: `http.enable=false
http.port=8080
https.enable=true
https.port=443`,
			want: "https://ccvm/client/#/accountuser?username=admin",
		},
		{
			name: "custom HTTPS listener",
			properties: `https.enable=true
https.port=9443`,
			want: "https://ccvm:9443/client/#/accountuser?username=admin",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := moldUIURLFromProperties(test.properties, "http://ccvm:8080/client/api")
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("moldUIURLFromProperties() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestMoldUIURLFallsBackToAPIEndpoint(t *testing.T) {
	got, err := moldUIURLFromAPIEndpoint("http://ccvm:18080/client/api")
	if err != nil {
		t.Fatal(err)
	}
	want := "http://ccvm:18080/client/#/accountuser?username=admin"
	if got != want {
		t.Fatalf("moldUIURLFromAPIEndpoint() = %q, want %q", got, want)
	}
}

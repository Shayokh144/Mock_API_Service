package proxy

import "testing"

func TestJoinPath(t *testing.T) {
	cases := []struct {
		base string
		req  string
		want string
	}{
		{base: "", req: "/v1/users", want: "/v1/users"},
		{base: "/", req: "/v1/users", want: "/v1/users"},
		{base: "/prefix", req: "/v1/users", want: "/prefix/v1/users"},
		{base: "/prefix/", req: "/v1/users", want: "/prefix/v1/users"},
		{base: "", req: "/v1/users/", want: "/v1/users/"},
		{base: "/prefix", req: "/v1/users/", want: "/prefix/v1/users/"},
	}
	for _, tc := range cases {
		got := joinPath(tc.base, tc.req)
		if got != tc.want {
			t.Fatalf("joinPath(%q, %q) = %q, want %q", tc.base, tc.req, got, tc.want)
		}
	}
}

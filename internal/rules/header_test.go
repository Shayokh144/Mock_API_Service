package rules

import (
	"net/http"
	"strings"
	"testing"
)

func TestHeaderOperatorsAndJSONIndex(t *testing.T) {
	store := NewMemory()
	if _, err := store.Create(Draft{
		Name: "Auth", Enabled: true, Method: "POST", PathPattern: "/v1/**", StatusCode: 200,
		Match: MatchSpec{Headers: mustConds(t, "header", "Authorization:absent\nAccept-Language:prefix=th\nX-Request-Id:contains=okya")},
	}); err != nil {
		t.Fatal(err)
	}
	header := http.Header{}
	header.Set("Accept-Language", "th-TH")
	header.Set("X-Request-Id", "okya-1")
	if _, ok := store.MatchHTTP(http.MethodPost, "/v1/oauth/token", "", "", header, nil); !ok {
		t.Fatal("expected header match after a ** path")
	}
	header.Set("Authorization", "Bearer x")
	if _, ok := store.MatchHTTP(http.MethodPost, "/v1/oauth/token", "", "", header, nil); ok {
		t.Fatal("authorization should miss")
	}
	if _, err := store.Create(Draft{
		Enabled: true, Method: "GET", PathPattern: "/v1/id*", StatusCode: 200,
	}); err == nil || !strings.Contains(err.Error(), "whole segment") {
		t.Fatalf("mid-segment star = %v", err)
	}
	if _, err := store.Create(Draft{
		Enabled: true, Method: "GET", PathPattern: "/v1/a/**/b", StatusCode: 200,
	}); err == nil {
		t.Fatal("** in the middle was accepted")
	}
	if _, err := store.Create(Draft{
		Name: "Mail", Enabled: true, Method: "POST", PathPattern: "/v1/mail", StatusCode: 200,
		Match: MatchSpec{Body: mustConds(t, "body", `user.email:regex=@okya\.test$`)},
	}); err != nil {
		t.Fatal(err)
	}
	bodyHeader := http.Header{"Content-Type": []string{"application/json"}}
	if _, ok := store.MatchHTTP(http.MethodPost, "/v1/mail", "", "", bodyHeader, []byte(`{"user":{"email":"a@okya.test"}}`)); !ok {
		t.Fatal("regex body missed")
	}
	if _, ok := store.MatchHTTP(http.MethodPost, "/v1/mail", "", "", bodyHeader, []byte(`{"user":{"email":"a@other.test"}}`)); ok {
		t.Fatal("regex body should miss")
	}
	if _, err := store.Create(Draft{
		Name: "Qty", Enabled: true, Method: "POST", PathPattern: "/v1/qty", StatusCode: 200,
		Match: MatchSpec{Body: mustConds(t, "body", "items.0.qty=2")},
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.MatchHTTP(http.MethodPost, "/v1/qty", "", "", bodyHeader, []byte(`{"items":[{"qty":2}]}`)); !ok {
		t.Fatal("array index missed")
	}
	if _, ok := store.MatchHTTP(http.MethodPost, "/v1/qty", "", "", bodyHeader, []byte(`{"items":[]}`)); ok {
		t.Fatal("out of range should miss")
	}
	if _, err := ParseConditions("X-Test:regex=(", "header"); err == nil || !strings.Contains(err.Error(), "regex") {
		t.Fatalf("bad regex = %v", err)
	}
}

func TestDoubleStarMatchesZeroOrMore(t *testing.T) {
	if !matchPattern("/v1/**", "/v1") || !matchPattern("/v1/**", "/v1/oauth/token") {
		t.Fatal("** should match the prefix and anything after it")
	}
	if matchPattern("/v1/**", "/v2/oauth/token") {
		t.Fatal("** matched a different prefix")
	}
}

func mustConds(t *testing.T, label, text string) []Condition {
	t.Helper()
	list, err := ParseConditions(text, label)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

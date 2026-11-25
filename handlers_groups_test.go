package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSkipGroupsEndpoints(t *testing.T) {
	s := makeTestServer(t)
	const token = "tok-groups"
	if _, err := s.db.Exec(
		`INSERT INTO users (id, name, token, connected) VALUES ($1,$2,$3,$4)`,
		"u-groups", "tester", token, 0); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	call := func(method, body string) (int, map[string]interface{}) {
		req := httptest.NewRequest(method, "/session/groups", strings.NewReader(body))
		req.Header.Set("token", token)
		rr := httptest.NewRecorder()
		s.router.ServeHTTP(rr, req)
		var out map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &out)
		return rr.Code, out
	}

	if code, out := call(http.MethodGet, ""); code != http.StatusOK || out["data"].(map[string]interface{})["skip_groups"] != false {
		t.Fatalf("default should be false; got %d %v", code, out)
	}
	if skipGroupsEnabled(s.db, "u-groups") {
		t.Fatalf("skipGroupsEnabled should be false by default")
	}

	if code, _ := call(http.MethodPost, `{}`); code != http.StatusBadRequest {
		t.Fatalf("missing skip_groups should be rejected; got %d", code)
	}

	if code, out := call(http.MethodPost, `{"skip_groups":true}`); code != http.StatusOK {
		t.Fatalf("enable failed: %d %v", code, out)
	}
	if code, out := call(http.MethodGet, ""); code != http.StatusOK || out["data"].(map[string]interface{})["skip_groups"] != true {
		t.Fatalf("should be true after enabling; got %d %v", code, out)
	}
	if !skipGroupsEnabled(s.db, "u-groups") {
		t.Fatalf("skipGroupsEnabled should be true after enabling")
	}
	if !skipGroupsEnabled(s.db, "missing-user") {
		t.Fatalf("unknown user should fail closed (skip groups)")
	}
}

func TestAddUserWithSkipGroups(t *testing.T) {
	s := makeTestServer(t)
	body := `{"name":"clinica","token":"tok-new","skipGroups":true}`
	req := httptest.NewRequest(http.MethodPost, "/admin/users", strings.NewReader(body))
	req.Header.Set("Authorization", *adminToken)
	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK && rr.Code != http.StatusCreated {
		t.Fatalf("create user failed: %d %s", rr.Code, rr.Body.String())
	}

	var skip bool
	if err := s.db.Get(&skip, "SELECT COALESCE(skip_groups, false) FROM users WHERE token = $1", "tok-new"); err != nil {
		t.Fatalf("read skip_groups: %v", err)
	}
	if !skip {
		t.Fatalf("user created with skipGroups=true should have skip_groups set")
	}
}

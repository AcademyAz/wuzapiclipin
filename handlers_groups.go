package main

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jmoiron/sqlx"
)

// skipGroupsEnabled reports whether the user asked to ignore group messages.
// On a database error it returns true so a misconfiguration never lets group
// messages reach a bot that asked not to receive them.
func skipGroupsEnabled(db *sqlx.DB, userID string) bool {
	var skip bool
	if err := db.Get(&skip, "SELECT COALESCE(skip_groups, false) FROM users WHERE id = $1", userID); err != nil {
		return true
	}
	return skip
}

// SetSkipGroups turns group message delivery on or off for this session.
// When enabled, messages from groups (and broadcast lists) are not sent to the
// webhook, not saved to history and their media is not downloaded.
func (s *server) SetSkipGroups() http.HandlerFunc {
	type skipGroupsStruct struct {
		SkipGroups *bool `json:"skip_groups"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		txtid := r.Context().Value("userinfo").(Values).Get("Id")

		var t skipGroupsStruct
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil || t.SkipGroups == nil {
			s.Respond(w, r, http.StatusBadRequest, errors.New("could not decode payload: skip_groups (boolean) is required"))
			return
		}

		if _, err := s.db.Exec("UPDATE users SET skip_groups = $1 WHERE id = $2", *t.SkipGroups, txtid); err != nil {
			s.Respond(w, r, http.StatusInternalServerError, errors.New("failed to save skip_groups configuration"))
			return
		}

		responseJson, _ := json.Marshal(map[string]interface{}{
			"Details":     "Group messages configuration saved",
			"skip_groups": *t.SkipGroups,
		})
		s.Respond(w, r, http.StatusOK, string(responseJson))
	}
}

// GetSkipGroups returns whether group messages are ignored for this session.
func (s *server) GetSkipGroups() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		txtid := r.Context().Value("userinfo").(Values).Get("Id")

		var skip bool
		if err := s.db.Get(&skip, "SELECT COALESCE(skip_groups, false) FROM users WHERE id = $1", txtid); err != nil {
			s.Respond(w, r, http.StatusInternalServerError, errors.New("failed to read skip_groups configuration"))
			return
		}

		responseJson, _ := json.Marshal(map[string]interface{}{"skip_groups": skip})
		s.Respond(w, r, http.StatusOK, string(responseJson))
	}
}

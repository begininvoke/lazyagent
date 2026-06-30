package cursor

import (
	"database/sql"
	"os"
)

// ReadAuth returns Cursor's locally-stored OAuth access token and Stripe
// membership type, both read from state.vscdb's ItemTable.
//
// ok is false (with a nil error) when Cursor simply isn't installed or the user
// isn't logged in — no database, or no cursorAuth/accessToken entry. Real failures
// (DB open/query errors) are returned as err so callers can surface them. The
// membership type may be empty even when a token is present; callers fall back to
// an explicit override in that case.
func ReadAuth() (accessToken, membershipType string, ok bool, err error) {
	path := stateDBPath()
	if path == "" {
		return "", "", false, nil
	}
	if _, statErr := os.Stat(path); statErr != nil {
		return "", "", false, nil
	}

	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return "", "", false, err
	}
	defer db.Close()

	accessToken = readItemValue(db, "cursorAuth/accessToken")
	if accessToken == "" {
		return "", "", false, nil
	}
	membershipType = readItemValue(db, "cursorAuth/stripeMembershipType")
	return accessToken, membershipType, true, nil
}

// readItemValue fetches a single ItemTable value, returning "" when the key is
// absent or unreadable (the caller treats both as "not set").
func readItemValue(db *sql.DB, key string) string {
	var v string
	if err := db.QueryRow("SELECT value FROM ItemTable WHERE key = ?", key).Scan(&v); err != nil {
		return ""
	}
	return v
}

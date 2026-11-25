package migrations_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// 0095 drops three unread columns and two covered indexes, and indexes the
// foreign keys a deleted message or conversation checks.
func TestAccountChatCleanup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	for table, column := range map[string]string{
		"credentials": "password_hash", "console_dashboard_sessions": "expires_at", "feedback": "replied_by",
	} {
		var n int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
WHERE table_schema = 'public' AND table_name = @table AND column_name = @column`,
			pgx.StrictNamedArgs{"table": table, "column": column}).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s.%s: %d %v", table, column, n, err)
		}
	}
	for index, want := range map[string]bool{
		"identities_account_id": false, "sessions_account_id": false,
		"identities_account_kind": true, "sessions_account_request_key": true,
		"messages_reply_to": true, "reports_conversation": true, "tickets_first_message": true,
		"reply_samples_answered": true, "notifications_bumped": true,
	} {
		var got bool
		if err := db.QueryRow(ctx, `SELECT to_regclass(@index) IS NOT NULL`,
			pgx.StrictNamedArgs{"index": index}).Scan(&got); err != nil || got != want {
			t.Errorf("%s: %v %v, want %v", index, got, err, want)
		}
	}
}

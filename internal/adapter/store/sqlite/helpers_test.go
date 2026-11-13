package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
)

const testAcc = model.AccountID("wa-test")

func newTestApp(t *testing.T) *AppStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	st, err := OpenApp(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	return st.(*AppStore)
}

func newTestAccount(t *testing.T) *AccountStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "account.db")
	st, _, err := OpenAccount(testAcc, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	return st.(*AccountStore)
}

// saveTestAccount crea una cuenta en app.db (las identidades, campañas y programados la referencian).
func saveTestAccount(t *testing.T, s *AppStore, id model.AccountID) {
	t.Helper()
	require.NoError(t, s.SaveAccount(context.Background(), model.Account{
		ID: id, Channel: channelOf(id), Label: string(id), Color: "#25D366", Enabled: true, CreatedAt: 1,
	}))
}

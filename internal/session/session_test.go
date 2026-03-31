package session

import (
	"testing"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/stretchr/testify/require"
)

func TestServiceSetModePersists(t *testing.T) {
	t.Parallel()

	conn, err := db.Connect(t.Context(), t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, conn.Close())
	})

	service := NewService(db.New(conn), conn)
	sess, err := service.Create(t.Context(), "Test Session")
	require.NoError(t, err)
	require.Equal(t, SessionModeBuild, sess.Mode)

	updated, err := service.SetMode(t.Context(), sess.ID, SessionModePlan)
	require.NoError(t, err)
	require.Equal(t, SessionModePlan, updated.Mode)

	reloaded, err := service.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, SessionModePlan, reloaded.Mode)
}

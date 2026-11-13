package capability

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSetJSONRoundTrip(t *testing.T) {
	in := Set{
		SendText: true, SendImage: true, React: true, MaxReactionsPerUser: 1,
		Edit: true, EditWindow: 15 * time.Minute,
		DeleteForEveryone: true, DeleteWindow: 48 * time.Hour,
		MaxTextLen: 4096, MaxFileSize: 2 << 30,
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(b, &raw))
	require.EqualValues(t, 15*60*1000, raw["editWindowMs"])
	require.EqualValues(t, 48*3600*1000, raw["deleteWindowMs"])

	var out Set
	require.NoError(t, json.Unmarshal(b, &out))
	require.Equal(t, in, out)
}

func TestSetJSONZeroWindows(t *testing.T) {
	b, err := json.Marshal(Set{})
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(b, &raw))
	require.EqualValues(t, 0, raw["editWindowMs"])
	var out Set
	require.NoError(t, json.Unmarshal(b, &out))
	require.Equal(t, Set{}, out)
}

func TestReadOnlyTurnsOffSending(t *testing.T) {
	s := Set{SendText: true, SendImage: true, Reply: true, React: true, Edit: true, DeleteForEveryone: true, VotePoll: true, Groups: true}
	r := s.ReadOnly()
	require.False(t, r.SendText || r.SendImage || r.Reply || r.React || r.Edit || r.DeleteForEveryone || r.VotePoll)
	require.True(t, r.Groups)
	require.True(t, s.SendText, "el original no cambia")
}

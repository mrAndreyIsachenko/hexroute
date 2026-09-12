package rootdaemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelplan"
)

// tunnelStateSchema names what the file holds, so a reader of the directory can
// tell it from the heartbeat beside it.
const tunnelStateSchema = "hexroute.tunnel-state.v1"

type storedTunnelState struct {
	Schema string           `json:"schema"`
	State  tunnelplan.State `json:"state"`
}

// tunnelStateStore keeps what one cycle leaves for the next.
//
// Three of the six causes are comparisons against the previous cycle, so they
// need memory that survives a restart. It is not policy, not an observation and
// not part of the operator snapshot; folding it into any of those would make an
// unrelated schema change every time a cause is added.
type tunnelStateStore struct {
	path string
}

func newTunnelStateStore(path string) (*tunnelStateStore, error) {
	if path == "" {
		return nil, nil
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("%w: tunnel state path", ErrInvalidConfig)
	}
	return &tunnelStateStore{path: path}, nil
}

// Load answers what the previous cycle left.
//
// A store that cannot be read answers an unknown state rather than an error. A
// cycle that refused to decide because it could not remember would be a cycle
// that stops observing over a file, and the causes that need no memory — the
// process, the wake gap, the payload path — are the ones that matter most.
func (store *tunnelStateStore) Load() tunnelplan.State {
	if store == nil {
		return tunnelplan.State{}
	}
	encoded, err := os.ReadFile(store.path)
	if err != nil {
		return tunnelplan.State{}
	}
	var stored storedTunnelState
	if err := json.Unmarshal(encoded, &stored); err != nil ||
		stored.Schema != tunnelStateSchema {
		return tunnelplan.State{}
	}
	return stored.State
}

// Save records what this cycle leaves for the next one.
func (store *tunnelStateStore) Save(state tunnelplan.State) error {
	if store == nil {
		return nil
	}
	encoded, err := json.Marshal(storedTunnelState{
		Schema: tunnelStateSchema, State: state,
	})
	if err != nil {
		return fmt.Errorf("%w: encode tunnel state: %v", ErrInvalidConfig, err)
	}
	temporary := store.path + ".pending"
	if err := os.WriteFile(temporary, encoded, 0o600); err != nil {
		return fmt.Errorf("%w: stage tunnel state: %v", ErrInvalidConfig, err)
	}
	if err := os.Rename(temporary, store.path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("%w: publish tunnel state: %v", ErrInvalidConfig, err)
	}
	return nil
}

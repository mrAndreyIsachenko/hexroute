package tunnelexec

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// DefaultNoticePath is where the runtime that gives the tunnel up leaves word of
// it, for the runtime that can tell the operator.
//
// It is a file because there is no other way across: the two daemons' only
// connection runs the other way — the user daemon publishes facts into the root
// socket and the root answers nothing back. Inventing a reverse channel for one
// sentence would be a larger change than the sentence deserves.
const DefaultNoticePath = "/Library/Application Support/Hexroute/observe-root/state/tunnel-handback.json"

// Notice is word that this runtime gave the tunnel up.
//
// It carries no destination, no interface and no identity: a reason from a
// closed list, when it happened, and whether the tunnel actually went back. It
// is readable by the operator's own session, which is what makes it useful and
// is also why it says nothing else.
type Notice struct {
	Schema string `json:"schema"`
	Reason string `json:"reason"`
	At     string `json:"at"`
	Given  bool   `json:"given"`
}

// NoticeSchema is the shape above, so a reader can refuse anything else.
const NoticeSchema = "hexroute.tunnel-handback.v1"

var ErrInvalidNotice = errors.New("invalid tunnel handback notice")

// WriteNotice leaves word, replacing whatever was there.
//
// The last one is what matters: a handback that has not been told about is the
// current state of the machine, and an operator reading this wants to know what
// it is now rather than what it has ever been. The record of every one is the
// archive's.
func WriteNotice(path string, notice Notice) error {
	if path == "" || notice.Reason == "" || notice.At == "" {
		return ErrInvalidNotice
	}
	notice.Schema = NoticeSchema
	content, err := json.Marshal(notice)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary := path + ".writing"
	// Readable by the operator's session, which is the whole point of it, and
	// writable by nothing else.
	if err := os.WriteFile(temporary, content, 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// ReadNotice reads the last word left, or reports that there is none.
func ReadNotice(path string) (Notice, bool, error) {
	if path == "" {
		return Notice{}, false, ErrInvalidNotice
	}
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Notice{}, false, nil
	}
	if err != nil {
		return Notice{}, false, err
	}
	var notice Notice
	if err := json.Unmarshal(content, &notice); err != nil {
		return Notice{}, false, ErrInvalidNotice
	}
	if notice.Schema != NoticeSchema || notice.Reason == "" {
		return Notice{}, false, ErrInvalidNotice
	}
	if _, err := time.Parse(time.RFC3339Nano, notice.At); err != nil {
		return Notice{}, false, ErrInvalidNotice
	}
	switch Handback(notice.Reason) {
	case HandbackRate, HandbackGrantLapsed, HandbackDeadTunnel:
	default:
		return Notice{}, false, ErrInvalidNotice
	}
	return notice, true, nil
}

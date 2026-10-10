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
	// TakenBack says this runtime has the tunnel again, so the state the rest
	// of this notice describes has ended.
	//
	// It is here rather than in a file of its own because this notice already
	// is the state: a handback that has not been superseded is what the machine
	// is now. And the word is not removed when the state ends, because the one
	// case where that matters is the one this notice exists for — nobody was at
	// the machine to read it.
	//
	// A reader that does not know this field ignores it, which is what makes
	// the field safe to add: the decoder here has never been strict, and an
	// older build announces the handback exactly as it did before.
	TakenBack bool `json:"taken_back,omitempty"`
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
	// A handback being written is one that has just happened, so whatever the
	// caller passed, the tunnel has not been taken back since. Clearing it here
	// rather than trusting the caller is what keeps the field from surviving
	// into the next handback, where it would say a condition had ended before
	// it began.
	notice.TakenBack = false
	return writeNotice(path, notice)
}

// MarkNoticeTakenBack records that the state this notice describes has ended.
//
// It reads and rewrites rather than replacing, because the reason, the time and
// whether the tunnel went back are the account of what happened and are not
// this runtime's to restate. No notice is not a failure: there is nothing to
// end, which is the ordinary case.
func MarkNoticeTakenBack(path string) (bool, error) {
	notice, found, err := ReadNotice(path)
	if err != nil || !found {
		return false, err
	}
	if notice.TakenBack {
		return false, nil
	}
	notice.TakenBack = true
	if err := writeNotice(path, notice); err != nil {
		return false, err
	}
	return true, nil
}

func writeNotice(path string, notice Notice) error {
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
	if !Handback(notice.Reason).Valid() {
		return Notice{}, false, ErrInvalidNotice
	}
	return notice, true, nil
}

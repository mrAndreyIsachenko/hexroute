package tunnelexec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func noticePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "tunnel-handback.json")
}

func leaveWord(t *testing.T, path string, reason Handback) {
	t.Helper()
	if err := WriteNotice(path, Notice{
		Reason: string(reason),
		At:     time.Now().UTC().Format(time.RFC3339Nano),
		Given:  true,
	}); err != nil {
		t.Fatalf("write notice: %v", err)
	}
}

// The end is recorded where the state ends, and the account of what happened is
// not restated while recording it.
func TestTakingTheTunnelBackMarksTheWordWithoutRewritingIt(t *testing.T) {
	path := noticePath(t)
	leaveWord(t, path, HandbackGrantLapsed)
	before, _, err := ReadNotice(path)
	if err != nil {
		t.Fatalf("read notice: %v", err)
	}

	marked, err := MarkNoticeTakenBack(path)
	if err != nil {
		t.Fatalf("mark: %v", err)
	}
	if !marked {
		t.Fatal("marking an unmarked notice reported no change")
	}

	after, found, err := ReadNotice(path)
	if err != nil || !found {
		t.Fatalf("read notice: %v, found=%v", err, found)
	}
	if !after.TakenBack {
		t.Fatal("the notice does not say the tunnel was taken back")
	}
	if after.Reason != before.Reason || after.At != before.At ||
		after.Given != before.Given {
		t.Fatalf("marking rewrote the account: %+v became %+v", before, after)
	}
}

// Marking twice is not two endings. The cycle reads this on every pass while it
// owns the tunnel, so the second pass must find nothing to record.
func TestMarkingTwiceRecordsOneEnding(t *testing.T) {
	path := noticePath(t)
	leaveWord(t, path, HandbackRate)
	if marked, err := MarkNoticeTakenBack(path); err != nil || !marked {
		t.Fatalf("first mark: %v, marked=%v", err, marked)
	}
	marked, err := MarkNoticeTakenBack(path)
	if err != nil {
		t.Fatalf("second mark: %v", err)
	}
	if marked {
		t.Fatal("a notice already marked reported a second ending")
	}
}

// No word is not a failure. A runtime that has never handed the tunnel back
// owns it in the ordinary way, and that is most cycles.
func TestMarkingWithNoWordLeftIsNotAnEnding(t *testing.T) {
	marked, err := MarkNoticeTakenBack(noticePath(t))
	if err != nil {
		t.Fatalf("mark with no notice: %v", err)
	}
	if marked {
		t.Fatal("marking reported an ending where no notice exists")
	}
}

// The field must not survive into the next handback, where it would say a
// condition ended before it began.
func TestANewHandbackIsNotAlreadyEnded(t *testing.T) {
	path := noticePath(t)
	leaveWord(t, path, HandbackRate)
	if _, err := MarkNoticeTakenBack(path); err != nil {
		t.Fatalf("mark: %v", err)
	}

	// Including when a caller passes the field set, which is the mistake the
	// clearing in WriteNotice exists to make impossible.
	if err := WriteNotice(path, Notice{
		Reason:    string(HandbackDeadTunnel),
		At:        time.Now().UTC().Format(time.RFC3339Nano),
		Given:     false,
		TakenBack: true,
	}); err != nil {
		t.Fatalf("write notice: %v", err)
	}
	notice, found, err := ReadNotice(path)
	if err != nil || !found {
		t.Fatalf("read notice: %v, found=%v", err, found)
	}
	if notice.TakenBack {
		t.Fatal("a freshly written handback says it has already been taken back")
	}
	if notice.Reason != string(HandbackDeadTunnel) {
		t.Fatalf("reason is %q, want %q", notice.Reason, HandbackDeadTunnel)
	}
}

// A build that does not know the field announces the handback as it always did.
// This is what makes adding the field safe, and rollback is a requirement here
// rather than a hope.
func TestABuildThatDoesNotKnowTheFieldStillReadsTheWord(t *testing.T) {
	path := noticePath(t)
	leaveWord(t, path, HandbackGrantLapsed)
	if _, err := MarkNoticeTakenBack(path); err != nil {
		t.Fatalf("mark: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	// The shape an older build decodes into: the field it has never heard of is
	// simply absent from it.
	var older struct {
		Schema string `json:"schema"`
		Reason string `json:"reason"`
		At     string `json:"at"`
		Given  bool   `json:"given"`
	}
	if err := json.Unmarshal(content, &older); err != nil {
		t.Fatalf("an older build could not decode the notice: %v", err)
	}
	if older.Schema != NoticeSchema || older.Reason != string(HandbackGrantLapsed) {
		t.Fatalf("an older build reads %+v", older)
	}
	if _, err := time.Parse(time.RFC3339Nano, older.At); err != nil {
		t.Fatalf("an older build cannot parse the time: %v", err)
	}
}

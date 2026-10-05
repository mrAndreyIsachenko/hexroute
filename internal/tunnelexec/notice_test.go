package tunnelexec

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Word of a handback survives being written and read, and says only what it
// should: a reason from the closed list, when, and whether the tunnel went back.
func TestANoticeCarriesTheHandbackAndNothingElse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tunnel-handback.json")
	at := time.Date(2026, 10, 2, 3, 4, 5, 0, time.UTC).Format(time.RFC3339Nano)
	if err := WriteNotice(path, Notice{Reason: string(HandbackGrantLapsed), At: at, Given: true}); err != nil {
		t.Fatalf("WriteNotice: %v", err)
	}
	notice, found, err := ReadNotice(path)
	if err != nil || !found {
		t.Fatalf("ReadNotice: %v, found=%v", err, found)
	}
	if notice.Reason != string(HandbackGrantLapsed) || notice.At != at || !notice.Given {
		t.Fatalf("notice = %+v", notice)
	}
	// It is readable by the operator's own session: that is what it is for.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o044 == 0 {
		t.Fatalf("the notice is not readable by anyone but its writer: %v", info.Mode())
	}
	// The last word replaces the one before it.
	later := at
	if err := WriteNotice(path, Notice{Reason: string(HandbackRate), At: later}); err != nil {
		t.Fatal(err)
	}
	notice, _, err = ReadNotice(path)
	if err != nil {
		t.Fatal(err)
	}
	if notice.Reason != string(HandbackRate) || notice.Given {
		t.Fatalf("the later notice reads as %+v", notice)
	}
}

// Nothing to read is not an error: a runtime that has never given the tunnel up
// leaves no word, and a reader must not treat that as a fault.
func TestNoNoticeIsNotAFailure(t *testing.T) {
	notice, found, err := ReadNotice(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil || found {
		t.Fatalf("an absent notice gave %+v, found=%v, err=%v", notice, found, err)
	}
}

// Anything else is refused rather than read as a handback.
func TestANoticeThatCannotBeTrustedIsRefused(t *testing.T) {
	at := time.Now().UTC().Format(time.RFC3339Nano)
	for name, content := range map[string]string{
		"not json":                 `{`,
		"another schema":           `{"schema":"something.else","reason":"rate_bound","at":"` + at + `"}`,
		"no reason":                `{"schema":"` + NoticeSchema + `","at":"` + at + `"}`,
		"a reason nobody wrote":    `{"schema":"` + NoticeSchema + `","reason":"because","at":"` + at + `"}`,
		"no moment":                `{"schema":"` + NoticeSchema + `","reason":"rate_bound"}`,
		"a moment nobody can read": `{"schema":"` + NoticeSchema + `","reason":"rate_bound","at":"yesterday"}`,
	} {
		path := filepath.Join(t.TempDir(), "tunnel-handback.json")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, found, err := ReadNotice(path); err == nil || found {
			t.Fatalf("%s was read as a handback", name)
		}
	}
	// And a notice with nothing to say is not written in the first place.
	path := filepath.Join(t.TempDir(), "tunnel-handback.json")
	if err := WriteNotice(path, Notice{At: at}); err == nil {
		t.Fatal("a notice with no reason was written")
	}
	if err := WriteNotice(path, Notice{Reason: string(HandbackRate)}); err == nil {
		t.Fatal("a notice with no moment was written")
	}
}

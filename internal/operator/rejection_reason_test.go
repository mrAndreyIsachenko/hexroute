package operator

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
)

// ipcErrorNames lists what the IPC package can refuse with, read out of its
// source rather than copied here.
//
// Copying the list is what let the gap open: errors were added to the protocol
// over time and the classifier was not, so each new one silently became
// "malformed_request". Derived, the test covers the error somebody adds next
// year without anybody remembering to.
func ipcErrorNames(t *testing.T) []string {
	t.Helper()
	fileSet := token.NewFileSet()
	pkg, err := parser.ParseDir(fileSet, filepath.Join("..", "ipc"), func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0)
	for _, p := range pkg {
		for _, file := range p.Files {
			for _, decl := range file.Decls {
				general, ok := decl.(*ast.GenDecl)
				if !ok || general.Tok != token.VAR {
					continue
				}
				for _, spec := range general.Specs {
					value, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, name := range value.Names {
						if strings.HasPrefix(name.Name, "Err") && name.IsExported() {
							names = append(names, name.Name)
						}
					}
				}
			}
		}
	}
	return names
}

// errorsByName maps the names above to the values, so the test asserts on the
// real errors rather than on strings.
func errorsByName() map[string]error {
	return map[string]error{
		"ErrUnauthorizedPeer":           ipc.ErrUnauthorizedPeer,
		"ErrFrameTooLarge":              ipc.ErrFrameTooLarge,
		"ErrMalformedFrame":             ipc.ErrMalformedFrame,
		"ErrPeerSilent":                 ipc.ErrPeerSilent,
		"ErrUnsupportedVersion":         ipc.ErrUnsupportedVersion,
		"ErrInvalidRequestID":           ipc.ErrInvalidRequestID,
		"ErrUnknownAction":              ipc.ErrUnknownAction,
		"ErrInvalidTarget":              ipc.ErrInvalidTarget,
		"ErrInvalidPolicyMessage":       ipc.ErrInvalidPolicyMessage,
		"ErrInvalidReconcilerMessage":   ipc.ErrInvalidReconcilerMessage,
		"ErrInvalidConnectivityMessage": ipc.ErrInvalidConnectivityMessage,
		"ErrConnectivityDomain":         ipc.ErrConnectivityDomain,
		"ErrInvalidSocketPath":          ipc.ErrInvalidSocketPath,
		"ErrSocketInUse":                ipc.ErrSocketInUse,
		"ErrResponseMismatch":           ipc.ErrResponseMismatch,
	}
}

// TestEveryIPCRefusalNamesItsCheck is the property, not a list of cases.
//
// A rejection logged as "malformed_request" says the request was wrong and
// nothing about which check refused it. On this machine every connectivity
// publication was refused that way for twenty-two hours, and four separate
// wrong diagnoses were built on the silence.
func TestEveryIPCRefusalNamesItsCheck(t *testing.T) {
	known := errorsByName()
	for _, name := range ipcErrorNames(t) {
		err, mapped := known[name]
		if !mapped {
			t.Fatalf("ipc.%s exists and this test does not know it; "+
				"add it here and give it a reason, or the next one is invisible too", name)
		}
		// Socket-lifecycle errors never reach a peer's request, so they are not
		// rejections and carry no reason of their own.
		if name == "ErrInvalidSocketPath" || name == "ErrSocketInUse" || name == "ErrResponseMismatch" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			if reasonFor(t, err) == logging.ReasonMalformedRequest {
				t.Fatalf("ipc.%s is reported as %q, which names no check",
					name, logging.ReasonMalformedRequest)
			}
		})
	}
}

func reasonFor(t *testing.T, err error) logging.Reason {
	t.Helper()
	var out bytes.Buffer
	logger, logErr := logging.New(&out, logging.ComponentDaemon)
	if logErr != nil {
		t.Fatal(logErr)
	}
	reporter, reporterErr := NewRejectionLogger(logger)
	if reporterErr != nil {
		t.Fatal(reporterErr)
	}
	reporter.ReportIPCRejection(err)
	var record struct {
		Reason logging.Reason `json:"reason"`
	}
	if decodeErr := json.Unmarshal(out.Bytes(), &record); decodeErr != nil {
		t.Fatalf("the rejection did not log a readable record: %v (%q)", decodeErr, out.String())
	}
	return record.Reason
}

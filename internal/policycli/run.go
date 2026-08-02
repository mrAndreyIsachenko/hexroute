package policycli

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/buildinfo"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyapproval"
	"github.com/mrAndreyIsachenko/hexroute/internal/replay"
)

const (
	outputSchema        = "hexroute.policy-command.v1"
	compatibilitySchema = "hexroute.policy-compatibility.v1"
	maxInputFile        = policy.MaxCanonicalJSONSize
)

type commandOutput struct {
	Schema         string `json:"schema"`
	Command        string `json:"command"`
	ManifestSHA256 string `json:"manifest_sha256,omitempty"`
	ArtifactSHA256 string `json:"artifact_sha256,omitempty"`
}

type compatibilityFile struct {
	Schema string                        `json:"schema"`
	Root   policy.InstalledCompatibility `json:"root"`
	User   policy.InstalledCompatibility `json:"user"`
}

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }
func (values *stringList) Set(value string) error {
	if value == "" {
		return errors.New("empty value")
	}
	*values = append(*values, value)
	return nil
}

func Run(args []string, stdout, stderr io.Writer) int {
	if stdout == nil || stderr == nil {
		return 1
	}
	if len(args) == 1 && args[0] == "--check" {
		return 0
	}
	if len(args) == 1 && args[0] == "--version" {
		_, _ = fmt.Fprintf(stdout, "hexroute-policy version=%s commit=%s\n", buildinfo.Version, buildinfo.Commit)
		return 0
	}
	if len(args) == 0 {
		writeError(stderr, "usage")
		return 2
	}
	var err error
	switch args[0] {
	case "compile":
		err = runCompile(args[1:], stdout, false)
	case "rollback":
		err = runCompile(args[1:], stdout, true)
	case "diff":
		err = runDiff(args[1:], stdout)
	case "replay":
		err = runReplay(args[1:], stdout)
	case "sign":
		err = runSign(args[1:], stdout)
	default:
		writeError(stderr, "usage")
		return 2
	}
	if err != nil {
		writeError(stderr, "failed")
		return 1
	}
	return 0
}

func runCompile(args []string, stdout io.Writer, rollback bool) error {
	name := "compile"
	if rollback {
		name = "rollback"
	}
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	sourcePath := flags.String("source", "", "strict operator YAML source")
	currentPath := flags.String("current", "", "current candidate directory")
	outPath := flags.String("out", "", "new private output directory")
	compilerVersion := flags.String("compiler-version", "", "compiler version")
	compilerSHA := flags.String("compiler-sha256", "", "compiler digest")
	signerFingerprint := flags.String("signer-fingerprint", "", "pinned signer fingerprint")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *sourcePath == "" || *outPath == "" ||
		*compilerVersion == "" || *compilerSHA == "" || *signerFingerprint == "" ||
		(rollback && *currentPath == "") {
		return errors.New("invalid compile flags")
	}
	sourceFile, err := openRegular(*sourcePath, policy.MaxOperatorSourceSize)
	if err != nil {
		return err
	}
	defer sourceFile.Close()
	source, err := policy.DecodeOperatorSource(sourceFile)
	if err != nil {
		return err
	}
	var current *policy.EffectiveSnapshot
	if *currentPath != "" {
		bundle, err := loadBundle(*currentPath)
		if err != nil {
			return err
		}
		current = &bundle.Snapshot
	}
	candidate, err := policy.CompileBundle(
		source, policy.DefaultSafetyEnvelope(),
		policy.CompilerIdentity{Version: *compilerVersion, SHA256: *compilerSHA},
		*signerFingerprint, current,
	)
	if err != nil {
		return err
	}
	if err := writeBundle(*outPath, candidate); err != nil {
		return err
	}
	return writeOutput(stdout, commandOutput{Schema: outputSchema, Command: name, ManifestSHA256: candidate.ManifestSHA256})
}

func runDiff(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("diff", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	currentPath := flags.String("current", "", "current candidate directory")
	candidatePath := flags.String("candidate", "", "candidate directory")
	outPath := flags.String("out", "", "new private output directory")
	initial := flags.Bool("initial", false, "diff against no current policy")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *candidatePath == "" || *outPath == "" ||
		(*initial == (*currentPath != "")) {
		return errors.New("invalid diff flags")
	}
	candidate, err := loadBundle(*candidatePath)
	if err != nil {
		return err
	}
	var current *policy.EffectiveSnapshot
	if !*initial {
		bundle, err := loadBundle(*currentPath)
		if err != nil {
			return err
		}
		current = &bundle.Snapshot
	}
	report, err := policy.BuildSemanticDiff(current, candidate.Snapshot)
	if err != nil {
		return err
	}
	digest, encoded, err := policy.CanonicalSHA256(report)
	if err != nil {
		return err
	}
	if err := writeArtifacts(*outPath, map[string][]byte{"diff.json": encoded}); err != nil {
		return err
	}
	return writeOutput(stdout, commandOutput{Schema: outputSchema, Command: "diff", ArtifactSHA256: digest})
}

func runReplay(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("replay", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	candidatePath := flags.String("candidate", "", "candidate directory")
	casesPath := flags.String("cases", "", "synthetic and redacted JSONL cases")
	outPath := flags.String("out", "", "new private output directory")
	var tracePaths stringList
	flags.Var(&tracePaths, "root-trace", "root JSONL trace; repeatable")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *candidatePath == "" || *casesPath == "" || *outPath == "" {
		return errors.New("invalid replay flags")
	}
	candidate, err := loadBundle(*candidatePath)
	if err != nil {
		return err
	}
	casesFile, err := openRegular(*casesPath, replay.MaxPolicyCaseInput)
	if err != nil {
		return err
	}
	cases, err := replay.DecodePolicyCases(casesFile)
	_ = casesFile.Close()
	if err != nil {
		return err
	}
	traces := make([]replay.Trace, 0, len(tracePaths))
	for _, path := range tracePaths {
		traceFile, err := openRegular(path, replay.MaxTrace)
		if err != nil {
			return err
		}
		trace, decodeErr := replay.Decode(traceFile)
		_ = traceFile.Close()
		if decodeErr != nil {
			return decodeErr
		}
		traces = append(traces, trace)
	}
	report, err := replay.EvaluatePolicy(candidate.Snapshot, cases, traces)
	if err != nil {
		return err
	}
	digest, encoded, err := policy.CanonicalSHA256(report)
	if err != nil {
		return err
	}
	if err := writeArtifacts(*outPath, map[string][]byte{"replay.json": encoded}); err != nil {
		return err
	}
	if err := writeOutput(stdout, commandOutput{Schema: outputSchema, Command: "replay", ArtifactSHA256: digest}); err != nil {
		return err
	}
	return replay.RequirePolicyReplay(report)
}

func runSign(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("sign", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	currentPath := flags.String("current", "", "current candidate directory")
	candidatePath := flags.String("candidate", "", "candidate directory")
	diffPath := flags.String("diff", "", "canonical diff report")
	replayPath := flags.String("replay", "", "canonical replay report")
	compatibilityPath := flags.String("compatibility", "", "installed compatibility JSON")
	publicKeyPath := flags.String("public-key", "", "base64 Ed25519 public key")
	service := flags.String("keychain-service", "", "user-presence Keychain service")
	account := flags.String("keychain-account", "", "Keychain account")
	outPath := flags.String("out", "", "new private output directory")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *candidatePath == "" || *diffPath == "" ||
		*replayPath == "" || *compatibilityPath == "" || *publicKeyPath == "" ||
		*service == "" || *account == "" || *outPath == "" {
		return errors.New("invalid sign flags")
	}
	candidate, err := loadBundle(*candidatePath)
	if err != nil {
		return err
	}
	var current *policy.EffectiveSnapshot
	if *currentPath != "" {
		bundle, err := loadBundle(*currentPath)
		if err != nil {
			return err
		}
		current = &bundle.Snapshot
	}
	var diff policy.SemanticDiff
	if err := decodeRegularJSON(*diffPath, &diff); err != nil || diff.Validate() != nil {
		return errors.New("invalid diff")
	}
	var replayReport replay.PolicyReport
	if err := decodeRegularJSON(*replayPath, &replayReport); err != nil || replayReport.Validate() != nil {
		return errors.New("invalid replay")
	}
	var compatibility compatibilityFile
	if err := decodeRegularJSON(*compatibilityPath, &compatibility); err != nil || compatibility.Schema != compatibilitySchema {
		return errors.New("invalid compatibility")
	}
	publicKey, err := readPublicKey(*publicKeyPath)
	if err != nil {
		return err
	}
	signer, err := policyapproval.NewKeychainSigner(policyapproval.ExecKeychainRunner{}, policyapproval.KeychainConfig{
		Service: *service, Account: *account, PublicKey: publicKey,
		RequireUserPresence: true, PromptTimeout: 2 * time.Minute,
	})
	if err != nil {
		return err
	}
	review, approval, err := policyapproval.ApproveCandidate(
		candidate, current, diff, replayReport,
		policyapproval.InstalledDomains{Root: compatibility.Root, User: compatibility.User}, signer,
	)
	if err != nil {
		return err
	}
	reviewDigest, reviewJSON, err := policy.CanonicalSHA256(review)
	if err != nil {
		return err
	}
	_, approvalJSON, err := policy.CanonicalSHA256(approval)
	if err != nil {
		return err
	}
	if err := writeArtifacts(*outPath, map[string][]byte{"review.json": reviewJSON, "approval.json": approvalJSON}); err != nil {
		return err
	}
	return writeOutput(stdout, commandOutput{
		Schema: outputSchema, Command: "sign", ManifestSHA256: candidate.ManifestSHA256, ArtifactSHA256: reviewDigest,
	})
}

func loadBundle(directory string) (policy.CandidateBundle, error) {
	manifest, err := readRegular(filepath.Join(directory, "manifest.json"), maxInputFile)
	if err != nil {
		return policy.CandidateBundle{}, err
	}
	root, err := readRegular(filepath.Join(directory, "root.json"), maxInputFile)
	if err != nil {
		return policy.CandidateBundle{}, err
	}
	user, err := readRegular(filepath.Join(directory, "user.json"), maxInputFile)
	if err != nil {
		return policy.CandidateBundle{}, err
	}
	return policy.DecodeCandidateBundle(manifest, root, user)
}

func writeBundle(directory string, candidate policy.CandidateBundle) error {
	return writeArtifacts(directory, map[string][]byte{
		"manifest.json": candidate.ManifestJSON,
		"root.json":     candidate.RootJSON,
		"user.json":     candidate.UserJSON,
	})
}

func writeArtifacts(directory string, artifacts map[string][]byte) error {
	if directory == "" || len(artifacts) == 0 {
		return errors.New("invalid output")
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		return err
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(directory)
		}
	}()
	for name, content := range artifacts {
		if filepath.Base(name) != name || len(content) == 0 || len(content) > maxInputFile {
			return errors.New("invalid artifact")
		}
		path := filepath.Join(directory, name)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		if _, err := file.Write(content); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	success = true
	return nil
}

func openRegular(path string, maximum int64) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > maximum {
		return nil, errors.New("invalid input file")
	}
	return os.Open(path)
}

func readRegular(path string, maximum int64) ([]byte, error) {
	file, err := openRegular(path, maximum)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, maximum+1))
}

func decodeRegularJSON(path string, destination any) error {
	encoded, err := readRegular(path, maxInputFile)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("invalid JSON")
	}
	return nil
}

func readPublicKey(path string) (ed25519.PublicKey, error) {
	encoded, err := readRegular(path, 1024)
	if err != nil {
		return nil, err
	}
	encoded = bytes.TrimSpace(encoded)
	for _, encoding := range []*base64.Encoding{base64.RawStdEncoding, base64.StdEncoding} {
		decoded, err := encoding.DecodeString(string(encoded))
		if err == nil && len(decoded) == ed25519.PublicKeySize {
			return ed25519.PublicKey(decoded), nil
		}
	}
	return nil, errors.New("invalid public key")
}

func writeOutput(writer io.Writer, output commandOutput) error {
	return json.NewEncoder(writer).Encode(output)
}

func writeError(writer io.Writer, code string) {
	_, _ = fmt.Fprintf(writer, "hexroute-policy: %s\n", code)
}

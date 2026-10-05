package rootdaemon

import (
	"context"
	"errors"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelclaim"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelexec"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelplan"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelstop"
)

// executionTunnel is the tunnel as the rebuild needs it: a process this runtime
// stops, one it asks to be started, and the interface that process brings up.
//
// It starts nothing itself. Verifying a signed configuration version belongs to
// the delivery path, and the always-running daemons are kept off that path, so
// the start goes through the binary an operator runs for the handover — the same
// verification, the same pinned key, the same target. What this runtime does
// directly is end a process it is already watching.
type executionTunnel struct {
	// start runs the handover binary's start, and reports the process it
	// started.
	start func(ctx context.Context) (int, error)
	// stop ends a process. It is a function so a test can watch what would have
	// been signalled.
	stop    func(pid int) error
	network interface {
		TUNInterfaces(ctx context.Context) ([]observe.TUNInterface, error)
	}
	tunAddress netip.Addr
}

func (tunnel executionTunnel) Stop(_ context.Context, pid int) error {
	if tunnel.stop == nil || pid <= 0 {
		return errors.New("nothing to stop")
	}
	return tunnel.stop(pid)
}

func (tunnel executionTunnel) Start(ctx context.Context) (int, error) {
	if tunnel.start == nil {
		return 0, errors.New("nothing to start the tunnel with")
	}
	return tunnel.start(ctx)
}

func (tunnel executionTunnel) Interface(ctx context.Context) (string, error) {
	if tunnel.network == nil {
		return "", tunnelexec.ErrNoInterface
	}
	interfaces, err := tunnel.network.TUNInterfaces(ctx)
	if err != nil {
		return "", err
	}
	managed, err := observe.FindTUNByAddress(interfaces, tunnel.tunAddress)
	if err != nil {
		return "", tunnelexec.ErrNoInterface
	}
	return managed.Name, nil
}

// releaseThroughHandover gives the tunnel back through the binary an operator
// would run for it, as a giving-up rather than an exchange: this runtime has
// decided it should not hold the tunnel, so a release that took it back when
// nobody else raised one would undo the decision that started it.
func releaseThroughHandover(
	configPath string,
	execution *RuntimeTunnelExecution,
) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		return exec.CommandContext(ctx,
			execution.HandoverBinary, releaseArguments(configPath, execution)...).Run()
	}
}

// releaseArguments is what the runtime asks the handover binary for. It is its
// own function so that what this runtime asks for can be read without running
// anything: the difference between an exchange and a giving-up is one flag, and
// it is the difference between a machine that keeps its tunnel and one that is
// left without.
//
// It carries the delivery arguments although a giving-up does not restore.
// `release` builds its starter before it touches anything — deliberately, so a
// release that stopped this runtime's tunnel and then found it had nothing to
// start it again from cannot happen — and refuses without them. Measured four
// times on 2026-09-26: `HANDBACK reason=rate_bound given=False`, once per cycle
// while the bound held, because this runtime asked for a release with two flags
// out of six.
func releaseArguments(configPath string, execution *RuntimeTunnelExecution) []string {
	return []string{
		"--config", configPath,
		"--tunnel-version", execution.VersionPath,
		"--target-key", execution.TargetKey,
		"--sing-box", execution.Binary,
		"--content", execution.ContentPath,
		"--relinquish",
		"release",
	}
}

// startThroughHandover asks the binary that owns the delivery path to raise the
// tunnel, and reads the process it started.
func startThroughHandover(
	binary, configPath string,
	execution *RuntimeTunnelExecution,
) func(ctx context.Context) (int, error) {
	return func(ctx context.Context) (int, error) {
		command := exec.CommandContext(ctx, binary,
			"--config", configPath,
			"--tunnel-version", execution.VersionPath,
			"--target-key", execution.TargetKey,
			"--sing-box", execution.Binary,
			"--content", execution.ContentPath,
			"start-tunnel")
		output, err := command.Output()
		if err != nil {
			return 0, err
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(output)))
		if err != nil || pid <= 0 {
			return 0, errors.New("the tunnel start reported no process")
		}
		return pid, nil
	}
}

// executionRoutes creates a host route on an interface, and does nothing else.
//
// It is the first mutation of the routing table this runtime performs. It is
// deliberately the narrowest one that restores what a rebuild removed: a host
// route to one destination, scoped to one interface. It creates, and where the
// route is already there in another shape it changes that one — `add` refuses a
// destination that exists, and a tunnel that went away can leave its route
// behind on a name nothing answers to.
type executionRoutes struct {
	run func(ctx context.Context, name string, args ...string) error
}

func (routes executionRoutes) Restore(
	ctx context.Context,
	destination netip.Addr,
	iface string,
) error {
	if routes.run == nil || !destination.IsValid() || iface == "" {
		return errors.New("invalid route restore")
	}
	address := destination.String()
	if err := routes.run(ctx, "/sbin/route",
		"-n", "add", "-host", address, "-interface", iface); err == nil {
		return nil
	}
	return routes.run(ctx, "/sbin/route",
		"-n", "change", "-host", address, "-interface", iface)
}

// executionPayload asks the same question the cycle asks: did traffic traverse.
type executionPayload struct {
	observer PayloadObserver
	endpoint observe.PayloadEndpoint
}

func (payload executionPayload) Traversed(ctx context.Context) (bool, error) {
	if payload.observer == nil {
		return false, errors.New("no payload observer")
	}
	observation, err := payload.observer.Payload(ctx, payload.endpoint)
	if err != nil {
		return false, err
	}
	return observation.Traversed, nil
}

// commandRunner runs a command and reports whether it succeeded. It exists so a
// test can watch what would have been run without running it.
func commandRunner(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

// newExecutor builds what acts on a decision.
//
// The count of rebuilds lives beside what one cycle leaves for the next, in the
// same state directory: it is the same kind of fact — what this runtime knows
// about its own past — and a runtime installed without that directory keeps no
// count, which is a bound reached rather than permission.
func newExecutor(
	config RuntimeConfig,
	configPath string,
	network *observe.MacOSObserver,
	payload PayloadObserver,
	claims *tunnelclaim.Store,
	readiness EndpointObserver,
) (executor, error) {
	built := executor{
		claims:           claims,
		payload:          &payloadState{},
		payloadThreshold: payloadThreshold(config),
		notice:           tunnelexec.DefaultNoticePath,
	}
	if config.TunnelSupervision != nil && config.TunnelSupervision.Execution != nil {
		built.rate = tunnelexec.Rate{
			Path: config.TunnelSupervision.Execution.RebuildsPath,
			Now:  time.Now,
		}
	}
	rebuilder, err := newRebuilder(config, configPath, network, payload, readiness)
	if err != nil {
		return executor{}, err
	}
	built.rebuilder = rebuilder
	if config.TunnelSupervision != nil && config.TunnelSupervision.Execution != nil {
		built.release = releaseThroughHandover(
			configPath, config.TunnelSupervision.Execution)
	}
	return built, nil
}

// newRebuilder builds what performs a rebuild, or reports that this runtime
// cannot perform one. A runtime without the configuration for it decides as
// before and performs nothing.
func newRebuilder(
	config RuntimeConfig,
	configPath string,
	network *observe.MacOSObserver,
	payload PayloadObserver,
	readiness EndpointObserver,
) (*tunnelexec.Rebuilder, error) {
	if config.TunnelSupervision == nil || config.TunnelSupervision.Execution == nil {
		return nil, nil
	}
	execution := config.TunnelSupervision.Execution
	return &tunnelexec.Rebuilder{
		Tunnel: executionTunnel{
			start:      startThroughHandover(execution.HandoverBinary, configPath, execution),
			stop:       tunnelstop.Stop,
			network:    network,
			tunAddress: config.ManagedTUNAddress,
		},
		Routes:  executionRoutes{run: commandRunner},
		Payload: executionPayload{observer: payload, endpoint: config.TunnelSupervision.Payload},
		Link:    executionLink{readiness: readiness, endpoints: outerEndpoints(config)},
		Bound:   execution.Bound,
		Poll:    execution.Poll,
		Now:     time.Now,
		Sleep:   sleepFor,
	}, nil
}

// executionLink is the outer path as a rebuild needs it: whether any endpoint
// this runtime watches for it answers.
//
// It asks the same observer the cycle asks, so the two cannot disagree about
// what the outer path being there means. A runtime configured with no such
// endpoint cannot say the path is away and does not claim it: the rebuild then
// waits out its bound exactly as it did before.
type executionLink struct {
	readiness EndpointObserver
	endpoints []observe.Endpoint
}

func (link executionLink) Ready(ctx context.Context) (bool, error) {
	if link.readiness == nil || len(link.endpoints) == 0 {
		return true, nil
	}
	for _, endpoint := range link.endpoints {
		observation, err := link.readiness.Endpoint(ctx, endpoint)
		if err == nil && observation.Ready {
			return true, nil
		}
	}
	return false, nil
}

// outerEndpoints are the endpoints whose purpose is the outer path, which is the
// same set the cycle judges it by.
func outerEndpoints(config RuntimeConfig) []observe.Endpoint {
	endpoints := make([]observe.Endpoint, 0, len(config.Endpoints))
	for _, configured := range config.Endpoints {
		if configured.Purpose == PurposeOuterReady {
			endpoints = append(endpoints, configured.Endpoint)
		}
	}
	return endpoints
}

// executor is what a cycle needs to act on what it decided.
//
// A daemon without one decides exactly as it did for the seven days the rule was
// proved over, and records that it performed nothing. That is not a degraded
// mode: it is the mode this runtime was in while it earned the right to act.
type executor struct {
	rebuilder *tunnelexec.Rebuilder
	rate      tunnelexec.Rate
	claims    interface {
		Held() (tunnelclaim.Claim, bool, error)
	}
	// release gives the tunnel back, through the same transaction the operator
	// runs by hand. A runtime that decided it should not hold the tunnel is in
	// no position to invent a second way of letting go of it.
	release func(ctx context.Context) error
	payload *payloadState
	// notice is where word of a handback is left for the runtime that can tell
	// the operator. The two daemons' only connection runs the other way.
	notice string
	// payloadThreshold is how many consecutive failures with the outer path up
	// end this runtime's ownership. Zero is no threshold, and hands nothing
	// back: a runtime configured without supervision holds nothing to give up.
	payloadThreshold uint32
}

// perform reads the gates and, where they are open, rebuilds.
//
// It is called after the decision is recorded and reports what to record about
// the act. A cycle that decided to do nothing performs nothing and records
// nothing about it: there was no act to account for.
func (executor executor) perform(
	ctx context.Context,
	decided bool,
	suspended bool,
	// authorization is the grant's answer to this act, and nil is the cycle
	// that could not put the question. The two are different states and the
	// gate reports them apart; a bool would have made the second look like a
	// refusal, which is what it did.
	authorization *event.TunnelAuthorization,
	current tunnelexec.Current,
) (*event.TunnelExecution, int) {
	if !decided {
		return nil, 0
	}
	gates := tunnelexec.Gates{
		Owns:       executor.owns(),
		Suspended:  suspended,
		Asked:      authorization != nil,
		Authorized: authorization != nil && authorization.Allowed,
		RateAllows: executor.rateAllows(),
	}
	if blocked := gates.Blocked(); blocked != "" {
		return &event.TunnelExecution{Blocked: string(blocked)}, 0
	}
	if executor.rebuilder == nil {
		// Every gate open and nothing to perform with is a runtime that was
		// installed without what a rebuild needs. It is recorded as blocked
		// rather than as an act: nothing happened, and the record says which
		// gate — the rate, which is the one this runtime keeps on itself — did
		// not let it through.
		return &event.TunnelExecution{Blocked: string(tunnelexec.BlockedRate)}, 0
	}
	outcome, err := executor.rebuilder.Rebuild(ctx, current)
	if err != nil {
		return &event.TunnelExecution{Blocked: string(tunnelexec.BlockedRate)}, 0
	}
	// A rebuild counts toward the bound whether or not it worked. A runtime
	// that only counted the ones that worked would rebuild without end exactly
	// where rebuilding is not working.
	_ = executor.rate.Record(time.Now())
	restored := len(outcome.Restored)
	elapsed := outcome.Elapsed.Milliseconds()
	// The process it started is reported alongside the record, because the
	// cycle that watches for a loss has to know about a replacement this
	// runtime made. A rebuild whose process the next cycle read as a stranger
	// decided another rebuild: measured 2026-09-26, twice inside ninety
	// seconds, until the bound stopped it.
	return &event.TunnelExecution{
		Performed: true,
		Reason:    string(outcome.Reason),
		Routes:    &restored,
		ElapsedMS: &elapsed,
	}, outcome.StartedAt
}

// currentTunnel is what a rebuild replaces: the process the cycle saw, the
// interface it holds, and the destinations whose routes point at that interface.
//
// The routes are the configured ones the cycle already looked at. A host route
// to somewhere this runtime knows nothing about is not restored, because it was
// never read — what is restored is what was seen, which is the same rule the
// record is written under.
func currentTunnel(summary Summary) tunnelexec.Current {
	current := tunnelexec.Current{
		PID:       summary.Observed.Process.Process.PID,
		Interface: summary.Observed.ManagedTUN.Name,
	}
	if current.Interface == "" {
		return current
	}
	for _, route := range summary.Observed.Routes {
		if route.Interface == current.Interface && route.Destination.IsValid() {
			current.Routes = append(current.Routes, route.Destination)
		}
	}
	return current
}

// payloadState is what this runtime has seen of the tunnel carrying traffic.
//
// It counts only the cycles where the outer path was reachable, because a
// payload that fails with the outer path down says nothing about the tunnel —
// the runtime this one reproduces judges its own payload the same way. It lives
// in memory: a restart forgets, which delays a handback and never causes one.
type payloadState struct{ failures uint32 }

// observe takes what one cycle saw of the payload and answers the running count.
//
// A cycle that did not finish carries the last answer rather than one of its
// own, so it neither counts nor clears: a suspended machine does not probe, and
// treating what it carried as this cycle's reading would count a dark wake as
// evidence about the tunnel.
//
// A failure with the outer path down says nothing about the tunnel either, and
// is left where it was rather than cleared, so a machine that alternates
// between an unreachable outer path and a dead tunnel still reaches the
// threshold.
//
// A success does say something, whatever the outer path was doing: traffic
// traversed the tunnel, so the tunnel carries something. Leaving the count
// standing through it was a defect — measured 2026-09-26T16:34:32Z, this
// runtime gave the tunnel back naming a dead tunnel while the cycle that
// decided it recorded two failures against a threshold of three. The count had
// climbed across cycles whose payload passed and whose outer path read as down,
// each of which cleared the planner's own ground and not this.
func (state *payloadState) observe(grounds tunnelplan.Grounds, complete bool) uint32 {
	if state == nil {
		return 0
	}
	switch {
	case !complete:
	case grounds.PayloadOK:
		state.failures = 0
	case !grounds.LinkPresent:
	default:
		state.failures++
	}
	return state.failures
}

// standing is what this cycle knows about this runtime's right and ability to go
// on holding the tunnel.
func (executor executor) standing(
	grounds tunnelplan.Grounds,
	complete bool,
	grantActive bool,
) tunnelexec.Standing {
	return tunnelexec.Standing{
		Owns:             executor.owns(),
		RateAllows:       executor.rateAllows(),
		GrantActive:      grantActive,
		PayloadFailures:  executor.payload.observe(grounds, complete),
		PayloadThreshold: executor.payloadThreshold,
	}
}

// handBack gives the tunnel up through the same transaction the operator runs,
// and reports what to record. It returns nothing while there is no reason to.
func (executor executor) handBack(ctx context.Context, standing tunnelexec.Standing) *event.TunnelHandback {
	reason := standing.Handback()
	if reason == "" {
		return nil
	}
	record := &event.TunnelHandback{Reason: string(reason)}
	if executor.release != nil {
		_ = executor.release(ctx)
		// Read from the claim rather than from the command's exit status.
		//
		// A giving-up does not take the tunnel back, so after releasing the
		// claim it waits for the previous owner's tunnel to carry traffic — and
		// reports a failure when it does not, although the tunnel was given
		// back. Measured 2026-09-26T14:57:37Z: the valve fired for a dead
		// tunnel, the claim was released, the incumbent raised its own within
		// the minute, and the record said `given=false`.
		//
		// Whether the tunnel was given back is a fact on disk. A claim that
		// cannot be read is not an answer either way, and says nothing was
		// given rather than guessing.
		record.Given = executor.released()
	}
	// Word is left where the runtime that can tell the operator will find it.
	// Failing to leave it does not change what happened, so it is not allowed
	// to fail the handback: the archive has the account either way.
	if executor.notice != "" {
		_ = tunnelexec.WriteNotice(executor.notice, tunnelexec.Notice{
			Reason: record.Reason,
			At:     time.Now().UTC().Format(time.RFC3339Nano),
			Given:  record.Given,
		})
	}
	return record
}

// released is whether the claim is gone and readable.
//
// It is not the negation of owns. An unreadable claim makes owns false — the
// tunnel is another runtime's until this one can show on disk that it is not —
// and it must not make this true, because a runtime that cannot read the claim
// has not shown it gave anything back.
func (executor executor) released() bool {
	if executor.claims == nil {
		return false
	}
	_, held, err := executor.claims.Held()
	return err == nil && !held
}

// owns is whether this runtime holds the tunnel. A claim that cannot be read is
// not a claim: the tunnel is another runtime's until this one can show on disk
// that it is not.
func (executor executor) owns() bool {
	if executor.claims == nil {
		return false
	}
	_, held, err := executor.claims.Held()
	return err == nil && held
}

// rateAllows is whether the bound this runtime keeps on itself lets another
// rebuild through. A count that cannot be read is a bound reached.
func (executor executor) rateAllows() bool {
	allowed, err := executor.rate.Allows()
	return err == nil && allowed
}

// sleepFor waits, and stops waiting when the cycle is cancelled.
func sleepFor(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// authorityActive is whether the runtime's policy still stands. No authority at
// all is no standing: a runtime that cannot ask is not one that may act.
func authorityActive(authority tunnelAuthorizer) bool {
	return authority != nil && authority.MutationAllowed()
}

// payloadThreshold is how many consecutive failures with the outer path up end
// this runtime's ownership. Absent supervision is no threshold, and no threshold
// hands nothing back.
// payloadThreshold is how many consecutive failures end ownership.
//
// The executor's own number where the configuration names one, and the
// planner's where it does not. They are different questions: the planner's was
// chosen when a failing payload was a cause to rebuild, and this one decides
// whether to stop holding the tunnel. Measured 2026-09-27, the shared number
// was two, runs of two are ordinary on this path, and ownership ended three
// times in two days.
func payloadThreshold(config RuntimeConfig) uint32 {
	if config.TunnelSupervision == nil {
		return 0
	}
	if execution := config.TunnelSupervision.Execution; execution != nil &&
		execution.HandbackPayloadFailures > 0 {
		return execution.HandbackPayloadFailures
	}
	return config.TunnelSupervision.Policy.PayloadFailures
}

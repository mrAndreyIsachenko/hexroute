## Context

```
for each endpoint:  probe (about 0.7s)  → fold into the summary
```

Three of them, one after another, inside a cycle that holds the operator socket
until it finishes.

## Decisions

**Probing is separated from folding.** All probes start, all answers land in a
slice at their configured index, and only then is the summary built by walking
that slice in order. The fold is the code that was there, unchanged and still
sequential, so nothing about what is observed depends on which answer arrived
first — including which failure is the one recorded.

**Only the endpoint probes are taken together.** Power, the physical network,
the sing-box process, the TUN interfaces and seventeen route lookups cost
0.08 seconds between them. Making those concurrent would buy nothing and add
ways to be wrong.

**The observer is used from several goroutines, which it already supports.**
`ReadinessObserver` holds a connector and a clock function and mutates nothing;
`DefaultConnector` is a value holding two connectors. Nothing is shared that
would need a lock.

**The context is the cycle's own.** A cancelled cycle cancels every probe in
flight, which is what the sequential loop did between iterations and now does
within one.

## Risks

A test that asserts concurrency by timing measures the machine. The regression
here instead makes each probe announce itself and wait for the others: taken
together they all arrive and proceed, taken in turn the first waits for arrivals
that cannot come and the test fails on its own deadline rather than on a
threshold someone tuned.

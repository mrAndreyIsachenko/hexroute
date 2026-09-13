# Design

## What says a machine slept

macOS keeps two clocks worth having. `mach_absolute_time` is suspended while the
system sleeps; `gettimeofday` is not. Go's monotonic reading, the one carried
inside a `time.Time` from `time.Now`, is `mach_absolute_time` on this platform —
read from the toolchain this repository builds with, at
`src/runtime/sys_darwin_arm64.s`, where `runtime·nanotime_trampoline` calls
`libc_mach_absolute_time`.

So the sleep between two cycles is the wall interval minus the steady interval,
and it needs no `pmset`, no log scraping and no wake notification. It is also
the only construction that says *sleep* rather than *absence*: a runtime that
was busy for thirty seconds advances both clocks by thirty seconds and reports
none of it as sleep.

## Decision: the cycle carries two clocks

`Cycle` takes a wall clock and a steady clock rather than one clock. In
production the wall clock is `time.Now` and the steady clock counts from a
`time.Now` captured when the cycle was built, so it is the monotonic reading and
nothing else.

Two clocks rather than one derived quantity, because a test cannot attach a
monotonic reading to a fabricated `time.Time`. With two, a test makes the
machine sleep by advancing the wall clock and leaving the steady one still, and
makes the runtime slow by advancing both. Neither case needs a real sleep, and
the difference between them is exactly what this change is about.

## Decision: the planner is told the sleep, not the interval

`Observed.SincePrevious` becomes `Observed.Slept`. The planner never wanted the
interval; it wanted the sleep, and asking for the interval is what let a slow
runtime answer the question.

Nothing else about the planner changes. The cause still compares against
`policy.WakeThreshold`, and the threshold keeps the value measured from the
supervisor's own configuration.

## Decision: the loop schedules from the start of a cycle

The loop records when it began an observation and waits until that instant plus
the interval. When the work has already consumed the period, `time.Until`
returns a non-positive duration and the timer fires at once.

This deliberately does not skip periods or bound catch-up. A cycle that overran
by a minute yields one immediate next cycle, not a minute's worth of them,
because each cycle observes the present rather than a moment it missed.

## What this does not do

The daemon still runs about three minutes before it logs `daemon_started`, and
observes nothing in that window. Scheduling from the start of a cycle does not
touch it, and measuring sleep directly means that window no longer produces a
wake gap when observation resumes — which is a correct outcome and not a cure.
What opening two spools and an archive over a hundred thousand files costs is a
separate question with its own measurement.

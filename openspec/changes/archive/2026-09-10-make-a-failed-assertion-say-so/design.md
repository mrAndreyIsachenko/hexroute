## Context

Four shapes, measured on this machine's bash 3.2 rather than reasoned about:

| shape | errexit fires |
|---|---|
| `[[ a == b ]]` as a statement | no |
| `[[ a == b ]] && cmd` | no |
| `[ a = b ]` as a statement | yes |
| a function whose body ends in a failing `[[ ]]` | yes, at the call site |

The second row is why the count matters: a conditional heading an `&&` list is
also inert, but in these gates every one of those is control flow — `break`,
`continue`, setting a flag — and not an assertion. Four of them, all left alone.

## Decisions

**Each assertion gains a failure action rather than a change of syntax.**
Rewriting `[[ ]]` to `[ ]` would restore the exit and lose the diagnosis: the
operator would learn that the gate failed and not which claim was false. That
is the state these gates were already in on every runner but this one, and it
is worth fixing at the same time.

**A local helper per script rather than a sourced library.** These gates are
self-contained and run individually as often as through `make`; a sourced file
adds a path to resolve and a way to fail that has nothing to do with what is
being checked. Four lines in eleven files is cheaper than that.

**The message is the condition.** A hand-written description drifts from the
condition beneath it; the condition text cannot. It is the thing the reader
needs and the thing that stays true.

**A gate refuses the shape, not the bash version.** Pinning a newer bash would
fix the runner and leave the gates unable to fail for anyone running them by
hand on a Mac, which is how they are usually run. Refusing the shape fixes it
everywhere and keeps working if the runner changes again.

## Risks

Converting an assertion that was never enforced can reveal that it was false.
That is the point of doing it, and each one found is reported rather than
quietly adjusted: an assertion that has to be weakened to pass is a claim the
repository was making and could not support.

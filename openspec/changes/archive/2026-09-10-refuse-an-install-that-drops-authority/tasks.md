# Tasks

## 1. Prove the gap before closing it

- [x] 1.1 A regression that fails against the current code: the configuration
      installed on this machine on 2026-09-10, and the stale one that replaced
      it, as fixtures. The check passes both on their own and must refuse the
      second when given the first. The fixtures carry no real key material —
      the shape is what the comparison reads.
- [x] 1.2 A regression for what must not be refused: a candidate that adds, a
      candidate identical to the installed one, and an array that changed
      length.
- [x] 1.3 Confirm by mutation — invert the direction of the comparison, drop
      the recursion into nested objects, and make the refusal a warning.
      Confirm the named test fails each time, judged by exit status.

## 2. The comparison

- [x] 2.1 `internal/configreduction` answers, for two configurations, which of
      the installed one's object key paths the candidate lacks. Values are not
      compared and arrays are not descended into.
- [x] 2.2 Both daemons' `--check` take the installed configuration as a second
      input and refuse a reduction, naming what would be lost.
- [x] 2.3 The refusal is a distinct exit and a distinct reason in the log, not
      the same rejection a malformed file gets. They send the reader to
      different places.

## 3. The installers

- [x] 3.1 Each installer passes the installed configuration to the check when
      one is in place, and stops before touching the binary or the plist.
      Stopping late is what makes a half-installed daemon.
- [x] 3.2 `HEXROUTE_ALLOW_REDUCED_CONFIG=1` proceeds anyway, and says in its
      output that it did.
- [x] 3.3 The replaced configuration is kept beside the installed one with the
      same owner and mask.
- [x] 3.4 The shell gate covers all three: a reduction refused, a reduction
      allowed by the variable, and a first install with nothing to compare.

## 4. What the runtime says for itself

- [x] 4.1 A daemon that starts without the settings that let it read a policy,
      whose store holds an active generation, reports that the authority is
      present and unreadable. It does not validate what it found and does not
      refuse to run: a root daemon that will not start observes nothing.

      The reading is `policystore.AuthorityPresent`, covered by its own tests.
      The call site is not gated: both daemons find their store at a fixed path
      derived from the account, which no test can point elsewhere, so a gate
      would have to run against the machine's own store. It is proven on the
      machine in 7.2 instead, and that limit is stated here rather than left to
      be discovered.

## 5. The procedure

- [x] 5.1 `docs/macos/root-observe.md` and `user-observe.md` say what the
      refusal means, that the working copy is not the record of what is
      installed, and how to proceed when the reduction is intended.

## 6. Gates

- [x] 6.1 `make check`, judged by exit status.
- [x] 6.2 `make secret-test`, and read the new fixtures by hand. A configuration
      fixture is exactly the shape of file that carries a live pinned key.

## 7. Prove it on the machine

- [x] 7.1 Against the installed configurations: the stale working copy is
      refused, and the current one is accepted.
- [x] 7.2 Record what it said.

      Against the installed user configuration, a copy of it with
      `policy_control` removed was refused with exit 3 and named eleven
      settings, from `policy_control` down to `policy_control.pinned_public_key`
      and each field of the compatibility block. The configuration actually
      installed was accepted against itself. The root guard was exercised
      against the file root is installed from, which is byte-identical to the
      installed one because the install was made from it.

      Had this existed on 2026-09-10 it would have named twelve settings and
      refused, and the signed generation would never have left either runtime.

## 8. Close

- [x] 8.1 Sync the delta into the baseline and archive.

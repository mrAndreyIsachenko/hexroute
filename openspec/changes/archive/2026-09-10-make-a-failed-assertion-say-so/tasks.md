# Tasks

## 1. Prove the shape before changing it

- [x] 1.1 Measure each conditional shape against the bash the gates actually run
      with, rather than reasoning about `set -e`. Record which fire and which do
      not.
- [x] 1.2 A gate that refuses the shape, and that fails against the repository
      as it stands today.

## 2. Convert

- [x] 2.1 Every bare statement-level conditional in `tests/*.sh` gains a failure
      action naming the condition.
- [x] 2.2 Leave the conditionals that head an intentional `&&` list alone. They
      are control flow, not assertions, and converting them would change what
      the gate does.

## 3. Read what the conversion reveals

- [x] 3.1 Run every gate after converting. Record each assertion that now fails:
      it was a claim this repository was making and could not support.

      One of thirty-three. The other thirty-two held; they simply could not have
      failed if they had not.

      `tests/install_reduction_guard_test.sh` asserted that each installer asks
      its question before it replaces the binary, and compared the guard's line
      against the first `/usr/bin/install` anywhere in the file. Both scripts
      create directories in a helper that appears earlier, so the comparison was
      124 against 87 and 109 against 91 — false in both, and silent. It was
      written the day before, in the change that added the guard.
- [x] 3.2 For each, say whether the claim or the code was wrong, and fix the one
      that was. Never weaken an assertion to make it pass.

      The claim was right and the measurement was wrong. Both installers do ask
      before they replace anything: the guard is at 124 and the binary is
      replaced at 141 in the root script, 109 and 123 in the user script. The
      assertion now compares against the line that replaces the binary rather
      than the first `install` of any kind, and it says which line came after
      which when it fails.

      Nothing was weakened. The one assertion that changed became narrower and
      harder to satisfy, not easier.

## 4. Keep it out

- [x] 4.1 `tests/assertion_shape_test.sh` refuses a bare statement-level
      conditional in any gate, and is registered in `make shell-test`.
- [x] 4.2 Confirm by mutation: reintroduce the shape in one gate and confirm the
      new gate fails, judged by exit status.

## 5. Correct the record

- [x] 5.1 `docs/roadmap.md` records sixty-seven assertions in seventeen gates.
      That count included conditionals inside `if`, `while` and intentional `&&`
      lists. Correct it to what was ever an inert assertion.

## 6. Gates

- [x] 6.1 `make check`, judged by exit status.

## 7. Close

- [x] 7.1 Sync the delta into the baseline and archive.

# Design

## One capability, root only

`pritunl_recovery` was deliberately one capability across two domains because
its two halves are one act. This is the opposite case and the same reasoning:
rebuilding the tunnel and reapplying its routes are one ownership, so they are
one grant — two could be revoked separately, leaving a runtime allowed to tear
the tunnel down and not to put its routes back.

Root only, because the user domain holds a keychain and a one-time code and has
no business restarting a tunnel. The envelope is where that is said, and it is
said by omission from the user domain's allowed capabilities, the same way every
other domain boundary in it is.

## Asking without acting

The handler gains one method beside the two it has, and the cycle calls it when
the decision is to act. The answer joins the record the decision already writes.

Under the generation now active the answer is a refusal, and that is the point:
it proves the question is asked and reaches the handler. When a generation
carrying the capability is installed, the same records turn without a line of
code changing — and if they do not, that is learned while this runtime owns
nothing.

## Why the answer is recorded rather than logged

The log is a stream a person reads while watching. The decision record is what a
comparison is made from later, and the question "would this have been allowed"
belongs beside "what would have been done" or the two have to be joined on time
by hand — which is exactly the joining this record was widened to stop.

## What is deliberately absent

No executor. Nothing in this change can restart anything, and the safety
allowlist it would have to pass is unchanged.

No generation. Compiling and signing one that grants the capability is a
ceremony with an operator's password, and putting it in the same change as the
code that introduces the capability would make one act of two — the shape this
repository already refuses for installs.

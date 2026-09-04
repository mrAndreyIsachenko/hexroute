# Published SigV4 vectors

These are AWS's own Signature Version 4 test suite files, unmodified. Each case
gives one request (`.req`) and the three artifacts a correct signer produces
from it: the canonical request (`.creq`), the string to sign (`.sts`) and the
authorization header including the signature (`.authz`).

They are here because every other test in this package proves only that the
signer agrees with itself. A hand-rolled signature can be internally flawless
and refused by every server, and nothing written by the same author who wrote
the signer can tell the difference. These files can.

## Provenance

`docs.aws.amazon.com/general/latest/gr/signature-v4-test-suite.html` does not
serve its content to a plain fetch, so the files were taken from two mirrors of
the suite that do not share a maintainer — `mongodb/libmongocrypt` and Bazel's
`third_party/aws-sig-v4-test-suite` — and compared. All four files of every
case are byte-identical between them.

The shared credentials are the suite's own published example values: access key
`AKIDEXAMPLE`, region `us-east-1`, service `service`, timestamp
`20150830T123600Z`. The secret is a published example too, and it was confirmed
rather than trusted: deriving the signing key from it and signing the published
`.sts` reproduces the published signature exactly. A wrong secret could not.

## reproduced/

Fourteen cases this signer reproduces at all three stages.

## inapplicable/

Eight cases it does not, all for one reason: they sign a header that is neither
`host` nor `x-amz-*`, and `canonicalize` deliberately signs nothing else.
Signing whatever a transport might add produces signatures that fail for
reasons the caller cannot see. They are kept rather than deleted so that the
exclusion is a property the test checks, not a claim in a comment.

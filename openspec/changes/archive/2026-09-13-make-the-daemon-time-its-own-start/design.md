# Design

## Why the store does not take a logger

`connectivityhost.Open` returns how long each store took, and the daemon writes
the records. The alternative — handing the store a logger — would put the
logging vocabulary inside a package whose job is durability, and would make a
store that opens differently in a test also log differently.

Returning timings keeps the measurement where the work is and the record where
the vocabulary is.

## Why a closed vocabulary of steps

Every other field of a log record is allowlisted, and the reason is that this
repository is public and its logs are collected. A free-text step name is a
place for a path, a hostname or a credential to arrive by accident, and the
secret guard cannot tell a step name from a leak.

So the steps are an enum, and adding one is an edit someone makes deliberately.

## Why absent rather than zero

A step that took no measurable time and a step that was not timed are different
claims, and a zero would say the first when the second is true. The field is
omitted when unset, which is the same discipline the reason field already keeps.

## What this is not

It is not a metric system, a histogram or a timer around everything. It is one
number on the records that already exist, for the one window where the daemon
is not yet observing and nothing outside it can say why.

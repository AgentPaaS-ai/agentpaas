# Compose workflows

Two ways more than one agent can work together. The platform infers the
shape from the pack you publish.

## Pipeline

This is the default.

A writes a work order, then dies. B and C run from that order. A starts
again with its notes plus their answers. Children never see A's notes.

Use a pipeline when A does not need to stay up while the others work.

## Phone call

Use a phone call only when a living A is required.

One supervisor stays up and phones named teammates from the signed list.
A call outside that list fails. Stopping A cancels that A's children only.

## How long it may run

Set the max duration from the human's job. The platform stores that as
seconds.

# Specifications

The [product specification](spec.md) is the living source of truth for product behavior. It holds
purpose, principles, the capability registry, cross-cutting rules and evaluation, and links a tree
of stage pages:

- [Stage 1 -- registry, split and restore](stage-1-core/README.md)
- [Stage 2 -- previews](stage-2-previews/README.md)
- [Stage 3 -- cloud publishing (future)](stage-3-cloud/README.md)

The [architecture](architecture.md) maps behavior onto packages, data flow and the transaction
state machine.

The registry order is the implementation line followed by the [forward plan](../impl/plan.md).
`make lint-spec-plan` fails when the registry and the plan disagree. Current behavior is indexed by
[current implementation](../impl/current.md).

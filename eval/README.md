# Evaluation (planned)

This directory holds the offline evaluation for Waypoint's semantic
verification step — the work that answers *"does the AI add value over
deterministic rules, and where does it fail?"*

**Nothing here is implemented yet.** Evaluation arrives in a later phase.

Planned approach:

- A small, human-reviewed dataset of synthetic purchase scenarios (compliant,
  qualitative mismatch, ambiguous, adversarial) stored as JSONL under
  `datasets/` — synthetic fixtures only, never real customer data.
- A rules-only baseline compared against the hybrid (rules + semantic model)
  pipeline on identical inputs.
- Reported metrics: confusion matrix, contradiction precision/recall,
  UNKNOWN rate, latency percentiles and cost per check — always with sample
  size, model identifier and limitations disclosed.

Scripts live in `scripts/`. No fabricated numbers: if the model shows no
measurable lift, this evaluation will say exactly that.

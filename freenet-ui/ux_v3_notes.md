# Overview UX v3 checkpoint

Текущий scope ветки #279:

- throughput-aware Best Server score; very low throughput penalized explicitly;
- current VPN remains in the deep-probe shortlist and is compared by the same HTTP/TCP/jitter/throughput metrics;
- Overview v3 contract: readable typography, factual topbar with current VPN + ISP + DNS + health;
- remove tautological `FreeNet доступен` status;
- add explicit `Проверить текущий VPN` action and human-readable explanation of recommendation.

DNS Resolver Policy (Native selector + Split DIRECT/VPN DoH selectors) remains the next separate PR after #279 runtime acceptance; it is intentionally not mixed into this branch.

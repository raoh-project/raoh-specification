# Cases left out of the suite

Version 0.8.0 was extracted from the compatibility corpus of raoh-go (commit 88322d2 of `develop`,
790 cases). Every case of that corpus became a case here, except the ones below, which check
behaviour outside this specification.

| Source decoder | Inputs | Why |
|----------------|--------|-----|
| `bytes` | `[0,1,127,128,255]`, `[]`, `null` | ObjectDecoders.bytes() reads a Java byte[], a host value outside the input model |

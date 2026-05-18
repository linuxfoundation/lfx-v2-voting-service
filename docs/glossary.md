# Voting Service Glossary

Terms specific to the LFX Voting Service. For general platform terms (Goa, NATS, OpenFGA, Heimdall, OpenSearch, feature branches, signoff, etc.) see the [LFX Platform Glossary](https://github.com/linuxfoundation/skills/blob/main/lfx/references/glossary.md).

---

## System Terms

| Term | What It Is |
|------|-----------|
| **ITX** | The legacy Linux Foundation voting platform that this service proxies. All vote data lives in ITX — this service translates between LFXv2 conventions and the ITX API. |
| **v1** | The legacy LFX platform (ITX-era). Uses Salesforce IDs (SFIDs) to identify resources like projects and committees. |
| **v2** | The current LFX platform. Uses UUIDs to identify all resources. This service bridges the two. |
| **M2M (Machine-to-Machine)** | The OAuth2 authentication pattern used when this service calls ITX. Instead of a user token, the service authenticates using a private key JWT assertion against Auth0, then uses the resulting token for all ITX requests. |
| **IDMapper** | The internal service layer (backed by NATS request/reply) that translates between v1 SFIDs and v2 UUIDs. Called on every proxied request to convert IDs in both directions. |
| **JetStream** | The NATS persistence layer used for event streaming and the KV bucket. More specific than plain NATS pub/sub — JetStream provides durable consumers, replay, and at-least-once delivery. |

---

## Data Terms

| Term | What It Is |
|------|-----------|
| **SFID (Salesforce ID)** | The 18-character identifier format used by the v1 LFX platform, derived from Salesforce. Example: `a094V00000A1XyzQAF`. Appears as `project_id` and `committee_id` in ITX requests/responses. |
| **vote vs. poll** | This service calls the resource a **vote**; ITX calls the same resource a **poll**. The proxy translates between `vote_uid` ↔ `poll_id`, `/votes` ↔ `/v2/voting/poll`, etc. everywhere. |
| **vote response** | A submitted ballot — one voter's answers to a vote. Called a **vote** in the ITX API (confusingly), but a **vote response** in LFXv2. |
| **Tombstone** | A marker value (`!del`) written to the `v1-mappings` NATS KV bucket when a vote or vote response is deleted. Prevents duplicate delete events from being re-published to downstream services on redelivery. |

---

## Authorization Terms

| Term | What It Is |
|------|-----------|
| **writer** | OpenFGA relation that grants full management access to a vote — create, update, delete, extend, enable, and bulk-resend. Checked against `project:{uid}` on create; against `vote:{uid}` on all subsequent mutations. |
| **viewer** | OpenFGA relation that grants read access to vote details (`GET /votes/{id}`). |
| **results_viewer** | OpenFGA relation that grants access to aggregated vote results (`GET /votes/{id}/results`). Separate from `viewer` so results can be restricted independently. |
| **auditor** | OpenFGA relation that grants access to individual vote response details (`GET /vote_responses/{id}`), including SES delivery tracking. |
| **owner** | OpenFGA relation that allows a user to submit (`POST /vote_responses`) or update (`PUT /vote_responses/{id}`) their own vote response. |

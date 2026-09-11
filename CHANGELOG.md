# Changelog

## v0.1.5

- Pause, disconnect and token changes now close active connections and cancel
  pending work before reporting success.
- Self-checks verify that the gateway accepts the token without replacing the
  running tunnel or interrupting checks.
- Block additional private-network and cloud-metadata routes, including IPv6
  address translations that could bypass destination checks.
- Limit queued data and concurrent connections so slow destinations cannot stall
  the whole relay. Fix malformed-frame handling on 32-bit systems.
- Add the PageCrawl logo and brand styles to the settings page, and improve its
  mobile and dark-mode layouts. The page remains available offline.
- Repair existing token-file permissions on Unix and prevent partial token saves.
  Exclude local credentials from source control and Docker build contexts.

Existing relay tokens remain valid. Restart the client after updating.
Custom gateways must support the authenticated diagnostic response for self-checks
to succeed.

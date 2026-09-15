---
url: https://sf-demo.coupahost.com/middleware/avr/wsdl-reference
updated: 2026-07-19
---
# AVR SOAP WSDL Reference

A reference index of the on-prem SOAP contract the AVR facade consumes. This
lists the WSDL structure only; the operation-to-endpoint behavior lives in the
AVR SOAP service and REST facade documents.

## Bindings

- The service exposes a single `document/literal` binding over HTTPS.
- WS-Security UsernameToken is required on every call; the token is sourced
  from `avr/ws-credentials`.

## Types

The complex types are versioned with a `contractVersion` attribute. A mismatch
between the facade's expected version and the on-prem WSDL surfaces as a schema
validation warning in the facade logs, not as a SOAP fault.

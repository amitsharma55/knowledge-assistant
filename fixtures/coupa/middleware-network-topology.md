---
url: https://sf-demo.coupahost.com/middleware/network-topology
updated: 2026-08-30
---
# Middleware Network Topology

How the Coupa AWS Middleware VPC is laid out in us-east-1. This is the network
view; the service-level architecture and ownership live in the middleware
architecture document.

## Subnets

- Private subnets across three availability zones hold the Lambdas and the
  NAT gateways; there are no public subnets in the middleware VPC.
- Direct Connect terminates on a transit gateway attachment, which is the only
  path to the on-prem SOAP services.

## Boundaries

This document does not describe the public request entrypoint or its
throttling; that is covered where the request edge is documented.

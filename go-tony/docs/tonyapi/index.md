# TonyAPI Documentation

This directory contains the design documentation for TonyAPI, a unified API system based on the Tony format.

## Overview

TonyAPI presents access to "1 giant Tony document" as a computed view. Operations use Tony documents tagged with `!apiop` that specify `path:`, `match:`, and optionally `patch:` for mutations.

## Core Documents

### [TonyAPI Design](./design.md)

The main design document covering architecture, components, protocols, and design philosophy.

**Start here** for an overview of the entire system.

### [Query Operations](./queries.md)

Comprehensive guide to query operations:
- Query structure (`path:`, `match:`, `!trim`/`!notrim`)
- Field selection and aliasing (`!as`)
- Nested queries with `!let` for joins
- Relational queries
- Complete query examples

### [Controllers](./controllers.md)

Detailed documentation on controllers:
- Mount points and controller registration
- Controller communication (local vs remote)
- Controller lifecycle and operations
- Diff-based communication
- Transaction coordination for multi-mount operations
- Examples: User controller, computed fields

### [LogD Server Design](./logd-server-design.md)

Design document for the diff-based backend server:
- Filesystem-based storage architecture
- Transaction support with participant counting
- State reconstruction from diffs
- Snapshot and caching strategies
- API interface and implementation details

### [Blog API Example](./blog-api-example.md)

Complete example API used throughout the documentation:
- Virtual document structure
- Schema definition
- Design decisions and patterns
- Used to illustrate queries, mutations, and subscriptions

### [Process Controller Example](./process-controller-example.md)

Example systems-level controller:
- Unix process tracker implementation
- Launching and killing processes
- Tracking managed processes
- Real-time process state updates

## Format Specifications

### [Mutation Formats](./mutations.md)

Detailed mutation format specifications:
- Basic mutation structure
- Create, update, delete operations
- Conditional updates
- Multiple updates
- Tony diff operations (`!replace`, `!insert`, `!delete`, etc.)

### [Subscription Formats (WATCH)](./subscriptions.md)

Detailed WATCH/subscription format specifications:
- WATCH request format
- Response stream format
- Diff-based change notifications
- Behavior and connection management

### [HTTP Protocols](./http-protocols.md)

HTTP request/response formats for all operations:
- MATCH (queries and metadata)
- PATCH (mutations)
- WATCH (subscriptions)
- MOUNT (controller registration)
- Complete HTTP examples

### [Schema Formats](./schema.md)

Schema format specifications and composition:
- Unified schema structure
- Type definitions and references
- Schema querying and validation
- Controller schema contributions
- Schema composition

### [GraphQL Comparison](./graphql-comparison.md)

Comprehensive comparison with GraphQL:
- Similarities and key differences
- Mental model, query language, mutations, subscriptions
- **Transaction support** (built-in vs implementation-dependent)
- Relations, error handling, transport
- Advantages, trade-offs, and use cases

## Key Concepts

- **Virtual Document**: The API exposes "1 giant Tony document" as a computed view
- **Mount Points**: Controllers handle operations for specific paths in the document tree
- **Diff-Based Storage**: Backend stores diffs (changes) rather than full state
- **Transactions**: Multi-path operations are guaranteed atomic through explicit transaction IDs
- **HTTP Methods**: MATCH (queries), PATCH (mutations), WATCH (subscriptions), MOUNT (controller registration)

For detailed information, see [design.md](./design.md) and [graphql-comparison.md](./graphql-comparison.md).

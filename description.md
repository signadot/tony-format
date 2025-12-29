# logd: improve auto-ID handling during schema migration

## Problem

During schema migration, auto-ID injection (`!logd-auto-id`) only uses the active schema. This means:

1. **Adding new auto-ID fields** requires a two-phase migration:
   - First migrate to add the field without `!logd-auto-id`
   - Use `MigrationPatch` to populate existing records
   - Complete migration
   - Then migrate again to add `!logd-auto-id`

2. **Records created during migration** won't have auto-generated values for new auto-ID fields until migration completes.

## Possible Improvements

- Dual auto-ID injection: inject IDs for both active and pending schemas during migration
- Track which records were created during migration and backfill auto-IDs on completion
- Provide a `MigrationPatch` variant that auto-generates IDs using pending schema

## Current Behavior

Documented in `api/session.go` on `SchemaSetRequest`. The two-phase approach works but is cumbersome for users.

## Related

- Issue #087: logd expose schema to clients (parent)
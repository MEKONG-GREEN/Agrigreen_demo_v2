-- Validate separately so the scan does not retain the ADD CONSTRAINT lock.
-- Forward-only, no data/column/ledger hash changes.
SET LOCAL lock_timeout = '5s';
ALTER TABLE pf_masterdata.catalog_effective
  VALIDATE CONSTRAINT catalog_effective_catalog_registry_check;

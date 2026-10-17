-- Expand the existing effective catalog ledger without rewriting any historical row.
-- Forward-only. Validation and legacy-constraint removal are separate transactions/files.
SET LOCAL lock_timeout = '5s';
ALTER TABLE pf_masterdata.catalog_effective
  ADD CONSTRAINT catalog_effective_catalog_registry_check
  CHECK (catalog ~ '^[a-z][a-z_]{1,39}$') NOT VALID;

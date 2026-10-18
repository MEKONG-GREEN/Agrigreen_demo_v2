-- Only remove the historical prefix restriction after the generic check is validated.
-- Forward-only: an application rollback must keep the wider, validated storage contract.
SET LOCAL lock_timeout = '5s';
DO $migration$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
     WHERE conrelid = 'pf_masterdata.catalog_effective'::regclass
       AND conname = 'catalog_effective_catalog_registry_check'
       AND convalidated
  ) THEN
    RAISE EXCEPTION 'catalog registry check must be validated before removing legacy check';
  END IF;
END
$migration$;
ALTER TABLE pf_masterdata.catalog_effective
  DROP CONSTRAINT catalog_effective_catalog_check;

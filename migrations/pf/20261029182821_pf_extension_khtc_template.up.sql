-- W11.65, BRD KHTC v2.3 FN-AD-04.03. KN owns authoring/publishing;
-- pf_export remains generic and immutable. No existing draft/export is rewritten.
CREATE TABLE pf_extension.khtc_template_version (
  id uuid NOT NULL,
  tenant_id uuid NOT NULL,
  stream_key text NOT NULL,
  seq bigint NOT NULL,
  template_key text NOT NULL CHECK (template_key IN ('mau_1','mau_2','mau_3','mau_4','mau_5')),
  action text NOT NULL CHECK (action IN ('save','restore')),
  definition text NOT NULL CHECK (octet_length(definition) BETWEEN 1 AND 1048576 AND jsonb_typeof(definition::jsonb) = 'object'),
  definition_hash text NOT NULL CHECK (definition_hash = encode(sha256(convert_to(definition,'UTF8')),'hex')),
  request_hash text NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
  restored_from uuid NULL,
  event_time timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  actor_ref uuid NOT NULL,
  on_behalf_of uuid NULL,
  support_reason text NULL,
  idem_key text NOT NULL,
  rule_version text NOT NULL,
  prev_hash bytea NOT NULL,
  row_hash bytea NOT NULL,
  PRIMARY KEY (id,recorded_at),
  CHECK (stream_key = 'khtc_template:' || template_key),
  CHECK ((action = 'restore') = (restored_from IS NOT NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX khtc_template_version_key ON pf_extension.khtc_template_version (tenant_id,template_key,seq DESC);
CREATE INDEX khtc_template_version_id ON pf_extension.khtc_template_version (tenant_id,id);
SELECT shared.apply_ledger('pf_extension.khtc_template_version','pf_extension.khtc_template_version');

CREATE TABLE pf_extension.khtc_template_publication (
  id uuid NOT NULL,
  tenant_id uuid NOT NULL,
  stream_key text NOT NULL,
  seq bigint NOT NULL,
  template_key text NOT NULL CHECK (template_key IN ('mau_1','mau_2','mau_3','mau_4','mau_5')),
  version_id uuid NOT NULL,
  version_no bigint NOT NULL CHECK (version_no > 0),
  definition_hash text NOT NULL CHECK (definition_hash ~ '^[0-9a-f]{64}$'),
  applies_from date NOT NULL CHECK (applies_from BETWEEN DATE '2000-01-01' AND DATE '2100-12-31'),
  request_hash text NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
  event_time timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  actor_ref uuid NOT NULL,
  on_behalf_of uuid NULL,
  support_reason text NULL,
  idem_key text NOT NULL,
  rule_version text NOT NULL,
  prev_hash bytea NOT NULL,
  row_hash bytea NOT NULL,
  PRIMARY KEY (id,recorded_at),
  CHECK (stream_key = 'khtc_template_publish:' || template_key)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX khtc_template_publication_date ON pf_extension.khtc_template_publication (tenant_id,template_key,applies_from DESC,seq DESC);
SELECT shared.apply_ledger('pf_extension.khtc_template_publication','pf_extension.khtc_template_publication');

CREATE FUNCTION pf_extension.fn_khtc_template_publication_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog,pg_temp AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pf_extension.khtc_template_version v
    WHERE v.tenant_id=NEW.tenant_id AND v.id=NEW.version_id AND v.template_key=NEW.template_key
      AND v.seq=NEW.version_no AND v.definition_hash=NEW.definition_hash) THEN
    RAISE EXCEPTION 'published template must reference its immutable tenant version'
      USING ERRCODE='check_violation', CONSTRAINT='khtc_template_publication_version';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_khtc_template_publication_guard BEFORE INSERT ON pf_extension.khtc_template_publication
  FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_template_publication_guard();

REVOKE ALL ON pf_extension.khtc_template_version,pf_extension.khtc_template_publication FROM PUBLIC;
GRANT SELECT,INSERT ON pf_extension.khtc_template_version,pf_extension.khtc_template_publication TO r_extension;

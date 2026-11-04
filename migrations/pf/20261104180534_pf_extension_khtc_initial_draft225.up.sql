-- Initial server drafts only. No draft is visible through the published plan ledger.
SET LOCAL lock_timeout = '5s';
CREATE TABLE pf_extension.khtc_plan_draft225_event (
 id uuid NOT NULL, tenant_id uuid NOT NULL, stream_key text NOT NULL, seq bigint NOT NULL,
 plan_year smallint NOT NULL CHECK(plan_year BETWEEN 2000 AND 2100),
 draft_id uuid NOT NULL, action text NOT NULL CHECK(action IN ('create','save','discard')),
 expected_revision bigint NOT NULL CHECK(expected_revision>=0),
 base_version integer NOT NULL CHECK(base_version=0),
 entries_count integer NOT NULL CHECK(entries_count BETWEEN 0 AND 5000),
 entries_hash bytea NOT NULL CHECK(octet_length(entries_hash)=32),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 authority_hash bytea NOT NULL CHECK(octet_length(authority_hash)=32),
 note text NOT NULL CHECK(char_length(note)<=1000),
 event_time timestamptz NOT NULL, recorded_at timestamptz NOT NULL DEFAULT now(),
 actor_ref uuid NOT NULL, on_behalf_of uuid NULL, support_reason text NULL,
 idem_key text NOT NULL, rule_version text NOT NULL, prev_hash bytea NOT NULL, row_hash bytea NOT NULL,
 PRIMARY KEY(id,recorded_at), UNIQUE(tenant_id,id,recorded_at,plan_year),
 CHECK(stream_key='plan-draft:'||plan_year::text), CHECK(action<>'discard' OR entries_count=0)
) PARTITION BY RANGE(recorded_at);
CREATE INDEX khtc_plan_draft225_year ON pf_extension.khtc_plan_draft225_event(tenant_id,plan_year,seq DESC);
SELECT shared.apply_ledger('pf_extension.khtc_plan_draft225_event','pf_extension.khtc_plan_draft225_event');
CREATE TABLE pf_extension.khtc_plan_draft225_entry (

  tenant_id         uuid          NOT NULL,
  draft_event_id        uuid          NOT NULL,               -- khtc_plan_version.id (cùng giao dịch)
  draft_recorded_at timestamptz NOT NULL,
  plan_year         smallint      NOT NULL CHECK (plan_year BETWEEN 2000 AND 2100),
  entry_ref         uuid          NOT NULL,               -- bền qua các phiên bản của năm
  position          integer       NOT NULL CHECK (position BETWEEN 0 AND 4999), -- thứ tự trong phiên bản (cha trước con)
  tab               text          NOT NULL CHECK (tab IN ('programs', 'prev_year', 'central_project', 'cooperation',
                                    'research', 'advisory')),
  kind              text          NOT NULL CHECK (kind IN ('program', 'group', 'item')),
  parent_ref        uuid          NULL,
  program_ref       uuid          NULL,                   -- khtc_program.program_ref (dòng program bắt buộc)
  sort_order        integer       NOT NULL CHECK (sort_order BETWEEN 0 AND 100000),
  code              text          NULL CHECK (code IS NULL OR char_length(code) BETWEEN 1 AND 40),
  name              text          NOT NULL CHECK (char_length(name) BETWEEN 1 AND 1000),
  unit_code         text          NULL,                   -- ĐVT (danh mục KHTC kind 'unit')
  planned_qty       numeric(20,4) NULL CHECK (planned_qty IS NULL OR planned_qty >= 0),
  scale_note        text          NULL CHECK (scale_note IS NULL OR char_length(scale_note) <= 1000),
  budget            numeric(20,2) NULL CHECK (budget IS NULL OR budget >= 0),  -- kinh phí KH (đồng)
  funding_code      text          NULL,                   -- nguồn kinh phí (kind 'funding_source')
  field_code        text          NULL,                   -- lĩnh vực (kind 'field')
  department_code   text          NULL CHECK (department_code IS NULL OR department_code ~ '^[a-z][a-z0-9_]{1,39}$'),
  communes          text[]        NOT NULL DEFAULT '{}' CHECK (cardinality(communes) <= 500),
  points_count      integer       NULL CHECK (points_count IS NULL OR points_count BETWEEN 0 AND 100000),
  data_source       text          NULL CHECK (data_source IS NULL OR data_source IN ('form', 'module')),
  source_key        text          NULL CHECK (source_key IS NULL OR source_key ~ '^[a-z][a-z0-9_.:-]{1,99}$'),
  tracking          text          NOT NULL CHECK (tracking IN ('tracking', 'stopped')),
  stopped_from      date          NULL,
  stop_decision     text          NULL CHECK (stop_decision IS NULL OR char_length(stop_decision) <= 100),
  note              text          NULL CHECK (note IS NULL OR char_length(note) <= 2000),
  recorded_at       timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, draft_event_id, entry_ref),
  CHECK (kind <> 'program' OR (program_ref IS NOT NULL AND parent_ref IS NULL)),
  CHECK (kind = 'program' OR parent_ref IS NOT NULL),
  -- Hạng mục có đúng một phòng phụ trách (FN-TI-02.02 quy tắc); nguồn module phải có khóa nguồn (BR-DL-10).
  CHECK (kind <> 'item' OR (department_code IS NOT NULL AND data_source IS NOT NULL)),
  CHECK (data_source IS DISTINCT FROM 'module' OR source_key IS NOT NULL),
  UNIQUE(tenant_id,draft_event_id,position),
  FOREIGN KEY(tenant_id,draft_event_id,draft_recorded_at,plan_year) REFERENCES pf_extension.khtc_plan_draft225_event(tenant_id,id,recorded_at,plan_year) DEFERRABLE INITIALLY DEFERRED,
  CHECK ((tracking = 'stopped') = (stopped_from IS NOT NULL))
);
SELECT shared.apply_append_only('pf_extension.khtc_plan_draft225_entry');
REVOKE ALL ON pf_extension.khtc_plan_draft225_event,pf_extension.khtc_plan_draft225_entry FROM PUBLIC;
GRANT SELECT,INSERT ON pf_extension.khtc_plan_draft225_event,pf_extension.khtc_plan_draft225_entry TO r_extension;
CREATE INDEX khtc_adhoc_open225_owner_year ON pf_extension.khtc_adhoc_open_intent(tenant_id,(plan->>'Year'));

CREATE FUNCTION pf_extension.fn_khtc225_active(p_tenant uuid,p_year integer) RETURNS boolean
LANGUAGE sql STABLE AS $$
 SELECT coalesce((SELECT action<>'discard' FROM pf_extension.khtc_plan_draft225_event
 WHERE tenant_id=p_tenant AND plan_year=p_year ORDER BY seq DESC LIMIT 1),false)
$$;
CREATE FUNCTION pf_extension.fn_khtc225_referenced(p_tenant uuid,p_year integer) RETURNS boolean
LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM pf_extension.khtc_period WHERE tenant_id=p_tenant AND plan_year=p_year)
 OR EXISTS(SELECT 1 FROM pf_extension.khtc_adhoc_open_intent WHERE tenant_id=p_tenant AND
 (plan->>'Year'=p_year::text OR NOT coalesce(jsonb_typeof(plan->'Year')='number' AND plan->>'Year' ~ '^(20[0-9]{2}|2100)$',false)))
$$;

-- Guard BEFORE trg_chain: all app/raw INSERT paths acquire the same year lock first.
CREATE FUNCTION pf_extension.fn_khtc225_draft_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE prior pf_extension.khtc_plan_draft225_event%ROWTYPE; n integer;
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended('pf_extension.khtc:plan:'||NEW.plan_year::text,0));
 SELECT * INTO prior FROM pf_extension.khtc_plan_draft225_event
 WHERE tenant_id=NEW.tenant_id AND plan_year=NEW.plan_year ORDER BY seq DESC LIMIT 1;
 IF NEW.expected_revision<>coalesce(prior.seq,0) THEN RAISE EXCEPTION 'khtc_draft225_conflict' USING ERRCODE='23514'; END IF;
 IF NEW.action='create' THEN
  IF prior.action IS NOT NULL AND prior.action<>'discard' THEN RAISE EXCEPTION 'khtc_draft225_conflict' USING ERRCODE='23514'; END IF;
 ELSE
  IF prior.action IS NULL OR prior.action='discard' OR prior.draft_id<>NEW.draft_id THEN RAISE EXCEPTION 'khtc_draft225_conflict' USING ERRCODE='23514'; END IF;
 END IF;
 IF EXISTS(SELECT 1 FROM pf_extension.khtc_plan_version WHERE tenant_id=NEW.tenant_id AND plan_year=NEW.plan_year)
 OR pf_extension.fn_khtc225_referenced(NEW.tenant_id,NEW.plan_year) THEN
  RAISE EXCEPTION 'khtc_draft225_year_referenced' USING ERRCODE='23514';
 END IF;
 SELECT count(*) INTO n FROM pf_extension.khtc_plan_draft225_entry
 WHERE tenant_id=NEW.tenant_id AND draft_event_id=NEW.id AND draft_recorded_at=NEW.recorded_at;
 IF n<>NEW.entries_count THEN RAISE EXCEPTION 'khtc_draft225_incomplete' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER a00_khtc225_draft_guard BEFORE INSERT ON pf_extension.khtc_plan_draft225_event
 FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc225_draft_guard();

-- Entries are staged before their immutable header in the SAME TX. Deferred FK
-- prevents orphan commit; once a header exists, no later insert may change it.
CREATE FUNCTION pf_extension.fn_khtc225_entry_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended('pf_extension.khtc:plan:'||NEW.plan_year::text,0));
 IF EXISTS(SELECT 1 FROM pf_extension.khtc_plan_draft225_event
 WHERE tenant_id=NEW.tenant_id AND id=NEW.draft_event_id AND recorded_at=NEW.draft_recorded_at) THEN
  RAISE EXCEPTION 'khtc_draft225_entries_frozen' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER a00_khtc225_entry_guard BEFORE INSERT ON pf_extension.khtc_plan_draft225_entry
 FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc225_entry_guard();

CREATE FUNCTION pf_extension.fn_khtc225_publish_fence() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE y integer;
BEGIN
 IF TG_ARGV[0]='intent' THEN
  IF NOT coalesce(jsonb_typeof(NEW.plan->'Year')='number' AND NEW.plan->>'Year' ~ '^(20[0-9]{2}|2100)$',false) THEN
   RAISE EXCEPTION 'khtc_draft225_unknown_owner_year' USING ERRCODE='23514';
  END IF;
  y:=(NEW.plan->>'Year')::integer;
 ELSE y:=NEW.plan_year;
 END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended('pf_extension.khtc:plan:'||y::text,0));
 IF pf_extension.fn_khtc225_active(NEW.tenant_id,y) THEN
  RAISE EXCEPTION 'khtc_draft225_active' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER a00_khtc225_version_fence BEFORE INSERT ON pf_extension.khtc_plan_version
 FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc225_publish_fence('version');
CREATE TRIGGER a00_khtc225_period_fence BEFORE INSERT ON pf_extension.khtc_period
 FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc225_publish_fence('period');
CREATE TRIGGER a00_khtc225_intent_fence BEFORE INSERT ON pf_extension.khtc_adhoc_open_intent
 FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc225_publish_fence('intent');

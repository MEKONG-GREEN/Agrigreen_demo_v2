-- P0-HTX-KREF (#130): the k/ref-owned trigger runs inside INSERT pf_party.org.
-- On any projection error the source node, ledger and outbox transaction aborts.
-- Existing outbox emission remains for remote clusters and share-scope consumers.
CREATE TRIGGER trg_kref_created AFTER INSERT ON pf_party.org
  FOR EACH ROW EXECUTE FUNCTION k_ref.project_created_org();

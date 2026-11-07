-- PRICE-TICKET-EDIT-LEASE: technical, expiring edit ownership (FN-TI-02.04 BR-06/AC-02).
-- A ticket has one holder; each tab has its own expiring handle. No names/PII are stored here.
-- These are transient coordination tables, not business ledgers. Ticket history stays append-only.
CREATE TABLE pf_price.ticket_edit_lease (
  tenant_id uuid NOT NULL,
  ticket_id uuid NOT NULL,
  holder_ref uuid NOT NULL,
  PRIMARY KEY (tenant_id, ticket_id)
);
CREATE TABLE pf_price.ticket_edit_handle (
  tenant_id uuid NOT NULL,
  ticket_id uuid NOT NULL,
  handle_id uuid NOT NULL,
  expires_at timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, ticket_id, handle_id),
  FOREIGN KEY (tenant_id, ticket_id) REFERENCES pf_price.ticket_edit_lease (tenant_id, ticket_id)
);
SELECT shared.apply_tenant_rls('pf_price.ticket_edit_lease');
SELECT shared.apply_tenant_rls('pf_price.ticket_edit_handle');
REVOKE ALL ON pf_price.ticket_edit_lease, pf_price.ticket_edit_handle FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON pf_price.ticket_edit_lease TO r_price;
GRANT SELECT, INSERT, UPDATE, DELETE ON pf_price.ticket_edit_handle TO r_price;

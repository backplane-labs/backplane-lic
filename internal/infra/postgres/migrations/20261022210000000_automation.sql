-- sqld:up
CREATE TABLE lic.automation_activity_results (
  account_id uuid NOT NULL REFERENCES lic.accounts(id) ON DELETE CASCADE,
  execution_key text NOT NULL,
  request_hash text NOT NULL,
  result jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (account_id, execution_key)
);

-- sqld:down
DROP TABLE lic.automation_activity_results;

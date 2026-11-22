CREATE TABLE IF NOT EXISTS workbench_github_account_backoffs (
  account_id INTEGER PRIMARY KEY,
  blocked_until TEXT NOT NULL
);

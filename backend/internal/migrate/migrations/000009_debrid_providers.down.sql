ALTER TABLE debrid_accounts DROP CONSTRAINT debrid_accounts_provider_check;
ALTER TABLE debrid_accounts ADD CONSTRAINT debrid_accounts_provider_check
    CHECK (provider IN ('realdebrid', 'torbox'));

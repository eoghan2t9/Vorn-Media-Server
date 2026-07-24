-- AllDebrid/Premiumize/Debrid-Link were added as debrid.Provider
-- implementations without a matching migration, so saving an account for
-- any of them would fail this table's original CHECK constraint at the DB
-- layer even though the Go/frontend side fully supported them.
ALTER TABLE debrid_accounts DROP CONSTRAINT debrid_accounts_provider_check;
ALTER TABLE debrid_accounts ADD CONSTRAINT debrid_accounts_provider_check
    CHECK (provider IN ('realdebrid', 'torbox', 'alldebrid', 'premiumize', 'debridlink'));

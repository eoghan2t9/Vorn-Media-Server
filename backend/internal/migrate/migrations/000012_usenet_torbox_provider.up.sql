-- Let a "usenet server" row be either a real NNTP server (existing
-- host/port/username/password fields) or a TorBox account, which caches
-- and repairs an NZB server-side via its own Usenet backend and hands back
-- whole finished files over HTTPS instead of raw yEnc segments.
ALTER TABLE usenet_servers
    ADD COLUMN provider TEXT NOT NULL DEFAULT 'nntp' CHECK (provider IN ('nntp', 'torbox')),
    ADD COLUMN api_key TEXT NOT NULL DEFAULT '';

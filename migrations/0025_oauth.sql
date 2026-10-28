-- OAuth 2.1 authorization server for MCP clients (ATL-75, ADR-0025). Access tokens are ordinary api_tokens
-- (hashed, scoped, revocable in the token list); only clients, codes and refresh tokens are new.
CREATE TABLE oauth_clients (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  redirect_uris TEXT NOT NULL, -- JSON array, exact match at authorize time
  created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE oauth_codes (
  code_hash TEXT PRIMARY KEY,
  client_id TEXT NOT NULL REFERENCES oauth_clients (id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  redirect_uri TEXT NOT NULL,
  challenge TEXT NOT NULL, -- PKCE S256 challenge
  scopes TEXT NOT NULL,
  expires_at INTEGER NOT NULL
) STRICT;

CREATE TABLE oauth_refresh (
  token_hash TEXT PRIMARY KEY,
  family TEXT NOT NULL, -- all refresh tokens that descend from one authorization
  client_id TEXT NOT NULL REFERENCES oauth_clients (id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  scopes TEXT NOT NULL,
  access_token_id INTEGER REFERENCES api_tokens (id) ON DELETE SET NULL,
  expires_at INTEGER NOT NULL,
  used_at INTEGER,
  created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX oauth_refresh_family ON oauth_refresh (family);

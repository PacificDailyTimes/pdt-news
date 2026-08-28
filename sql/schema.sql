-- pdt-news PostgreSQL schema
CREATE TABLE IF NOT EXISTS meta (
  k TEXT PRIMARY KEY,
  v TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
  id BIGSERIAL PRIMARY KEY,
  login_id VARCHAR(64) NOT NULL UNIQUE,
  email VARCHAR(160) NOT NULL UNIQUE,
  pass_hash TEXT,
  role TEXT NOT NULL DEFAULT 'consumer',
  handle VARCHAR(64) UNIQUE,
  name TEXT NOT NULL DEFAULT '',
  bio TEXT NOT NULL DEFAULT '',
  avatar TEXT,
  totp_secret TEXT,
  totp_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS users_role ON users(role);

CREATE TABLE IF NOT EXISTS sessions (
  token TEXT PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS magic_links (
  token TEXT PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS passkeys (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  cred_id BYTEA NOT NULL UNIQUE,
  pubkey TEXT NOT NULL,
  name TEXT,
  sign_count INT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS sites (
  id BIGSERIAL PRIMARY KEY,
  slug TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  tagline TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  theme TEXT NOT NULL DEFAULT 'masthead',
  colors JSONB NOT NULL DEFAULT '{}',
  is_main BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS site_members (
  site_id BIGINT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  can_post BOOLEAN NOT NULL DEFAULT TRUE,
  PRIMARY KEY (site_id, user_id)
);

CREATE TABLE IF NOT EXISTS series (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  slug TEXT NOT NULL,
  descr TEXT NOT NULL DEFAULT '',
  lang TEXT NOT NULL DEFAULT 'en',
  author TEXT,
  owner TEXT,
  email TEXT,
  copy TEXT,
  keywords TEXT,
  explicit BOOLEAN NOT NULL DEFAULT FALSE,
  itunes_cat TEXT,
  UNIQUE (site_id, slug)
);

CREATE TABLE IF NOT EXISTS pieces (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  author_id BIGINT NOT NULL REFERENCES users(id),
  type TEXT NOT NULL DEFAULT 'post',
  status TEXT NOT NULL DEFAULT 'draft',
  series_id BIGINT REFERENCES series(id),
  title TEXT NOT NULL,
  slug TEXT NOT NULL,
  content TEXT NOT NULL DEFAULT '',
  excerpt TEXT NOT NULL DEFAULT '',
  feat_img TEXT,
  feat_aud TEXT,
  feat_vid TEXT,
  feat_place TEXT NOT NULL DEFAULT 'top',
  published_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (site_id, slug)
);
CREATE INDEX IF NOT EXISTS pieces_live ON pieces(site_id, type, status, published_at DESC);

CREATE TABLE IF NOT EXISTS menus (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  label TEXT NOT NULL,
  href TEXT NOT NULL,
  pos INT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS media (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT REFERENCES sites(id) ON DELETE SET NULL,
  user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
  kind TEXT NOT NULL,
  path TEXT NOT NULL,
  title TEXT,
  mime TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS aggregation (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  source TEXT NOT NULL,
  series_id BIGINT REFERENCES series(id),
  interval_min INT NOT NULL DEFAULT 15,
  status TEXT NOT NULL DEFAULT 'active',
  last_fetch TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS style_classes (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  css TEXT NOT NULL,
  UNIQUE (site_id, name)
);

CREATE TABLE IF NOT EXISTS products (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  slug TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  price_cents INT NOT NULL DEFAULT 0,
  currency TEXT NOT NULL DEFAULT 'USD',
  kind TEXT NOT NULL DEFAULT 'physical',
  virtual_url TEXT,
  interval_n INT,
  interval_unit TEXT,
  ship BOOLEAN NOT NULL DEFAULT FALSE,
  weight_g INT,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  UNIQUE (site_id, slug)
);

CREATE TABLE IF NOT EXISTS orders (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT REFERENCES users(id),
  email TEXT,
  total_cents INT NOT NULL,
  tax_cents INT NOT NULL DEFAULT 0,
  ship_cents INT NOT NULL DEFAULT 0,
  currency TEXT NOT NULL DEFAULT 'USD',
  status TEXT NOT NULL DEFAULT 'pending',
  dest_country TEXT,
  dest_state TEXT,
  dest_zip TEXT,
  dest_addr TEXT,
  pay_via TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS order_items (
  id BIGSERIAL PRIMARY KEY,
  order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  product_id BIGINT REFERENCES products(id),
  title TEXT NOT NULL,
  qty INT NOT NULL DEFAULT 1,
  price_cents INT NOT NULL
);

CREATE TABLE IF NOT EXISTS invoices (
  id BIGSERIAL PRIMARY KEY,
  order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  number TEXT NOT NULL UNIQUE,
  pdf_path TEXT,
  emailed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS subscriptions (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  product_id BIGINT NOT NULL REFERENCES products(id),
  status TEXT NOT NULL DEFAULT 'active',
  interval_n INT NOT NULL,
  interval_unit TEXT NOT NULL,
  start_on DATE NOT NULL,
  skip_until DATE,
  next_on DATE NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS follows (
  consumer_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  author_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY (consumer_id, author_id)
);

CREATE TABLE IF NOT EXISTS entitlements (
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  product_id BIGINT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  source TEXT NOT NULL DEFAULT 'purchase',
  PRIMARY KEY (user_id, product_id)
);
